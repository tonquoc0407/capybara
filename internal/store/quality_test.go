package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestDatasetScoreExperimentComparison(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "quality.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Now().UTC()
	for _, id := range []string{"baseline", "candidate", "foreign"} {
		project := "checkout"
		if id == "foreign" {
			project = "other"
		}
		sp := Span{ID: id + "-span", RunID: id, Kind: KindAgent, Name: "answer", Status: "ok", StartedAt: at, EndedAt: at.Add(time.Second)}
		if err := st.WriteBatch(ctx, Batch{
			Source: "test", RunContexts: map[string]RunContext{id: {Project: project}},
			Spans: []Span{sp}, Contents: []Content{
				{SpanID: sp.ID, Role: "user", Seq: 0, Body: "question", MediaType: "text/plain"},
				{SpanID: sp.ID, Role: "assistant", Seq: 1, Body: "answer", MediaType: "text/plain"},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	d, err := st.CreateDataset(ctx, "checkout", "failed charges")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddDatasetItem(ctx, d.ID, "foreign"); err == nil {
		t.Fatal("accepted cross-project source")
	}
	item, err := st.AddDatasetItem(ctx, d.ID, "baseline")
	if err != nil || item.InputJSON != `"question"` || item.ExpectedJSON != `"answer"` {
		t.Fatalf("item: %+v, %v", item, err)
	}
	revised, err := st.SetDatasetExpected(ctx, d.ID, item.ID, "corrected answer")
	if err != nil || revised.ExpectedJSON != `"corrected answer"` {
		t.Fatalf("expected revision: %+v, %v", revised, err)
	}
	source, err := st.Contents(ctx, "baseline-span")
	if err != nil || source[1].Body != "answer" {
		t.Fatalf("source evidence changed: %+v, %v", source, err)
	}
	left, err := st.CreateExperiment(ctx, d.ID, "before")
	if err != nil {
		t.Fatal(err)
	}
	right, err := st.CreateExperiment(ctx, d.ID, "after")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetDatasetExpected(ctx, d.ID, item.ID, "changed after experiment"); !errors.Is(err, ErrDatasetFrozen) {
		t.Fatalf("mutable experiment case: %v", err)
	}
	if _, err := st.AddDatasetItem(ctx, d.ID, "candidate"); !errors.Is(err, ErrDatasetFrozen) {
		t.Fatalf("mutable experiment dataset: %v", err)
	}
	frozen, err := st.GetDataset(ctx, d.ID)
	if err != nil || !frozen.Frozen {
		t.Fatalf("dataset not marked frozen: %+v, %v", frozen, err)
	}
	clone, err := st.CloneDataset(ctx, d.ID, "revised cases")
	if err != nil || clone.Project != d.Project || clone.Frozen {
		t.Fatalf("clone: %+v, %v", clone, err)
	}
	clonedItems, err := st.ListDatasetItems(ctx, clone.ID)
	if err != nil || len(clonedItems) != 1 || clonedItems[0].ID == item.ID || clonedItems[0].SourceRunID != item.SourceRunID || clonedItems[0].ExpectedJSON != revised.ExpectedJSON {
		t.Fatalf("cloned evidence: %+v, %v", clonedItems, err)
	}
	if _, err := st.SetDatasetExpected(ctx, clone.ID, clonedItems[0].ID, "new version"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetDatasetExpected(ctx, clone.ID, item.ID, "wrong item"); err == nil {
		t.Fatal("edited a case outside its dataset")
	}
	if _, err := st.AddScore(ctx, "baseline", item.ID, "quality", 0.4, "human", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddScore(ctx, "candidate", item.ID, "quality", 0.9, "human", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CompareExperiments(ctx, left.ID, right.ID, "quality"); err == nil {
		t.Fatal("accepted incomplete comparison")
	}
	if err := st.RecordExperimentResult(ctx, left.ID, item.ID, "baseline", "quality"); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordExperimentResult(ctx, right.ID, item.ID, "candidate", "quality"); err != nil {
		t.Fatal(err)
	}
	comparison, err := st.CompareExperiments(ctx, left.ID, right.ID, "quality")
	if err != nil || comparison.Items != 1 || comparison.Delta < 0.49 || comparison.Delta > 0.51 {
		t.Fatalf("comparison: %+v, %v", comparison, err)
	}
	if _, err := st.AddScore(ctx, "candidate", item.ID, "quality", 2, "human", ""); err == nil {
		t.Fatal("accepted out-of-range score")
	}
}

func TestDatasetRejectsMissingContent(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.WriteBatch(ctx, Batch{Source: "test", Spans: []Span{{ID: "s", RunID: "r", Kind: KindAgent, Name: "x", Status: "ok"}}}); err != nil {
		t.Fatal(err)
	}
	d, err := st.CreateDataset(ctx, "default", "cases")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateExperiment(ctx, d.ID, "empty"); err == nil {
		t.Fatal("created an experiment from an empty dataset")
	}
	if _, err := st.AddDatasetItem(ctx, d.ID, "r"); err == nil {
		t.Fatal("accepted empty input and output")
	}
}
