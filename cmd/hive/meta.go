package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func (s *replState) handleMeta(ctx context.Context, line string) (bool, error) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return false, nil
	}
	command := strings.ToLower(fields[0])
	args := fields[1:]

	switch command {
	case "!quit", "!exit", "!q":
		return true, nil
	case "!help", "!h", "!?":
		printMetaHelp(os.Stderr)
		return false, nil
	case "!schemas":
		if len(args) > 0 {
			return false, fmt.Errorf("usage: !schemas")
		}
		return false, s.executeMetaSQL(ctx, "SHOW DATABASES")
	case "!tables":
		pattern := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		sql := "SHOW TABLES"
		if pattern != "" {
			sql += " LIKE '" + strings.ReplaceAll(pattern, "'", "''") + "'"
		}
		return false, s.executeMetaSQL(ctx, sql)
	case "!columns", "!describe":
		if len(args) == 0 {
			return false, fmt.Errorf("usage: %s <table>", command)
		}
		return false, s.executeMetaSQL(ctx, "DESCRIBE "+quoteTable(strings.Join(args, " ")))
	case "!outputformat":
		if len(args) != 1 {
			return false, fmt.Errorf("usage: !outputformat table|vertical|csv|tsv|json|jsonl")
		}
		return false, s.handleSet([]string{"outputformat", args[0]})
	case "!delimiter":
		if len(args) != 1 {
			return false, fmt.Errorf("usage: !delimiter <string>")
		}
		if err := validateDelimiter(args[0]); err != nil {
			return false, err
		}
		s.delimiter = args[0]
		return false, nil
	case "!run", "!source":
		path := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		if path == "" {
			return false, fmt.Errorf("usage: %s <file>", command)
		}
		return false, s.runFile(ctx, path)
	case "!history":
		s.printHistory()
		return false, nil
	case "!record":
		arg := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		return false, s.handleRecord(arg)
	case "!dbinfo":
		return false, s.printDBInfo(ctx)
	case "!set":
		return false, s.handleSet(args)
	case "!fetchsize":
		if len(args) == 0 {
			fmt.Fprintf(os.Stderr, "fetchsize = %d\n", s.fetchSize)
			return false, nil
		}
		n, err := strconv.Atoi(args[0])
		if err != nil || n <= 0 {
			return false, fmt.Errorf("!fetchsize requires a positive integer")
		}
		s.fetchSize = n
		s.client.SetFetchSize(n)
		return false, nil
	case "!verbose":
		if len(args) == 0 {
			fmt.Fprintf(os.Stderr, "verbose = %v\n", s.verbose)
			return false, nil
		}
		value, err := parseOnOff(args[0])
		if err != nil {
			return false, err
		}
		s.verbose = value
		s.applyVerbose()
		return false, nil
	case "!timing":
		if len(args) == 0 {
			fmt.Fprintf(os.Stderr, "timing = %v\n", s.timing)
			return false, nil
		}
		value, err := parseOnOff(args[0])
		if err != nil {
			return false, err
		}
		s.timing = value
		return false, nil
	default:
		return false, fmt.Errorf("unknown command %q (try !help)", fields[0])
	}
}

func (s *replState) executeMetaSQL(ctx context.Context, sql string) error {
	return s.runQuery(ctx, sql)
}

