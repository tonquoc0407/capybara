package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"regexp"

	"github.com/tonquoc0407/capybara/internal/proxy"
	"github.com/tonquoc0407/capybara/internal/store"
)

func proxyCmd(ctx context.Context, dbPath string, allowContent bool, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "serve" {
		return errors.New("usage: capybara proxy serve -upstream URL [options]")
	}
	fs := flag.NewFlagSet("proxy serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "127.0.0.1:7656", "loopback listen address")
	upstream := fs.String("upstream", "", "full upstream /v1/chat/completions URL")
	keyEnv := fs.String("key-env", "OPENAI_API_KEY", "environment variable containing upstream API key")
	capture := fs.Bool("capture-content", false, "store request and response JSON")
	deny := fs.String("deny-regex", "", "reject requests or responses matching this pattern")
	budget := fs.Float64("max-cost-usd", 0, "soft session budget using provider-reported token usage")
	inputPrice := fs.Float64("input-per-million", 0, "configured USD per million input tokens")
	outputPrice := fs.Float64("output-per-million", 0, "configured USD per million output tokens")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: capybara proxy serve -upstream URL [options]")
	}
	var pattern *regexp.Regexp
	if *deny != "" {
		var err error
		pattern, err = regexp.Compile(*deny)
		if err != nil {
			return err
		}
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	s := &proxy.Server{
		Store: st, UpstreamURL: *upstream, APIKey: os.Getenv(*keyEnv),
		CaptureContent: *capture && allowContent, DenyPattern: pattern,
		MaxCostUSD: *budget, InputPerM: *inputPrice, OutputPerM: *outputPrice,
	}
	return s.Serve(ctx, *addr, out)
}
