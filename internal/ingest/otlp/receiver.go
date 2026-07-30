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

func (r *Receiver) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/traces", r.handleTraces)
	return mux
}

func (r *Receiver) handleTraces(w http.ResponseWriter, req *http.Request) {
	var body io.Reader = req.Body
	if req.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(req.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer gz.Close()
		body = gz
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	contentType, _, _ := strings.Cut(req.Header.Get("Content-Type"), ";")
	exportReq := ptraceotlp.NewExportRequest()
	switch contentType {
	case "application/x-protobuf":
		err = exportReq.UnmarshalProto(raw)
	case "application/json":
		err = exportReq.UnmarshalJSON(raw)
	default:
		http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ingest(req.Context(), exportReq.Traces()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := ptraceotlp.NewExportResponse()
	var respBytes []byte
	if contentType == "application/json" {
		respBytes, err = resp.MarshalJSON()
	} else {
		respBytes, err = resp.MarshalProto()
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(respBytes)
}
