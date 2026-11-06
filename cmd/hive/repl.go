package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/chzyer/readline"
	hive "github.com/wgzhao/hive-go"
	"github.com/wgzhao/hive-go/internal/sqlsplit"
)

type replState struct {
	client    *hive.Client
	opts      *resolvedOptions
	currentDB string

	format     string
	nullString string
	noHeader   bool
	verbose    bool
	timing     bool
	silent     bool
	fetchSize  int
}

func newReplState(client *hive.Client, opts *resolvedOptions) *replState {
	state := &replState{
		client:     client,
		opts:       opts,
		currentDB:  opts.cfg.Database,
		format:     opts.format,
		nullString: opts.nullString,
		noHeader:   opts.noHeader,
		verbose:    opts.verbose,
		timing:     opts.timing,
		silent:     opts.silent,
		fetchSize:  opts.cfg.FetchSize,
	}
	if state.currentDB == "" {
		state.currentDB = "default"
	}
	state.applyVerbose()
	return state
}

func runREPL(ctx context.Context, client *hive.Client, opts *resolvedOptions) int {
	state := newReplState(client, opts)

	if opts.historyFile != "" {
		if dir := filepath.Dir(opts.historyFile); dir != "" && dir != "." {
			_ = os.MkdirAll(dir, 0700)
		}
	}
	rl, err := readline.NewEx(&readline.Config{
		Prompt:            state.prompt(),
		HistoryFile:       opts.historyFile,
		HistorySearchFold: true,
		InterruptPrompt:   "^C",
		EOFPrompt:         "exit",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: initialize readline: %v\n", err)
		return 1
	}
	defer rl.Close()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	var pending strings.Builder
	for {
		prompt := state.prompt()
		if pending.Len() > 0 {
			prompt = "      -> "
		}
		rl.SetPrompt(prompt)
		line, err := rl.Readline()
		if errors.Is(err, readline.ErrInterrupt) {
			pending.Reset()
			continue
		}
		if errors.Is(err, io.EOF) {
			if strings.TrimSpace(pending.String()) != "" {
				if err := state.executeScript(ctx, sigCh, pending.String()); err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				}
			}
			return 0
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}

		trimmed := strings.TrimSpace(line)
		if pending.Len() == 0 && trimmed == "" {
			continue
		}
		if pending.Len() == 0 && strings.HasPrefix(trimmed, "!") {
			quit, err := state.handleMeta(ctx, trimmed)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				continue
			}
			if quit {
				return 0
			}
			continue
		}

		pending.WriteString(line)
		pending.WriteByte('\n')
		if !sqlsplit.Complete(pending.String()) {
			continue
		}
		script := pending.String()
		pending.Reset()
		if err := state.executeScript(ctx, sigCh, script); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
	}
}

func (s *replState) prompt() string {
	return fmt.Sprintf("hive (%s)> ", displayDatabase(s.currentDB))
}

func (s *replState) snapshot() *resolvedOptions {
	opts := *s.opts
	opts.format = s.format
	opts.nullString = s.nullString
	opts.noHeader = s.noHeader
	opts.verbose = s.verbose
	opts.timing = s.timing
	opts.silent = s.silent
	return &opts
}

func (s *replState) executeScript(ctx context.Context, sigCh <-chan os.Signal, script string) error {
	err := s.executeStatements(ctx, sigCh, script)
	if errors.Is(err, hive.ErrCanceled) || errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "Query canceled")
		return nil
	}
	return err
}

func (s *replState) executeStatements(ctx context.Context, sigCh <-chan os.Signal, script string) error {
	statements := sqlsplit.Split(script)
	for _, statement := range statements {
		if s.verbose {
			fmt.Fprintf(os.Stderr, "-- statement: %s\n", oneLine(statement))
		}
		queryCtx, cancel := context.WithCancel(ctx)
		stop := make(chan struct{})
		if sigCh != nil {
			go func() {
				select {
				case <-sigCh:
					cancel()
				case <-stop:
				}
			}()
		}

		s.client.SetFetchSize(s.fetchSize)
		_, err := runStatement(queryCtx, s.client, statement, s.snapshot())
		close(stop)
		cancel()

		if err != nil {
			if errors.Is(err, hive.ErrCanceled) || errors.Is(err, context.Canceled) {
				return hive.ErrCanceled
			}
			return err
		}
		if database, ok := parseUse(statement); ok {
			s.currentDB = database
		}
	}
	return nil
}
