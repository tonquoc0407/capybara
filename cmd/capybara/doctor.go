package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/charmbracelet/x/term"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/configpath"
	"github.com/tonquoc0407/capybara/internal/ingest/otlp"
	"github.com/tonquoc0407/capybara/internal/tui"
)

// doctorCmd reports local prerequisites without opening the database or
// retaining a network listener. It is diagnostic: unavailable optional tools
// and busy ports are output, not command failures.
func doctorCmd(_ context.Context, dbPath, httpAddr string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	grpcAddr := fs.String("grpc", otlp.DefaultGRPCAddr, "OTLP gRPC address to check")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: capybara [flags] doctor [-grpc host:port]")
	}
	if httpAddr == "" {
		httpAddr = otlp.DefaultHTTPAddr
	}
	dbPath = absolutePath(dbPath)
	lines := []string{
		"capybara " + version,
		"platform " + runtime.GOOS + "/" + runtime.GOARCH,
		"database " + dbPath,
		"database directory " + directoryStatus(filepath.Dir(dbPath)),
		"config theme " + configpath.File("config.toml"),
		"config pricing " + analyze.DefaultPricingPath(),
		"config mapping " + otlp.DefaultMappingPath(),
		"terminal stdin " + fmt.Sprint(term.IsTerminal(os.Stdin.Fd())),
		"terminal stdout " + fmt.Sprint(term.IsTerminal(os.Stdout.Fd())),
		"listener otlp grpc " + listenerStatus(*grpcAddr),
		"listener otlp http " + listenerStatus(httpAddr),
		"editor " + editorStatus(),
		"python " + commandStatus("python3", "python"),
		"gpu " + commandStatus("nvidia-smi"),
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	return nil
}

func absolutePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// directoryStatus performs a harmless create/remove probe only in an existing
// directory. A missing database parent is useful to report, not create.
func directoryStatus(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return path + " not found"
		}
		return path + " error"
	}
	if !info.IsDir() {
		return path + " not a directory"
	}
	f, err := os.CreateTemp(path, ".capybara-doctor-*")
	if err != nil {
		return path + " not writable"
	}
	name := f.Name()
	closeErr := f.Close()
	removeErr := os.Remove(name)
	if closeErr != nil || removeErr != nil {
		return path + " not writable"
	}
	return path + " writable"
}

func listenerStatus(addr string) string {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return addr + " unavailable"
	}
	bound := lis.Addr().String()
	if err := lis.Close(); err != nil {
		return bound + " unavailable"
	}
	return bound + " available"
}

func editorStatus() string {
	name, _, err := tui.EditorCommand()
	if err != nil {
		return "invalid"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return name + " unavailable"
	}
	return path + " available"
}

func commandStatus(names ...string) string {
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path + " available"
		}
	}
	return "unavailable"
}
