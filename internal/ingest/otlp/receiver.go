package otlp

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	_ "google.golang.org/grpc/encoding/gzip" // registers gzip decompression for grpc exporters
	"google.golang.org/grpc/status"

	"github.com/tonquoc0407/capybara/internal/store"
)

// Receiver ingests OTLP traces over grpc and http into the store.
type Receiver struct {
	GRPCAddr   string
	HTTPAddr   string
	store      *store.Store
	capture    bool
	mapping    *Mapping
	mappingErr error
	grpcLis    net.Listener
	httpLis    net.Listener
}

// New returns a receiver bound to the default localhost OTLP ports, loading the
// optional user mapping. A broken mapping file is held until Run reports it, so
// New stays signature-compatible with its callers.
func New(st *store.Store, captureContent bool) *Receiver {
	m, err := LoadMapping(DefaultMappingPath())
	return &Receiver{
		GRPCAddr:   "127.0.0.1:4317",
		HTTPAddr:   "127.0.0.1:4318",
		store:      st,
		capture:    captureContent,
		mapping:    m,
		mappingErr: err,
	}
}

// Listen binds the ports without serving, so callers can report the address
// before traffic starts. Each transport is bound on its own: 4317 and 4318 are
// the ports every other tracing tool wants too, and losing one of them is no
// reason to refuse the other. The error names what could not be bound; whether
// that is fatal is the caller's call.
func (r *Receiver) Listen() error {
	if r.mappingErr != nil {
		return fmt.Errorf("mapping: %w", r.mappingErr)
	}
	if r.grpcLis != nil || r.httpLis != nil {
		return nil
	}
	var errs []error
	if lis, err := net.Listen("tcp", r.GRPCAddr); err != nil {
		errs = append(errs, fmt.Errorf("otlp grpc listen: %w", err))
	} else {
		r.grpcLis = lis
	}
	if lis, err := net.Listen("tcp", r.HTTPAddr); err != nil {
		errs = append(errs, fmt.Errorf("otlp http listen: %w", err))
	} else {
		r.httpLis = lis
	}
	return errors.Join(errs...)
}

// Listening reports whether any transport bound.
func (r *Receiver) Listening() bool {
	return r.grpcLis != nil || r.httpLis != nil
}

// HTTPBase is what OTEL_EXPORTER_OTLP_ENDPOINT wants: the collector root, with
// no signal path, because every SDK appends /v1/traces itself.
func (r *Receiver) HTTPBase() string {
	if r.httpLis == nil {
		return ""
	}
	return "http://" + r.httpLis.Addr().String()
}

// HTTPEndpoint is the bound OTLP trace endpoint, known only after Listen when
// the port was left to the operating system.
func (r *Receiver) HTTPEndpoint() string {
	if r.httpLis == nil {
		return ""
	}
	return "http://" + r.httpLis.Addr().String() + "/v1/traces"
}

// Run serves whichever ports bound, until ctx is cancelled.
func (r *Receiver) Run(ctx context.Context) error {
	_ = r.Listen() // a port that would not bind is reported by Listen's caller
	if !r.Listening() {
		return nil
	}
	if ctx.Err() != nil {
		r.close()
		return nil
	}
	return r.serve(ctx)
}

func (r *Receiver) close() {
	if r.grpcLis != nil {
		r.grpcLis.Close()
	}
	if r.httpLis != nil {
		r.httpLis.Close()
	}
}

func (r *Receiver) serve(ctx context.Context) error {
	// Content-heavy batches blow past grpc's 4 MiB default; never drop data.
	grpcSrv := grpc.NewServer(grpc.MaxRecvMsgSize(64 << 20))
	ptraceotlp.RegisterGRPCServer(grpcSrv, &exportServer{recv: r})
	pmetricotlp.RegisterGRPCServer(grpcSrv, &metricServer{recv: r})
	httpSrv := &http.Server{Handler: r.handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 2)
	running := 0
	if r.grpcLis != nil {
		running++
		go func() { errc <- grpcSrv.Serve(r.grpcLis) }()
	}
	if r.httpLis != nil {
		running++
		go func() {
			if err := httpSrv.Serve(r.httpLis); !errors.Is(err, http.ErrServerClosed) {
				errc <- err
				return
			}
			errc <- nil
		}()
	}
	drain := func() {
		for range running {
			<-errc
		}
	}
	select {
	case <-ctx.Done():
		grpcSrv.GracefulStop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			httpSrv.Close()
		}
		drain()
		return nil
	case err := <-errc:
		running--
		grpcSrv.Stop()
		httpSrv.Close()
		drain()
		return fmt.Errorf("otlp serve: %w", err)
	}
}

