package otlp

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
)

var sampleAt = time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)

func gauge(md pmetric.Metrics, name string) pmetric.NumberDataPointSlice {
	m := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	m.SetName(name)
	return m.SetEmptyGauge().DataPoints()
}

func point(points pmetric.NumberDataPointSlice, trace, span string, value float64) pmetric.NumberDataPoint {
	dp := points.AppendEmpty()
	dp.SetTimestamp(pcommon.NewTimestampFromTime(sampleAt))
	dp.SetDoubleValue(value)
	if trace != "" {
		dp.Attributes().PutStr(attrTraceID, trace)
	}
	if span != "" {
		dp.Attributes().PutStr(attrSpanID, span)
	}
	return dp
}

func TestToSamplesMergesBothGaugesOfOneReading(t *testing.T) {
	md := pmetric.NewMetrics()
	point(gauge(md, metricCPU), "trace1", "span1", 0.75)
	rss := point(gauge(md, metricRSS), "trace1", "span1", 0)
	rss.SetIntValue(4096)
	samples := ToSamples(md)
	if len(samples) != 1 {
		t.Fatalf("got %d samples, want the two gauges merged into 1", len(samples))
	}
	got := samples[0]
	if got.RunID != "trace1" || got.SpanID != "span1" {
		t.Errorf("ids = %s/%s, want trace1/span1", got.RunID, got.SpanID)
	}
	if got.CPUUtil == nil || *got.CPUUtil != 0.75 {
		t.Errorf("cpu = %v, want 0.75", got.CPUUtil)
	}
	if got.RSSBytes == nil || *got.RSSBytes != 4096 {
		t.Errorf("rss = %v, want 4096 from the int data point", got.RSSBytes)
	}
	if !got.At.Equal(sampleAt) {
		t.Errorf("at = %v, want %v", got.At, sampleAt)
	}
}

// A reading that names no span cannot say which node was running, which is the
// only thing it is for.
func TestToSamplesDropsPointsMissingEitherID(t *testing.T) {
	md := pmetric.NewMetrics()
	points := gauge(md, metricCPU)
	point(points, "trace1", "", 0.5)
	point(points, "", "span1", 0.5)
	point(points, "trace1", "span1", 0.5)
	if got := ToSamples(md); len(got) != 1 {
		t.Fatalf("got %d samples, want only the fully attributed one", len(got))
	}
}

func TestToSamplesIgnoresUnknownMetrics(t *testing.T) {
	md := pmetric.NewMetrics()
	point(gauge(md, "system.disk.io"), "trace1", "span1", 42)
	if got := ToSamples(md); len(got) != 0 {
		t.Fatalf("got %d samples, want none for an unrecognized metric", len(got))
	}
}

func TestToSamplesIgnoresNonGaugeMetrics(t *testing.T) {
	md := pmetric.NewMetrics()
	m := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	m.SetName(metricCPU)
	dp := m.SetEmptySum().DataPoints().AppendEmpty()
	dp.SetDoubleValue(1)
	dp.Attributes().PutStr(attrTraceID, "trace1")
	dp.Attributes().PutStr(attrSpanID, "span1")
	if got := ToSamples(md); len(got) != 0 {
		t.Fatalf("got %d samples, want none for a sum", len(got))
	}
}

func TestToSamplesKeepsEachTimestampSeparately(t *testing.T) {
	md := pmetric.NewMetrics()
	points := gauge(md, metricCPU)
	first := point(points, "trace1", "span1", 0.1)
	first.SetTimestamp(pcommon.NewTimestampFromTime(sampleAt))
	second := point(points, "trace1", "span1", 0.9)
	second.SetTimestamp(pcommon.NewTimestampFromTime(sampleAt.Add(time.Second)))
	if got := ToSamples(md); len(got) != 2 {
		t.Fatalf("got %d samples, want one per timestamp", len(got))
	}
}

func TestReceiverStoresSamplesOverHTTP(t *testing.T) {
	st := openTemp(t)
	r := &Receiver{store: st}
	md := pmetric.NewMetrics()
	point(gauge(md, metricCPU), "trace1", "span1", 0.25)
	raw, err := pmetricotlp.NewExportRequestFromMetrics(md).MarshalProto()
	if err != nil {
		t.Fatalf("MarshalProto: %v", err)
	}
	req := httptest.NewRequest("POST", "/v1/metrics", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/x-protobuf")
	w := httptest.NewRecorder()
	r.handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	latest, err := st.LatestResourceSamples(context.Background(), "trace1")
	if err != nil {
		t.Fatalf("LatestResourceSamples: %v", err)
	}
	if got, ok := latest["span1"]; !ok || got.CPUUtil == nil || *got.CPUUtil != 0.25 {
		t.Errorf("stored sample = %+v, want cpu 0.25 on span1", got)
	}
}
