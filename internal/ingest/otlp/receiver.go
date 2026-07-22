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
	GRPCAddr string
	HTTPAddr string
	store    *store.Store
	capture  bool
	grpcLis  net.Listener
	httpLis  net.Listener
}

// New returns a receiver bound to the default localhost OTLP ports.
func New(st *store.Store, captureContent bool) *Receiver {
	return &Receiver{
		GRPCAddr: "127.0.0.1:4317",
		HTTPAddr: "127.0.0.1:4318",
		store:    st,
		capture:  captureContent,
	}
}

// Listen binds both ports without serving, so callers can fail fast.
func (r *Receiver) Listen() error {
	if r.grpcLis != nil {
		return nil
	}
	grpcLis, err := net.Listen("tcp", r.GRPCAddr)
	if err != nil {
		return fmt.Errorf("otlp grpc listen: %w", err)
	}
	httpLis, err := net.Listen("tcp", r.HTTPAddr)
	if err != nil {
		grpcLis.Close()
		return fmt.Errorf("otlp http listen: %w", err)
	}
	r.grpcLis, r.httpLis = grpcLis, httpLis
	return nil
}

// Run serves on both ports until ctx is cancelled, binding them if needed.
func (r *Receiver) Run(ctx context.Context) error {
	if err := r.Listen(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		r.grpcLis.Close()
		r.httpLis.Close()
		return nil
	}
	return r.serve(ctx, r.grpcLis, r.httpLis)
}

func (r *Receiver) serve(ctx context.Context, grpcLis, httpLis net.Listener) error {
	// Content-heavy batches blow past grpc's 4 MiB default; never drop data.
	grpcSrv := grpc.NewServer(grpc.MaxRecvMsgSize(64 << 20))
	ptraceotlp.RegisterGRPCServer(grpcSrv, &exportServer{recv: r})
	httpSrv := &http.Server{Handler: r.handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 2)
	go func() { errc <- grpcSrv.Serve(grpcLis) }()
	go func() {
		if err := httpSrv.Serve(httpLis); !errors.Is(err, http.ErrServerClosed) {
			errc <- err
			return
		}
		errc <- nil
	}()
	select {
	case <-ctx.Done():
		grpcSrv.GracefulStop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			httpSrv.Close()
		}
		<-errc
		<-errc
		return nil
	case err := <-errc:
		grpcSrv.Stop()
		httpSrv.Close()
		<-errc
		return fmt.Errorf("otlp serve: %w", err)
	}
}

func (r *Receiver) ingest(ctx context.Context, td ptrace.Traces) error {
	return r.store.WriteBatch(ctx, ToBatch(td, r.capture))
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
