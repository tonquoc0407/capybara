package otlp

import (
	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/tonquoc0407/capybara/internal/store"
)

// OTel's own way to link a metric to a trace is the exemplar, which is sampled
// and optional - no use when the reading's whole value is naming the span that
// was running. capybara's SDK puts the ids on the data point instead, in the
// same capybara.* namespace the tracer already uses for schema and entrypoint.
const (
	attrTraceID  = "capybara.trace_id"
	attrSpanID   = "capybara.span_id"
	attrSpanName = "capybara.span_name"
)

// The two process gauges capybara reads. Both are OTel names; anything else in
// the payload is ignored rather than guessed at.
const (
	metricCPU = "process.cpu.utilization"
	metricRSS = "process.memory.usage"
	// No stable OTel convention exists for these, so they carry capybara's own
	// names and mean the whole device, not this process.
	metricGPUUtil = "capybara.gpu.utilization"
	metricGPUMem  = "capybara.gpu.memory.usage"
)

func sampledMetric(name string) bool {
	switch name {
	case metricCPU, metricRSS, metricGPUUtil, metricGPUMem:
		return true
	}
	return false
}

// ToSamples maps OTLP metrics onto resource samples, one per (span, timestamp).
// A data point without both ids is dropped: an unattributable reading says
// nothing about which node was running.
func ToSamples(md pmetric.Metrics) []store.ResourceSample {
	byKey := make(map[sampleKey]*store.ResourceSample)
	var order []sampleKey
	resources := md.ResourceMetrics()
	for i := 0; i < resources.Len(); i++ {
		scopes := resources.At(i).ScopeMetrics()
		for j := 0; j < scopes.Len(); j++ {
			metrics := scopes.At(j).Metrics()
			for k := 0; k < metrics.Len(); k++ {
				metric := metrics.At(k)
				name := metric.Name()
				if !sampledMetric(name) {
					continue
				}
				if metric.Type() != pmetric.MetricTypeGauge {
					continue
				}
				points := metric.Gauge().DataPoints()
				for p := 0; p < points.Len(); p++ {
					collect(byKey, &order, name, points.At(p))
				}
			}
		}
	}
	out := make([]store.ResourceSample, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	return out
}

type sampleKey struct {
	spanID string
	nanos  int64
}

func collect(byKey map[sampleKey]*store.ResourceSample, order *[]sampleKey, name string, dp pmetric.NumberDataPoint) {
	attrs := dp.Attributes()
	trace, ok := attrs.Get(attrTraceID)
	if !ok {
		return
	}
	span, ok := attrs.Get(attrSpanID)
	if !ok {
		return
	}
	traceID, spanID := trace.Str(), span.Str()
	if traceID == "" || spanID == "" {
		return
	}
	key := sampleKey{spanID: spanID, nanos: int64(dp.Timestamp())}
	sm, seen := byKey[key]
	if !seen {
		sm = &store.ResourceSample{RunID: traceID, SpanID: spanID, At: toTime(dp.Timestamp())}
		byKey[key] = sm
		*order = append(*order, key)
	}
	if sm.SpanName == "" {
		if n, ok := attrs.Get(attrSpanName); ok {
			sm.SpanName = n.Str()
		}
	}
	value := numberValue(dp)
	switch name {
	case metricCPU:
		sm.CPUUtil = &value
	case metricRSS:
		rss := int64(value)
		sm.RSSBytes = &rss
	case metricGPUUtil:
		sm.GPUUtil = &value
	case metricGPUMem:
		mem := int64(value)
		sm.GPUMemBytes = &mem
	}
}

func numberValue(dp pmetric.NumberDataPoint) float64 {
	if dp.ValueType() == pmetric.NumberDataPointValueTypeInt {
		return float64(dp.IntValue())
	}
	return dp.DoubleValue()
}
