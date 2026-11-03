package export

import (
	"encoding/xml"
	"fmt"
	"math"
	"os"

	"github.com/tonquoc0407/capybara/internal/store"
)

// WriteExperimentJUnit emits a single CI test case for a paired comparison.
func WriteExperimentJUnit(path string, result store.ExperimentComparison, minDelta float64) error {
	if math.IsInf(minDelta, 0) || math.IsNaN(minDelta) {
		return fmt.Errorf("JUnit export requires a finite minimum delta")
	}
	type failure struct {
		Message string `xml:"message,attr"`
		Text    string `xml:",chardata"`
	}
	type testCase struct {
		Name    string   `xml:"name,attr"`
		Class   string   `xml:"classname,attr"`
		Failure *failure `xml:"failure,omitempty"`
		Output  string   `xml:"system-out"`
	}
	type suite struct {
		XMLName  xml.Name `xml:"testsuite"`
		Name     string   `xml:"name,attr"`
		Tests    int      `xml:"tests,attr"`
		Failures int      `xml:"failures,attr"`
		Case     testCase `xml:"testcase"`
	}
	pass := result.Delta >= minDelta
	artifact := suite{Name: "capybara experiment", Tests: 1,
		Case: testCase{Name: result.ScoreName, Class: "capybara.experiment",
			Output: fmt.Sprintf("dataset=%s items=%d baseline=%.6f candidate=%.6f delta=%.6f min_delta=%.6f",
				result.DatasetID, result.Items, result.Baseline, result.Candidate, result.Delta, minDelta)}}
	if !pass {
		artifact.Failures = 1
		artifact.Case.Failure = &failure{Message: "experiment regression",
			Text: fmt.Sprintf("delta %.6f is below minimum %.6f", result.Delta, minDelta)}
	}
	raw, err := xml.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append([]byte(xml.Header), append(raw, '\n')...)); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}