func (r *Receiver) ingest(ctx context.Context, td ptrace.Traces) error {
	return r.store.WriteBatch(ctx, toBatch(td, r.capture, r.mapping))
}

func (r *Receiver) ingestMetrics(ctx context.Context, md pmetric.Metrics) error {
	return r.store.PutResourceSamples(ctx, "otlp", ToSamples(md))
}

type exportServer struct {
	ptraceotlp.UnimplementedGRPCServer
	recv *Receiver
}

func (s *exportServer) Export(ctx context.Context, req ptraceotlp.ExportRequest) (ptraceotlp.ExportResponse, error) {
	if err := s.recv.ingest(ctx, req.Traces()); err != nil {
		return ptraceotlp.NewExportResponse(), status.Error(codes.Internal, err.Error())
	}
	return ptraceotlp.NewExportResponse(), nil
}

type metricServer struct {
	pmetricotlp.UnimplementedGRPCServer
	recv *Receiver
}

func (s *metricServer) Export(ctx context.Context, req pmetricotlp.ExportRequest) (pmetricotlp.ExportResponse, error) {
	if err := s.recv.ingestMetrics(ctx, req.Metrics()); err != nil {
		return pmetricotlp.NewExportResponse(), status.Error(codes.Internal, err.Error())
	}
	return pmetricotlp.NewExportResponse(), nil
}

func (r *Receiver) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/traces", r.handleTraces)
	mux.HandleFunc("POST /v1/metrics", r.handleMetrics)
	return mux
}

// maxHTTPBody matches the grpc receiver's MaxRecvMsgSize: an unbounded read
// here, compressed or not, lets an untrusted sender exhaust memory with one
// request.
const maxHTTPBody = 64 << 20

func (r *Receiver) handleTraces(w http.ResponseWriter, req *http.Request) {
	raw, contentType, ok := readExport(w, req)
	if !ok {
		return
	}
	exportReq := ptraceotlp.NewExportRequest()
	if !unmarshalExport(w, contentType, raw, exportReq.UnmarshalProto, exportReq.UnmarshalJSON) {
		return
	}
	if err := r.ingest(req.Context(), exportReq.Traces()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := ptraceotlp.NewExportResponse()
	writeExport(w, contentType, resp.MarshalProto, resp.MarshalJSON)
}

func (r *Receiver) handleMetrics(w http.ResponseWriter, req *http.Request) {
	raw, contentType, ok := readExport(w, req)
	if !ok {
		return
	}
	exportReq := pmetricotlp.NewExportRequest()
	if !unmarshalExport(w, contentType, raw, exportReq.UnmarshalProto, exportReq.UnmarshalJSON) {
		return
	}
	if err := r.ingestMetrics(req.Context(), exportReq.Metrics()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := pmetricotlp.NewExportResponse()
	writeExport(w, contentType, resp.MarshalProto, resp.MarshalJSON)
}

func readExport(w http.ResponseWriter, req *http.Request) (raw []byte, contentType string, ok bool) {
	req.Body = http.MaxBytesReader(w, req.Body, maxHTTPBody)
	var body io.Reader = req.Body
	if req.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(req.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil, "", false
		}
		defer gz.Close()
		body = io.LimitReader(gz, maxHTTPBody)
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, "", false
	}
	contentType, _, _ = strings.Cut(req.Header.Get("Content-Type"), ";")
	return raw, contentType, true
}

func unmarshalExport(w http.ResponseWriter, contentType string, raw []byte, proto, jsonUn func([]byte) error) bool {
	var err error
	switch contentType {
	case "application/x-protobuf":
		err = proto(raw)
	case "application/json":
		err = jsonUn(raw)
	default:
		http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
		return false
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func writeExport(w http.ResponseWriter, contentType string, proto, jsonMar func() ([]byte, error)) {
	marshal := proto
	if contentType == "application/json" {
		marshal = jsonMar
	}
	respBytes, err := marshal()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(respBytes)
}
