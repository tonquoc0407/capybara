package export

import (
	"encoding/json"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestSARIFStructure(t *testing.T) {
	findings := []store.Finding{
		{RunID: "run1", SpanID: "spanA", Type: "improvised", Severity: "error", Detail: `{"tool":"lookup"}`},
		{RunID: "run1", SpanID: "spanB", Type: "tool_error", Severity: "warning"},
		{RunID: "run2", SpanID: "spanC", Type: "improvised", Severity: "error", Detail: `{"tool":"lookup"}`},
	}
	raw, err := SARIF(findings, "9.9.9")
	if err != nil {
		t.Fatalf("SARIF: %v", err)
	}
	var log struct {
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name            string `json:"name"`
					SemanticVersion string `json:"semanticVersion"`
					Rules           []struct {
						ID string `json:"id"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID    string                `json:"ruleId"`
				Level     string                `json:"level"`
				Message   struct{ Text string } `json:"message"`
				Locations []struct {
					LogicalLocations []struct {
						FullyQualifiedName string `json:"fullyQualifiedName"`
					} `json:"logicalLocations"`
				} `json:"locations"`
				PartialFingerprints map[string]string `json:"partialFingerprints"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(raw, &log); err != nil {
		t.Fatalf("unmarshal SARIF: %v", err)
	}
	if log.Version != "2.1.0" {
		t.Errorf("version = %q", log.Version)
	}
	run := log.Runs[0]
	if run.Tool.Driver.Name != "capybara" || run.Tool.Driver.SemanticVersion != "9.9.9" {
		t.Errorf("driver = %+v", run.Tool.Driver)
	}
	// One rule per distinct type, sorted.
	if len(run.Tool.Driver.Rules) != 2 || run.Tool.Driver.Rules[0].ID != "improvised" {
		t.Errorf("rules = %+v", run.Tool.Driver.Rules)
	}
	if len(run.Results) != 3 {
		t.Fatalf("results = %d, want 3", len(run.Results))
	}
	r := run.Results[0]
	if r.Level != "error" || r.RuleID != "improvised" {
		t.Errorf("result[0] = %+v", r)
	}
	if r.Locations[0].LogicalLocations[0].FullyQualifiedName != "run1/spanA" {
		t.Errorf("logical location = %+v", r.Locations[0])
	}
	if r.PartialFingerprints["capybara/v1"] == "" {
		t.Errorf("missing fingerprint: %+v", r.PartialFingerprints)
	}
	// Same type, different span must fingerprint differently.
	if run.Results[0].PartialFingerprints["capybara/v1"] == run.Results[2].PartialFingerprints["capybara/v1"] {
		t.Error("distinct findings share a fingerprint")
	}
}

func TestSARIFLevelFallsBackToWarning(t *testing.T) {
	raw, _ := SARIF([]store.Finding{{RunID: "r", SpanID: "s", Type: "x", Severity: "bogus"}}, "")
	var log struct {
		Runs []struct {
			Results []struct {
				Level string `json:"level"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(raw, &log); err != nil {
		t.Fatal(err)
	}
	if log.Runs[0].Results[0].Level != "warning" {
		t.Errorf("level = %q, want warning", log.Runs[0].Results[0].Level)
	}
}
