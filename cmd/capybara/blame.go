package main

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/tonquoc0407/capybara/internal/analyze"
	"github.com/tonquoc0407/capybara/internal/store"
)

func blameCmd(ctx context.Context, dbPath string, args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: capybara blame <run>")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	run, err := st.ResolveRunID(ctx, args[0])
	if err != nil {
		return err
	}
	chain, err := analyze.Blame(ctx, st, run)
	if err != nil {
		return err
	}
	return printBlame(out, chain)
}

// printBlame renders the tainted path most-recent first, the root cause last.
// A run whose output carries no finding prints nothing.
func printBlame(out io.Writer, chain *analyze.BlameChain) error {
	if len(chain.Hops) == 0 {
		return nil
	}
	w := &errWriter{w: out}
	w.printf("blame %s\n", shortID(chain.RunID))
	for i, hop := range chain.Hops {
		tags := []string{}
		if i == 0 {
			tags = append(tags, "final output")
		}
		if hop.Root {
			tags = append(tags, "root")
		}
		right := blameReason(hop)
		if len(tags) > 0 {
			tag := "(" + strings.Join(tags, ", ") + ")"
			if right == "" {
				right = tag
			} else {
				right = tag + " " + right
			}
		}
		label := strings.Repeat("  ", hop.Depth) + spanKindName(hop.Span)
		w.printf("  %-28s %s\n", truncate(label, 28), right)
	}
	return w.err
}

func blameReason(hop analyze.BlameHop) string {
	if len(hop.Findings) == 0 {
		if hop.Span.Status == "error" {
			return "error"
		}
		return ""
	}
	parts := make([]string, 0, len(hop.Findings))
	for _, f := range hop.Findings {
		parts = append(parts, analyze.FindingSummary(f))
	}
	return strings.Join(parts, "; ")
}

func spanKindName(sp store.Span) string {
	name := sp.Name
	if sp.Attrs.ToolName != "" {
		name = sp.Attrs.ToolName
	}
	switch sp.Kind {
	case store.KindLLM, store.KindTool, store.KindRetrieval:
		return string(sp.Kind) + " " + name
	}
	return name
}
