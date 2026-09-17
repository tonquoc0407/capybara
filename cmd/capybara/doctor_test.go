package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/configpath"
	"github.com/tonquoc0407/capybara/internal/ingest/otlp"
)

func TestDoctorReportsSafeLocalDiagnostics(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "nested", "trace.db")
	grpc := freeAddress(t)
	httpAddr := freeAddress(t)
	var out strings.Builder
	if err := doctorCmd(context.Background(), db, httpAddr, []string{"-grpc", grpc}, &out); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"capybara " + version,
		"platform " + runtime.GOOS + "/" + runtime.GOARCH,
		"database " + absolutePath(db),
		"database directory " + filepath.Dir(absolutePath(db)) + " not found",
		"config theme " + configpath.File("config.toml"),
		"config pricing " + analyze.DefaultPricingPath(),
		"config mapping " + otlp.DefaultMappingPath(),
		"listener otlp grpc " + grpc + " available",
		"listener otlp http " + httpAddr + " available",
		"terminal stdin ",
		"terminal stdout ",
		"editor ",
		"python ",
		"gpu ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("doctor output missing %q:\n%s", want, got)
		}
	}
	if _, err := os.Stat(filepath.Dir(db)); !os.IsNotExist(err) {
		t.Fatalf("doctor created database directory: %v", err)
	}
	if _, err := os.Stat(db); !os.IsNotExist(err) {
		t.Fatalf("doctor created database: %v", err)
	}
}

func TestDoctorReportsUnavailableListenerAndInvalidEditor(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	t.Setenv("VISUAL", `"unclosed`)
	var out strings.Builder
	if err := doctorCmd(context.Background(), filepath.Join(t.TempDir(), "trace.db"), held.Addr().String(), nil, &out); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	for _, want := range []string{
		"listener otlp http " + held.Addr().String() + " unavailable",
		"editor invalid",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("doctor output missing %q:\n%s", want, out.String())
		}
	}
}

func TestDoctorRejectsArguments(t *testing.T) {
	err := doctorCmd(context.Background(), "capybara.db", "127.0.0.1:0", []string{"extra"}, nil)
	if err == nil || !strings.Contains(err.Error(), "usage: capybara") {
		t.Fatalf("doctor extra argument = %v, want usage error", err)
	}
}

func TestDirectoryStatus(t *testing.T) {
	dir := t.TempDir()
	if got := directoryStatus(dir); got != dir+" writable" {
		t.Errorf("writable directory = %q", got)
	}
	missing := filepath.Join(dir, "missing")
	if got := directoryStatus(missing); got != missing+" not found" {
		t.Errorf("missing directory = %q", got)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := directoryStatus(file); got != file+" not a directory" {
		t.Errorf("file directory = %q", got)
	}
}

func TestCommandStatus(t *testing.T) {
	if got := commandStatus("definitely-not-capybara-command"); got != "unavailable" {
		t.Errorf("missing command = %q", got)
	}
}

func freeAddress(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := lis.Addr().String()
	if err := lis.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}
