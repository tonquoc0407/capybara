// Command capybara is a terminal trace debugger for AI agents.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/ingest/claude"
	"github.com/tonquoc0407/capybara/internal/ingest/intake"
	"github.com/tonquoc0407/capybara/internal/ingest/otlp"
	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/tui"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		if !errors.Is(err, errDiverged) {
			fmt.Fprintf(os.Stderr, "capybara: %v\n", err)
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("capybara", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("db", "capybara.db", "database file")
	noContent := fs.Bool("no-content", false, "drop prompt, completion and tool content")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return usage(out)
		}
		return err
	}
	capture := !*noContent
	args = fs.Args()
	if len(args) == 0 {
		return receive(ctx, *dbPath, capture)
	}
	switch args[0] {
	case "import":
		return importFile(ctx, *dbPath, capture, args[1:])
	case "watch":
		return watch(ctx, *dbPath, capture, args[1:])
	case "diff":
		return diffCmd(ctx, *dbPath, args[1:], out)
	case "replay":
		return replayCmd(ctx, *dbPath, capture, args[1:], out)
	case "blame":
		return blameCmd(ctx, *dbPath, args[1:], out)
	case "export":
		return exportCmd(ctx, *dbPath, args[1:], out)
	case "check":
		return checkCmd(ctx, *dbPath, args[1:], out)
	case "serve":
		return fmt.Errorf("%s: not implemented", args[0])
	case "help":
		return usage(out)
	}
	return fmt.Errorf("unknown command %q", args[0])
}

// receive runs the OTLP receiver, the claude watcher when its log directory
// exists, and the TUI, until any of them quits.
func receive(ctx context.Context, dbPath string, capture bool) error {
	if ctx.Err() != nil {
		return nil
	}
	th, err := loadTheme()
	if err != nil {
		return err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	rcv := otlp.New(st, capture)
	if err := rcv.Listen(); err != nil {
		return err
	}
	claudeRoot := claude.DefaultRoot()
	if claudeRoot != "" {
		claudeNoticeOnce(claudeRoot)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errc := make(chan error, 3)
	workers := 2
	an, err := analyze.New(st)
	if err != nil {
		return err
	}
	go func() { errc <- rcv.Run(runCtx) }()
	go func() { errc <- an.Watch(runCtx) }()
	if claudeRoot != "" {
		workers++
		go func() { errc <- claude.Watch(runCtx, st, claudeRoot, capture) }()
	}
	errs := []error{tui.Run(runCtx, st, th, capture)}
	cancel()
	for range workers {
		errs = append(errs, <-errc)
	}
	return errors.Join(errs...)
}

// claudeNoticeOnce announces the auto-enabled claude watcher on first
// detection ever, tracked by a marker file in the config dir.
func claudeNoticeOnce(root string) {
	marker := filepath.Join(configDir(), "capybara", "claude-notice")
	if _, err := os.Stat(marker); err == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		return
	}
	fmt.Fprintf(os.Stderr, "watching claude code sessions in %s\n", root)
}

func watch(ctx context.Context, dbPath string, capture bool, args []string) error {
	if len(args) != 1 || args[0] != "claude" {
		return errors.New("usage: capybara watch claude")
	}
	root := claude.DefaultRoot()
	if root == "" {
		return errors.New("~/.claude/projects does not exist")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	an, err := analyze.New(st)
	if err != nil {
		return err
	}
	errc := make(chan error, 1)
	go func() { errc <- an.Watch(watchCtx) }()
	err = claude.Watch(watchCtx, st, root, capture)
	cancel()
	return errors.Join(err, <-errc)
}

func importFile(ctx context.Context, dbPath string, capture bool, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: capybara import <file>")
	}
	path := args[0]
	var importer func(context.Context, *store.Store, io.Reader, bool) error
	switch filepath.Ext(path) {
	case ".jsonl":
		importer = intake.ImportJSONL
	case ".json":
		importer = intake.ImportReplay
	default:
		return fmt.Errorf("%s: expected .json (agent-replay) or .jsonl (span per line)", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := importer(ctx, st, f, capture); err != nil {
		return fmt.Errorf("import %s: %w", path, err)
	}
	an, err := analyze.New(st)
	if err != nil {
		return err
	}
	if err := an.Sweep(ctx); err != nil {
		return fmt.Errorf("analyze %s: %w", path, err)
	}
	return nil
}

func usage(w io.Writer) error {
	_, err := io.WriteString(w, `usage: capybara [flags] [command]

Without a command, starts the TUI, the OTLP receiver, and the claude
watcher when ~/.claude/projects exists.

  watch    tail an external session source (claude)
  import   import a trace file (agent-replay json, span-per-line jsonl)
  diff     compare two runs
  replay   re-run a recorded run, optionally with an edited tool output
  blame    walk a run's final output back to its tainted source
  serve    serve the read-only web view
  export   export a run (--golden snapshots a fixture for CI)
  check    compare a run against a golden snapshot, non-zero on divergence
  help     print this message

Flags, before the command:

  -db path      database file (default capybara.db)
  -no-content   drop prompt, completion and tool content
`)
	return err
}
