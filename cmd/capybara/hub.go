package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/tonquoc0407/capybara/internal/hub"
	"github.com/tonquoc0407/capybara/internal/store"
)

func hubCmd(ctx context.Context, dbPath string, allowContent bool, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: capybara hub <serve|token|audit|backup|restore>")
	}
	if args[0] == "serve" {
		fs := flag.NewFlagSet("hub serve", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		addr := fs.String("addr", "127.0.0.1:7655", "listen address")
		capture := fs.Bool("capture-content", false, "store prompt, response and log bodies")
		retention := fs.Duration("retention", 0, "delete unpinned runs older than this duration (0 keeps all)")
		redact := fs.String("redact-regex", "", "replace matches in captured content and attributes before storage")
		alertWebhook := fs.String("alert-webhook", "", "POST threshold alerts to this URL")
		alertProject := fs.String("alert-project", "default", "project monitored by alert thresholds")
		alertWindow := fs.Duration("alert-window", 10*time.Minute, "rolling alert window")
		alertInterval := fs.Duration("alert-interval", time.Minute, "alert check interval")
		alertErrors := fs.Int64("alert-errors", 0, "alert when errors in window reach this count")
		alertP95 := fs.Duration("alert-p95", 0, "alert when p95 run latency exceeds this duration")
		alertCost := fs.Float64("alert-cost-usd", 0, "alert when known run cost exceeds this amount")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("usage: capybara hub serve [-addr host:port] [-capture-content]")
		}
		var pattern *regexp.Regexp
		if *redact != "" {
			var err error
			pattern, err = regexp.Compile(*redact)
			if err != nil {
				return fmt.Errorf("invalid redact-regex: %w", err)
			}
		}
		st, err := store.Open(dbPath)
		if err != nil {
			return err
		}
		defer st.Close()
		var alerts *hub.AlertConfig
		if *alertWebhook != "" {
			alerts = &hub.AlertConfig{
				Project: *alertProject, WebhookURL: *alertWebhook,
				Window: *alertWindow, Interval: *alertInterval, MinErrors: *alertErrors,
				MaxP95Latency: *alertP95, MaxCostUSD: *alertCost,
			}
		}
		return (hub.Server{Store: st, CaptureContent: *capture && allowContent, Retention: *retention, RedactPattern: pattern, Alerts: alerts}).Serve(ctx, *addr, out)
	}
	if args[0] == "restore" {
		if len(args) != 2 {
			return errors.New("usage: capybara hub restore <backup.db>")
		}
		previous, err := store.RestoreBackup(ctx, dbPath, args[1])
		if err != nil {
			return err
		}
		if previous != "" {
			_, err = fmt.Fprintln(out, previous)
		}
		return err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	if args[0] == "backup" {
		if len(args) != 2 {
			return errors.New("usage: capybara hub backup <file.db>")
		}
		if err := st.Backup(ctx, args[1]); err != nil {
			return err
		}
		_, err := fmt.Fprintln(out, args[1])
		return err
	}
	if args[0] == "audit" {
		fs := flag.NewFlagSet("hub audit", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		project := fs.String("project", "default", "project")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("usage: capybara hub audit [-project name]")
		}
		events, err := st.AuditEvents(ctx, *project, 100)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(events)
	}
	if args[0] != "token" || len(args) < 2 {
		return errors.New("usage: capybara hub token <create|list|revoke>")
	}
	switch args[1] {
	case "create":
		fs := flag.NewFlagSet("hub token create", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		project := fs.String("project", "default", "project")
		role := fs.String("role", "viewer", "admin, editor, or viewer")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("usage: capybara hub token create [-project name] [-role admin|editor|viewer]")
		}
		token, secret, err := st.CreateHubToken(ctx, *project, *role)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "%s %s\n", token.ID, secret)
		return err
	case "list":
		fs := flag.NewFlagSet("hub token list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		project := fs.String("project", "default", "project")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("usage: capybara hub token list [-project name]")
		}
		tokens, err := st.ListHubTokens(ctx, *project)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(tokens)
	case "revoke":
		if len(args) != 3 {
			return errors.New("usage: capybara hub token revoke <token-id>")
		}
		return st.RevokeHubToken(ctx, args[2])
	}
	return errors.New("usage: capybara hub token <create|list|revoke>")
}
