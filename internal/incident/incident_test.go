package incident

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestGroupsAndPacketKeepEvidenceSeparate(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, id := range []string{"a", "b"} {
		sp := store.Span{ID: id + "-span", RunID: id, Kind: store.KindTool,
			Name: "fetch", StartedAt: time.Now(), EndedAt: time.Now(), Status: "error"}
		if err := st.WriteBatch(ctx, store.Batch{Source: "test",
			Spans: []store.Span{sp}, Findings: []store.Finding{{RunID: id, SpanID: sp.ID,
				Type: "drift", Severity: "warning", Detail: `{}`}}}); err != nil {
			t.Fatal(err)
		}
	}
	groups, err := List(ctx, st, "", 10)
	if err != nil || len(groups) != 1 || groups[0].Count != 2 {
		t.Fatalf("groups: %+v %v", groups, err)
	}
	packet, err := BuildPacket(ctx, st, "a")
	if err != nil {
		t.Fatal(err)
	}
	if packet.ReplayInputsRecorded || len(packet.Hypotheses) != 1 || len(packet.Observed.Spans) != 1 {
		t.Fatalf("packet: %+v", packet)
	}
	if packet.Fingerprint != groups[0].Fingerprint {
		t.Fatal("packet fingerprint differs")
	}
}

func TestListFiltersBeforeLimit(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "incidents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	base := time.Now().Add(-time.Hour)
	for i, sample := range []struct{ id, status string }{{"broken", "error"}, {"healthy-1", "ok"}, {"healthy-2", "ok"}} {
		sp := store.Span{ID: sample.id + "-span", RunID: sample.id, Kind: store.KindAgent,
			Name: "agent", StartedAt: base.Add(time.Duration(i) * time.Minute),
			EndedAt: base.Add(time.Duration(i)*time.Minute + time.Second), Status: sample.status}
		if err := st.WriteBatch(ctx, store.Batch{Source: "test", Spans: []store.Span{sp}}); err != nil {
			t.Fatal(err)
		}
	}
	groups, err := List(ctx, st, "", 1)
	if err != nil || len(groups) != 1 || len(groups[0].Runs) != 1 || groups[0].Runs[0] != "broken" {
		t.Fatalf("older incident hidden by healthy runs: %+v, %v", groups, err)
	}
}

func TestFingerprintSeparatesFailurePositionsAndCanonicalSchemaChanges(t *testing.T) {
	spans := []store.Span{{ID: "a", Kind: store.KindTool, Name: "fetch", Status: "error"},
		{ID: "b", Kind: store.KindTool, Name: "fetch", Status: "ok"}}
	first, _ := Fingerprint(spans, nil)
	later := append([]store.Span(nil), spans...)
	later[0].Status, later[1].Status = "ok", "error"
	second, _ := Fingerprint(later, nil)
	if first == second {
		t.Fatal("different first failure positions were grouped together")
	}
	classified := append([]store.Span(nil), spans...)
	classified[0].Attrs.Raw = map[string]any{"error.type": "TimeoutError"}
	timeout, _ := Fingerprint(classified, nil)
	classified[0].Attrs.Raw = map[string]any{"error.type": "PermissionDenied"}
	denied, _ := Fingerprint(classified, nil)
	if timeout == denied {
		t.Fatal("different error types were grouped together")
	}
	finding := store.Finding{SpanID: "a", Type: "drift", Detail: `{"missing":["currency","price"],"retyped":[{"field":"id","want":"number","got":"string"}]}`}
	canonical, _ := Fingerprint(spans, []store.Finding{finding})
	reordered := finding
	reordered.Detail = `{"retyped":[{"got":"string","want":"number","field":"id"}],"missing":["price","currency"],"tool":"display only"}`
	duplicate, _ := Fingerprint(spans, []store.Finding{reordered, finding})
	if canonical != duplicate {
		t.Fatal("JSON ordering or duplicate signals split the incident")
	}
	different := finding
	different.Detail = `{"missing":["address"]}`
	other, _ := Fingerprint(spans, []store.Finding{different})
	if canonical == other {
		t.Fatal("different schema changes were merged")
	}
	renamed := append([]store.Span(nil), spans...)
	renamed[0].ID, renamed[1].ID = "new-a", "new-b"
	reordered.SpanID = "new-a"
	stable, _ := Fingerprint(renamed, []store.Finding{reordered})
	if stable != canonical {
		t.Fatal("span IDs changed the semantic fingerprint")
	}
}

func TestPacketHTMLEscapesRecordedContent(t *testing.T) {
	p := Packet{RunID: "r", Context: RunContext{Project: "default"},
		Observed: Observed{Contents: map[string][]store.Content{"s": {{SpanID: "s", Role: "output", Body: `<script>alert(1)</script>`}}}}}
	body, err := PacketHTML(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `<script>alert(1)</script>`) || !strings.Contains(string(body), `&lt;script&gt;`) {
		t.Fatalf("recorded content was interpreted as markup: %s", body)
	}
}

func TestPacketLinksReplayComparisonAndScores(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "packet.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	for _, sample := range []struct{ id, status string }{{"before", "error"}, {"after", "ok"}} {
		err := st.WriteBatch(ctx, store.Batch{Source: "test", Spans: []store.Span{{ID: sample.id + "-span",
			RunID: sample.id, Kind: store.KindAgent, Name: "agent", StartedAt: now,
			EndedAt: now.Add(time.Second), Status: sample.status}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetRunParent(ctx, "after", "replay", "before"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddScore(ctx, "after", "", "quality", 1, "human", "reviewed"); err != nil {
		t.Fatal(err)
	}
	p, err := BuildPacket(ctx, st, "after")
	if err != nil || p.Comparison == nil || p.Comparison.ParentRunID != "before" ||
		p.Comparison.ParentStatus != "error" || p.Comparison.CurrentStatus != "ok" || len(p.Scores) != 1 {
		t.Fatalf("packet lacked verification data: %+v, %v", p, err)
	}
}
