// Package proxy provides an optional, local OpenAI chat-completions subset.
package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
)

const maxRequest = 1 << 20

type Server struct {
	Store          *store.Store
	UpstreamURL    string
	APIKey         string
	Client         *http.Client
	CaptureContent bool
	DenyPattern    *regexp.Regexp
	MaxCostUSD     float64 // soft limit for this process; checked after reported usage
	InputPerM      float64
	OutputPerM     float64
	mu             sync.Mutex
	spent          float64
	unknownUsage   bool
}

func (s *Server) validate() error {
	u, err := url.Parse(s.UpstreamURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("proxy needs an HTTP(S) upstream URL")
	}
	if u.Scheme == "http" {
		host := u.Hostname()
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return errors.New("plaintext upstream must be on loopback")
		}
	}
	if s.Store == nil || s.MaxCostUSD < 0 || s.InputPerM < 0 || s.OutputPerM < 0 ||
		math.IsNaN(s.MaxCostUSD) || math.IsInf(s.MaxCostUSD, 0) ||
		math.IsNaN(s.InputPerM) || math.IsInf(s.InputPerM, 0) ||
		math.IsNaN(s.OutputPerM) || math.IsInf(s.OutputPerM, 0) {
		return errors.New("invalid proxy configuration")
	}
	if s.MaxCostUSD > 0 && (s.InputPerM == 0 || s.OutputPerM == 0) {
		return errors.New("budget requires input and output prices")
	}
	return nil
}

func (s *Server) Serve(ctx context.Context, addr string, out io.Writer) error {
	if err := s.validate(); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("proxy must bind loopback")
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "http://%s\n", lis.Addr()); err != nil {
		lis.Close()
		return err
	}
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(lis) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(stop)
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", s.chat)
	return mux
}

func newIDs() (string, string, error) {
	var trace [16]byte
	var span [8]byte
	if _, err := rand.Read(trace[:]); err != nil {
		return "", "", err
	}
	if _, err := rand.Read(span[:]); err != nil {
		return "", "", err
	}
	return hex.EncodeToString(trace[:]), hex.EncodeToString(span[:]), nil
}

func (s *Server) priceVersion() string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%.12g|%.12g|%s", s.InputPerM, s.OutputPerM, s.UpstreamURL)))
	return hex.EncodeToString(h[:])
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	if err := s.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequest)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid or oversized request", http.StatusRequestEntityTooLarge)
		return
	}
	var request struct {
		Model    string            `json:"model"`
		Stream   bool              `json:"stream"`
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(raw, &request); err != nil || request.Model == "" || len(request.Messages) == 0 {
		http.Error(w, "model and messages are required", http.StatusBadRequest)
		return
	}
	if request.Stream {
		http.Error(w, "streaming is not supported by this proxy", http.StatusBadRequest)
		return
	}
	if s.DenyPattern != nil && s.DenyPattern.Match(raw) {
		http.Error(w, "request blocked by guardrail", http.StatusUnprocessableEntity)
		return
	}
	// Serialize requests so concurrent calls cannot both pass the session
	// budget check before either one's usage has been observed.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.MaxCostUSD > 0 && (s.unknownUsage || s.spent >= s.MaxCostUSD) {
		http.Error(w, "proxy session budget unavailable or exhausted", http.StatusPaymentRequired)
		return
	}
	traceID, spanID, err := newIDs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	started := time.Now().UTC()
	upstream, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.UpstreamURL, bytes.NewReader(raw))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	upstream.Header.Set("Content-Type", "application/json")
	if s.APIKey != "" {
		upstream.Header.Set("Authorization", "Bearer "+s.APIKey)
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	response, callErr := client.Do(upstream)
	var reply []byte
	status := http.StatusBadGateway
	if callErr == nil {
		defer response.Body.Close()
		status = response.StatusCode
		reply, callErr = io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
		if len(reply) > 8<<20 {
			callErr = errors.New("upstream response too large")
			reply = nil
		}
	}
	ended := time.Now().UTC()
	var usage struct {
		Prompt     int64 `json:"prompt_tokens"`
		Completion int64 `json:"completion_tokens"`
	}
	var envelope struct {
		Usage json.RawMessage `json:"usage"`
	}
	if callErr == nil && status >= 200 && status < 300 {
		_ = json.Unmarshal(reply, &envelope)
		if len(envelope.Usage) > 0 && string(envelope.Usage) != "null" && json.Unmarshal(envelope.Usage, &usage) == nil && usage.Prompt >= 0 && usage.Completion >= 0 {
			// The provider's usage is the basis for the configured estimate.
		} else if s.MaxCostUSD > 0 {
			s.unknownUsage = true
			envelope.Usage = nil
		}
	}
	spanStatus := "ok"
	if callErr != nil || status < 200 || status >= 300 {
		spanStatus = "error"
	}
	batch := store.Batch{Source: "proxy", DropContent: !s.CaptureContent,
		Spans: []store.Span{{ID: spanID, RunID: traceID, Kind: store.KindLLM, Name: "chat.completions",
			StartedAt: started, EndedAt: ended, Status: spanStatus,
			TokensIn: usage.Prompt, TokensOut: usage.Completion,
			Attrs: store.Attrs{Model: request.Model, Provider: "openai-compatible"}}}}
	if s.CaptureContent {
		batch.Contents = []store.Content{{SpanID: spanID, Role: "input", Body: string(raw), MediaType: "application/json"},
			{SpanID: spanID, Role: "output", Seq: 1, Body: string(reply), MediaType: "application/json"}}
	}
	storageErr := s.Store.WriteBatch(r.Context(), batch)
	if storageErr == nil && len(envelope.Usage) > 0 && spanStatus == "ok" && usage.Prompt >= 0 && usage.Completion >= 0 {
		cost := (float64(usage.Prompt)*s.InputPerM + float64(usage.Completion)*s.OutputPerM) / 1e6
		if cost > 0 {
			storageErr = s.Store.SetSpanEstimates(r.Context(), map[string]store.SpanEstimate{spanID: {
				CostUSD: cost, Provenance: store.CostProvenance{Kind: "estimate", PriceSource: "proxy configuration",
					PriceVersion: s.priceVersion(), UsageSource: "provider response", CheckedAt: ended},
			}})
			s.spent += cost
		}
	}
	if storageErr != nil {
		w.Header().Set("X-Capybara-Trace-Error", "recording failed")
		if s.MaxCostUSD > 0 {
			s.unknownUsage = true
		}
	}
	w.Header().Set("X-Capybara-Trace-ID", traceID)
	if callErr != nil {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	if s.DenyPattern != nil && s.DenyPattern.Match(reply) {
		http.Error(w, "response blocked by guardrail", http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(reply)
}