func (s *replState) runFile(ctx context.Context, path string) error {
	if s.runDepth >= 10 {
		return fmt.Errorf("!run nesting is too deep (max 10)")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	s.runDepth++
	defer func() { s.runDepth-- }()
	return s.runScript(ctx, string(data), s.scriptForce)
}

func (s *replState) printHistory() {
	if s.opts.historyFile == "" {
		fmt.Fprintln(os.Stderr, "history is disabled")
		return
	}
	data, err := os.ReadFile(s.opts.historyFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "history: %v\n", err)
		return
	}
	if len(data) == 0 {
		return
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	for i, line := range lines {
		fmt.Fprintf(os.Stderr, "%4d  %s\n", i+1, line)
	}
}

func (s *replState) handleRecord(arg string) error {
	if arg == "" {
		if s.record != nil {
			fmt.Fprintf(os.Stderr, "record = %s\n", s.record.Name())
		} else {
			fmt.Fprintln(os.Stderr, "record = off")
		}
		return nil
	}
	if strings.EqualFold(arg, "off") {
		s.stopRecording()
		return nil
	}
	if s.record != nil {
		return fmt.Errorf("already recording to %s (use !record off)", s.record.Name())
	}
	f, err := os.Create(arg)
	if err != nil {
		return err
	}
	s.record = f
	s.out = io.MultiWriter(os.Stdout, f)
	return nil
}

func (s *replState) printDBInfo(ctx context.Context) error {
	name, version, err := s.client.ServerInfo(ctx)
	if err != nil {
		return err
	}
	if name == "" {
		name = "(unknown)"
	}
	if version == "" {
		version = "(unknown)"
	}
	cfg := s.client.Config()
	fmt.Fprintf(os.Stderr, "server = %s\n", name)
	fmt.Fprintf(os.Stderr, "version = %s\n", version)
	fmt.Fprintf(os.Stderr, "protocol = %s\n", s.client.Protocol())
	fmt.Fprintf(os.Stderr, "auth = %s\n", cfg.Auth)
	fmt.Fprintf(os.Stderr, "host = %s:%d\n", cfg.Host, cfg.Port)
	fmt.Fprintf(os.Stderr, "database = %s\n", displayDatabase(s.currentDB))
	return nil
}

func (s *replState) handleSet(args []string) error {
	if len(args) == 0 {
		printSettings(s)
		return nil
	}
	key := strings.ToLower(args[0])
	value := strings.Join(args[1:], " ")
	switch key {
	case "outputformat", "format":
		format := strings.ToLower(value)
		switch format {
		case "table", "vertical", "csv", "tsv", "json", "jsonl":
			s.format = format
			return nil
		default:
			return fmt.Errorf("unsupported output format %q", value)
		}
	case "delimiter":
		if err := validateDelimiter(value); err != nil {
			return err
		}
		s.delimiter = value
		return nil
	case "binaryformat", "binary-format":
		format := strings.ToLower(value)
		switch format {
		case "text", "base64", "hex":
			s.opts.binaryFormat = format
			return nil
		default:
			return fmt.Errorf("unsupported binary format %q", value)
		}
	case "fetchsize":
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return fmt.Errorf("fetchsize must be a positive integer")
		}
		s.fetchSize = n
		s.client.SetFetchSize(n)
		return nil
	case "verbose":
		value, err := parseOnOff(value)
		if err != nil {
			return err
		}
		s.verbose = value
		s.applyVerbose()
		return nil
	case "timing":
		value, err := parseOnOff(value)
		if err != nil {
			return err
		}
		s.timing = value
		return nil
	case "header":
		value, err := parseOnOff(value)
		if err != nil {
			return err
		}
		s.noHeader = !value
		return nil
	case "nullstring":
		s.nullString = value
		return nil
	default:
		return fmt.Errorf("unknown setting %q", args[0])
	}
}

func (s *replState) applyVerbose() {
	if s.verbose {
		s.client.SetLogf(func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		})
		return
	}
	s.client.SetLogf(nil)
}

func parseOnOff(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "true", "1", "yes":
		return true, nil
	case "off", "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("expected on or off, got %q", value)
	}
}

func printSettings(s *replState) {
	fmt.Fprintf(os.Stderr, "outputformat = %s\n", s.format)
	fmt.Fprintf(os.Stderr, "fetchsize = %d\n", s.fetchSize)
	fmt.Fprintf(os.Stderr, "verbose = %v\n", s.verbose)
	fmt.Fprintf(os.Stderr, "timing = %v\n", s.timing)
	fmt.Fprintf(os.Stderr, "header = %v\n", !s.noHeader)
	fmt.Fprintf(os.Stderr, "nullstring = %s\n", s.nullString)
	fmt.Fprintf(os.Stderr, "delimiter = %s\n", s.delimiter)
	fmt.Fprintf(os.Stderr, "binaryformat = %s\n", s.opts.binaryFormat)
}

func quoteTable(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "`") {
		return name
	}
	parts := strings.Split(name, ".")
	for i, part := range parts {
		parts[i] = "`" + strings.ReplaceAll(strings.TrimSpace(part), "`", "``") + "`"
	}
	return strings.Join(parts, ".")
}

func printMetaHelp(w *os.File) {
	fmt.Fprint(w, `Meta commands:
  !schemas              SHOW DATABASES
  !tables [pattern]     SHOW TABLES [LIKE 'pattern']
  !columns <table>      DESCRIBE <table> (alias: !describe)
  !outputformat FMT     table, vertical, csv, tsv, json or jsonl
  !delimiter STR        set the statement delimiter (default ;)
  !run <file>           execute a script (alias: !source)
  !history              show command history
  !record [file|off]    record result output to a file
  !dbinfo               show server, protocol and authentication details
  !set [key value]      show or change outputformat, fetchsize, verbose,
                        timing, header, nullstring, delimiter and binaryformat
  !fetchsize N          rows per fetch
  !verbose on|off
  !timing on|off
  !help                 this help
  !quit                 exit
`)
}
