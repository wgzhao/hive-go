package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"time"

	"github.com/pborman/getopt"
	"github.com/wgzhao/hive-go"
	"golang.org/x/term"
)

// version is injected at build time with -ldflags "-X main.version=...".
// Binaries installed with `go install module@version` receive no ldflags, so
// fall back to the module version embedded in the Go build info.
var version = "dev"

func versionString() string {
	if version != "" && version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}

type cliOptions struct {
	url      string
	username string
	password string
	auth     string
	protocol string

	host     string
	port     int
	database string

	execute string
	file    string

	fetchSize      int
	format         string
	nullString     string
	noHeader       bool
	verbose        bool
	timing         bool
	silent         bool
	connectTimeout time.Duration
	socketTimeout  time.Duration
	queryTimeout   int

	historyFile string
	noHistory   bool

	showVersion bool
	showHelp    bool
}

func main() {
	os.Exit(runCLI(os.Args[1:]))
}

func runCLI(args []string) int {
	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hive: %v\n", err)
		printUsage(os.Stderr)
		return 2
	}
	if opts.showHelp {
		printUsage(os.Stdout)
		return 0
	}
	if opts.showVersion {
		fmt.Printf("hive %s\n", versionString())
		return 0
	}

	resolved, err := resolveOptions(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hive: %v\n", err)
		return 2
	}
	if resolved.execute != "" && resolved.file != "" {
		fmt.Fprintln(os.Stderr, "hive: --execute and --file are mutually exclusive")
		return 2
	}

	client, err := hive.Connect(context.Background(), resolved.cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	defer client.Close()

	if resolved.verbose {
		fmt.Fprintf(os.Stderr, "Connected to %s:%d, database %s, protocol %s\n",
			resolved.cfg.Host, resolved.cfg.Port, displayDatabase(resolved.cfg.Database), client.Protocol())
	}

	var script string
	switch {
	case resolved.execute != "":
		script = resolved.execute
	case resolved.file != "":
		data, err := readScript(resolved.file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		script = data
	case !isTerminal(os.Stdin):
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: read stdin: %v\n", err)
			return 1
		}
		script = string(data)
	default:
		return runREPL(context.Background(), client, resolved)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := runStream(ctx, client, script, resolved); err != nil {
		if errors.Is(err, hive.ErrCanceled) || errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "Query canceled")
			return 130
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func parseArgs(args []string) (*cliOptions, error) {
	o := &cliOptions{
		format:         "table",
		nullString:     "NULL",
		connectTimeout: 10 * time.Second,
	}

	set := getopt.New()
	set.SetProgram("hive")

	url := set.StringLong("url", 'u', "", "hive2 URL")
	username := set.StringLong("username", 'n', "", "username")
	password := set.StringLong("password", 'p', "", "password")
	auth := set.StringLong("auth", 0, "", "auth mode")
	protocol := set.StringLong("protocol", 0, "", "protocol version")
	host := set.StringLong("host", 0, "", "server host")
	port := set.IntLong("port", 0, 0, "server port")
	database := set.StringLong("database", 'd', "", "database")
	execute := set.StringLong("execute", 'e', "", "SQL to execute")
	file := set.StringLong("file", 'f', "", "SQL file")
	fetchSize := set.IntLong("fetch-size", 0, 0, "fetch size")
	format := set.StringLong("output-format", 0, "table", "output format")
	nullString := set.StringLong("null-string", 0, "NULL", "NULL text")
	noHeader := set.BoolLong("no-header", 0, "omit header")
	verbose := set.BoolLong("verbose", 0, "verbose output")
	timing := set.BoolLong("timing", 0, "print timing")
	silent := set.BoolLong("silent", 0, "suppress status")
	connectTimeout := set.DurationLong("connect-timeout", 0, 10*time.Second, "connect timeout")
	socketTimeout := set.DurationLong("socket-timeout", 0, 0, "socket timeout")
	queryTimeout := set.IntLong("query-timeout", 0, 0, "query timeout seconds")
	historyFile := set.StringLong("history-file", 0, "", "history file")
	noHistory := set.BoolLong("no-history", 0, "disable history")
	help := set.BoolLong("help", 'h', "print help")
	showVersion := set.BoolLong("version", 'v', "print version")

	if err := set.Getopt(append([]string{"hive"}, args...), nil); err != nil {
		return nil, err
	}
	if rest := set.Args(); len(rest) > 0 {
		return nil, fmt.Errorf("unexpected argument %q (use -e or -f)", rest[0])
	}

	o.url = *url
	o.username = *username
	o.password = *password
	o.auth = *auth
	o.protocol = *protocol
	o.host = *host
	o.port = *port
	o.database = *database
	o.execute = *execute
	o.file = *file
	o.fetchSize = *fetchSize
	o.format = *format
	o.nullString = *nullString
	o.noHeader = *noHeader
	o.verbose = *verbose
	o.timing = *timing
	o.silent = *silent
	o.connectTimeout = *connectTimeout
	o.socketTimeout = *socketTimeout
	o.queryTimeout = *queryTimeout
	o.historyFile = *historyFile
	o.noHistory = *noHistory
	o.showHelp = *help
	o.showVersion = *showVersion
	return o, nil
}

func readScript(path string) (string, error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(data), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}

func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func displayDatabase(db string) string {
	if db == "" {
		return "default"
	}
	return db
}
