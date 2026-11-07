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

	delimiter   string
	vars        *variables
	out         io.Writer
	record      *os.File
	runDepth    int
	scriptForce bool
	sigCh       <-chan os.Signal
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
		delimiter:  opts.delimiter,
		vars:       opts.vars,
		out:        os.Stdout,
	}
	if state.currentDB == "" {
		state.currentDB = "default"
	}
	state.applyVerbose()
	return state
}

func runREPL(ctx context.Context, state *replState) int {
	opts := state.opts

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
	defer state.stopRecording()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	state.sigCh = sigCh

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
				if err := state.executeScript(ctx, pending.String()); err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				}
			}
			return 0
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}

		line, err = state.vars.expand(line)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			continue
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
		if !sqlsplit.CompleteWith(pending.String(), state.delimiter) {
			continue
		}
		script := pending.String()
		pending.Reset()
		if err := state.executeScript(ctx, script); err != nil {
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
	opts.delimiter = s.delimiter
	return &opts
}

func (s *replState) executeScript(ctx context.Context, script string) error {
	err := s.executeStatements(ctx, script)
	if errors.Is(err, hive.ErrCanceled) || errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "Query canceled")
		return nil
	}
	return err
}

func (s *replState) executeStatements(ctx context.Context, script string) error {
	for _, statement := range sqlsplit.SplitWith(script, s.delimiter) {
		if err := s.executeOne(ctx, statement); err != nil {
			if errors.Is(err, hive.ErrCanceled) || errors.Is(err, context.Canceled) {
				return hive.ErrCanceled
			}
			return err
		}
	}
	return nil
}

// executeOne runs a single statement and updates the prompt database.
func (s *replState) executeOne(ctx context.Context, statement string) error {
	if s.verbose {
		fmt.Fprintf(os.Stderr, "-- statement: %s\n", oneLine(statement))
	}
	if err := s.runQuery(ctx, statement); err != nil {
		return err
	}
	if database, ok := parseUse(statement); ok {
		s.currentDB = database
	}
	return nil
}

// executeScriptStatements runs every statement in script and, when force is
// set, continues past errors. It reports whether any statement failed.
func (s *replState) executeScriptStatements(ctx context.Context, script string, force bool) (bool, error) {
	failed := false
	for _, statement := range sqlsplit.SplitWith(script, s.delimiter) {
		err := s.executeOne(ctx, statement)
		if err == nil {
			continue
		}
		if errors.Is(err, hive.ErrCanceled) || errors.Is(err, context.Canceled) {
			return failed, err
		}
		if !force {
			return failed, err
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		failed = true
	}
	return failed, nil
}

func (s *replState) runQuery(ctx context.Context, statement string) error {
	queryCtx, cancel := context.WithCancel(ctx)
	stop := make(chan struct{})
	if s.sigCh != nil {
		go func() {
			select {
			case <-s.sigCh:
				cancel()
			case <-stop:
			}
		}()
	}

	s.client.SetFetchSize(s.fetchSize)
	_, err := runStatement(queryCtx, s.client, statement, s.snapshot(), s.out)
	close(stop)
	cancel()
	return err
}

func (s *replState) stopRecording() {
	if s.record == nil {
		return
	}
	_ = s.record.Close()
	s.record = nil
	s.out = os.Stdout
}
