package main

import (
	"context"
	"fmt"
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
	case "!columns":
		if len(args) == 0 {
			return false, fmt.Errorf("usage: !columns <table>")
		}
		return false, s.executeMetaSQL(ctx, "DESCRIBE "+quoteTable(strings.Join(args, " ")))
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
	s.client.SetFetchSize(s.fetchSize)
	_, err := runStatement(ctx, s.client, sql, s.snapshot())
	return err
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
		case "table", "vertical", "csv", "tsv", "json":
			s.format = format
			return nil
		default:
			return fmt.Errorf("unsupported output format %q", value)
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
  !columns <table>      DESCRIBE <table>
  !set [key value]      show or change outputformat, fetchsize, verbose,
                        timing, header and nullstring
  !fetchsize N          rows per fetch
  !verbose on|off
  !timing on|off
  !help                 this help
  !quit                 exit
`)
}
