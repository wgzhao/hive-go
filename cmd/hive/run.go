package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	hive "github.com/wgzhao/hive-go"
	"github.com/wgzhao/hive-go/internal/sqlsplit"
)

// runStream executes a script from -e, -f or non-TTY stdin. It supports SQL
// statements terminated by semicolons plus REPL-style "!" meta commands.
func runStream(ctx context.Context, client *hive.Client, script string, opts *resolvedOptions) error {
	state := newReplState(client, opts)
	var pending strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(script))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	executed := false

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if pending.Len() == 0 && trimmed == "" {
			continue
		}
		if pending.Len() == 0 && strings.HasPrefix(trimmed, "!") {
			quit, err := state.handleMeta(ctx, trimmed)
			if err != nil {
				return err
			}
			executed = true
			if quit {
				return nil
			}
			continue
		}
		pending.WriteString(line)
		pending.WriteByte('\n')
		if !sqlsplit.Complete(pending.String()) {
			continue
		}
		statement := pending.String()
		pending.Reset()
		if err := state.executeStatements(ctx, nil, statement); err != nil {
			return err
		}
		executed = true
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read script: %w", err)
	}
	if strings.TrimSpace(pending.String()) != "" {
		if err := state.executeStatements(ctx, nil, pending.String()); err != nil {
			return err
		}
		executed = true
	}
	if !executed && !opts.silent {
		fmt.Fprintln(os.Stderr, "No statements to execute")
	}
	return nil
}

// runStatement executes one statement and writes its result set.
func runStatement(ctx context.Context, client *hive.Client, statement string, opts *resolvedOptions) (int, error) {
	start := time.Now()
	result, err := client.Execute(ctx, statement)
	if err != nil {
		return 0, err
	}
	defer result.Close()

	rows := 0
	columns := result.Columns()
	if len(columns) > 0 {
		writer := newResultWriter(opts, os.Stdout)
		if err := writer.WriteHeader(columns); err != nil {
			return 0, err
		}
		for {
			row, err := result.Next(ctx)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return rows, err
			}
			if err := writer.WriteRow(row); err != nil {
				return rows, err
			}
			rows++
		}
		if err := writer.Close(); err != nil {
			return rows, err
		}
	} else if !opts.silent {
		if affected, ok := result.RowsAffected(); ok {
			fmt.Fprintf(os.Stderr, "Rows affected: %d\n", affected)
		} else {
			fmt.Fprintln(os.Stderr, "OK")
		}
	}

	if !opts.silent {
		for _, message := range result.InfoMessages() {
			fmt.Fprintf(os.Stderr, "Info: %s\n", message)
		}
	}
	for _, warning := range result.Warnings() {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", warning)
	}
	if opts.verbose {
		blob, columnar, rowBased := result.FetchStats()
		fmt.Fprintf(os.Stderr, "Fetch batches: blob=%d columnar=%d row-based=%d\n",
			blob, columnar, rowBased)
	}
	if opts.timing {
		fmt.Fprintf(os.Stderr, "Time taken: %.3f seconds, Fetched: %d row(s)\n",
			time.Since(start).Seconds(), rows)
	}
	if err := result.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: close operation: %v\n", err)
	}
	return rows, nil
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		s = s[:157] + "..."
	}
	return s
}

func parseUse(statement string) (string, bool) {
	s := strings.TrimSpace(statement)
	if len(s) < 4 {
		return "", false
	}
	if !strings.EqualFold(s[:3], "use") || (s[3] != ' ' && s[3] != '\t' && s[3] != '\n' && s[3] != '\r') {
		return "", false
	}
	rest := strings.TrimSpace(s[3:])
	if rest == "" {
		return "", false
	}
	if rest[0] == '`' {
		var b strings.Builder
		for i := 1; i < len(rest); i++ {
			if rest[i] == '`' {
				if i+1 < len(rest) && rest[i+1] == '`' {
					b.WriteByte('`')
					i++
					continue
				}
				return b.String(), b.Len() > 0
			}
			b.WriteByte(rest[i])
		}
		return "", false
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", false
	}
	name := strings.TrimSuffix(fields[0], ";")
	if name == "" {
		return "", false
	}
	return name, true
}
