package scenario

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func loadExample(t *testing.T, language string) Spec {
	t.Helper()
	path := filepath.Join("..", "..", "examples", "scenario", "weather-"+language+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var spec Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	spec.Command[1] = filepath.Join("..", "..", spec.Command[1])
	return spec
}

func TestPythonAndNodeMultiTurnMockFlow(t *testing.T) {
	for _, language := range []string{"python", "node"} {
		t.Run(language, func(t *testing.T) {
			spec := loadExample(t, language)
			if _, err := exec.LookPath(spec.Command[0]); err != nil {
				t.Skip(err)
			}
			result, err := Run(context.Background(), spec)
			if err != nil || !result.Passed || result.Steps != 3 || len(result.Turns) != 2 {
				t.Fatalf("result = %+v, %v", result, err)
			}
		})
	}
}

func TestScenarioFailsClosedOnUnmockedToolAndStepLimit(t *testing.T) {
	spec := loadExample(t, "python")
	if _, err := exec.LookPath(spec.Command[0]); err != nil {
		t.Skip(err)
	}
	spec.ToolMocks = nil
	result, err := Run(context.Background(), spec)
	if err != nil || result.Passed || !strings.Contains(result.Turns[0].Reason, "unmocked tool") {
		t.Fatalf("unmocked result = %+v, %v", result, err)
	}
	spec = loadExample(t, "python")
	spec.MaxSteps = 1
	result, err = Run(context.Background(), spec)
	if err != nil || result.Passed || result.Turns[0].Reason != "step limit reached" {
		t.Fatalf("step limit result = %+v, %v", result, err)
	}
}

func TestScenarioRejectsUnverifiableOrExcessCost(t *testing.T) {
	spec := Spec{
		Command: []string{"python3", "-c", `import json;print(json.dumps({"assistant":"ok","cost_usd":0.2}))`},
		Turns:   []Turn{{User: "hello"}}, MaxCostUSD: 0.1,
	}
	if _, err := exec.LookPath(spec.Command[0]); err != nil {
		t.Skip(err)
	}
	result, err := Run(context.Background(), spec)
	if err != nil || result.Passed || result.Turns[0].Reason != "reported cost budget exceeded" {
		t.Fatalf("cost result = %+v, %v", result, err)
	}
	spec.Command[2] = `import json;print(json.dumps({"assistant":"ok"}))`
	result, err = Run(context.Background(), spec)
	if err != nil || result.Passed || !strings.Contains(result.Turns[0].Reason, "did not report cost") {
		t.Fatalf("unknown cost result = %+v, %v", result, err)
	}
}
