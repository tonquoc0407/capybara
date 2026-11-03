package store

import "time"

// Kind classifies a span in capybara's internal taxonomy.
type Kind string

// The internal span kinds; everything unrecognized is KindOther.
const (
	KindAgent     Kind = "agent"
	KindLLM       Kind = "llm"
	KindTool      Kind = "tool"
	KindRetrieval Kind = "retrieval"
	KindOther     Kind = "other"
)

// ParseKind maps s onto a known kind, defaulting to KindOther.
func ParseKind(s string) Kind {
	switch k := Kind(s); k {
	case KindAgent, KindLLM, KindTool, KindRetrieval:
		return k
	}
	return KindOther
}

// Attrs is the structured attribute set serialized into spans.attrs_json.
type Attrs struct {
	Model    string         `json:"model,omitempty"`
	Provider string         `json:"provider,omitempty"`
	ToolName string         `json:"tool_name,omitempty"`
	MCP      bool           `json:"mcp,omitempty"`
	Raw      map[string]any `json:"raw,omitempty"`
}

// Run is one row of the runs table plus its findings count.
type Run struct {
	ID          string
	Project     string
	Environment string
	Release     string
	SessionID   string
	UserID      string
	Source      string
	StartedAt   time.Time
	EndedAt     time.Time
	ModelMain   string
	TokensIn    int64
	TokensOut   int64
	CostUSD     *float64
	Status      string
	Label       string
	ParentRunID string
	Findings    int64
}

// RunContext contains optional grouping metadata observed on a trace.
type RunContext struct {
	Project, Environment, Release, SessionID, UserID string
}

// TraceLog is an OTLP log linked to a trace; SpanID may name a span that has
// not arrived yet. Body is empty when content capture is disabled.
type TraceLog struct {
	ID, RunID, SpanID, Severity, Body, AttrsJSON string
	ObservedAt                                   time.Time
}

// CostProvenance describes whether a cost was provider reported or estimated.
// PriceEffectiveAt is zero when the rate table does not publish an effective date.
type CostProvenance struct {
	SpanID           string     `json:"span_id"`
	Kind             string     `json:"kind"`
	PriceSource      string     `json:"price_source"`
	PriceVersion     string     `json:"price_version"`
	PriceEffectiveAt *time.Time `json:"price_effective_at,omitempty"`
	CheckedAt        time.Time  `json:"checked_at"`
	UsageSource      string     `json:"usage_source"`
}

// SpanEstimate binds an automatic cost and its provenance to a span revision.
type SpanEstimate struct {
	CostUSD    float64
	Provenance CostProvenance
	// Revision guards automatic estimates against late changes to pricing inputs.
	Revision int64
}

// Span is one row of the spans table.
type Span struct {
	// Revision is internal optimistic concurrency state, never imported/exported.
	Revision  int64 `json:"-"`
	ID        string
	RunID     string
	ParentID  string
	Kind      Kind
	Name      string
	StartedAt time.Time
	EndedAt   time.Time
	TokensIn  int64
	TokensOut int64
	CostUSD   *float64
	Status    string
	Attrs     Attrs
}

// Content is one row of the contents table.
type Content struct {
	SpanID    string
	Role      string
	Seq       int
	Body      string
	MediaType string
}

// CachedLLM is one row of llm_cache: a recorded model response keyed by the
// hash of the request that produced it.
type CachedLLM struct {
	RunID       string
	SpanID      string
	RequestHash string
	Response    string
}

// Finding is one row of the findings table. SpanID is empty for run-level
// findings such as parse errors.
type Finding struct {
	ID       int64
	RunID    string
	SpanID   string
	Type     string
	Severity string
	Detail   string
	// Origin separates imported evidence from findings that analysis may replace.
	Origin string `json:"-"`
}

// Taint is one propagation edge: SpanID consumed content derived from
// SourceSpanID, a finding-marked span.
type Taint struct {
	RunID        string
	SpanID       string
	SourceSpanID string
}

// ResourceSample is one process reading taken while SpanID was executing.
// CPUUtil is OTel's fraction, not a percentage, and exceeds 1 on more than one
// core. The span it names may never reach the spans table: that is the point,
// which is also why SpanName rides along rather than being looked up.
// The GPU pair is the whole device's, not this process's - nvidia reports no
// per-process utilization - and is nil where there is no card.
type ResourceSample struct {
	RunID       string
	SpanID      string
	SpanName    string
	At          time.Time
	CPUUtil     *float64
	RSSBytes    *int64
	GPUUtil     *float64
	GPUMemBytes *int64
}
