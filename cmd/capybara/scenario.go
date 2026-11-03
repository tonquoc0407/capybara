package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/tonquoc0407/capybara/internal/scenario"
)

var errScenarioFailed = errors.New("scenario failed")

func scenarioCmd(ctx context.Context, args []string, out io.Writer) error {
	if len(args) != 2 || args[0] != "run" {
		return errors.New("usage: capybara scenario run <spec.json>")
	}
	raw, err := os.ReadFile(args[1])
	if err != nil {
		return err
	}
	var spec scenario.Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return fmt.Errorf("scenario JSON: %w", err)
	}
	result, err := scenario.Run(ctx, spec)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(out).Encode(result); err != nil {
		return err
	}
	if !result.Passed {
		return errScenarioFailed
	}
	return nil
}
