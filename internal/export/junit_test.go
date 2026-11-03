package export

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestExperimentJUnitReportsRegressionAndPass(t *testing.T) {
	result := store.ExperimentComparison{
		DatasetID: "data", ScoreName: "quality", Items: 2,
		Baseline: 0.8, Candidate: 0.6, Delta: -0.2,
	}
	for _, trial := range []struct {
		threshold float64
		failures  string
	}{{0, `failures="1"`}, {-0.3, `failures="0"`}} {
		path := filepath.Join(t.TempDir(), "result.xml")
		if err := WriteExperimentJUnit(path, result, trial.threshold); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), trial.failures) {
			t.Fatalf("wrong JUnit result: %s", raw)
		}
		if err := WriteExperimentJUnit(path, result, trial.threshold); err == nil {
			t.Fatal("overwrote artifact")
		}
	}
}
