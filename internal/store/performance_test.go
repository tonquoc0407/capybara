package store_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/testutil"
)

func BenchmarkWriteBatch(b *testing.B) {
	for _, n := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprintf("spans-%d", n), func(b *testing.B) {
			batch := testutil.TraceBatch(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				st, err := store.Open(filepath.Join(b.TempDir(), "trace.db"))
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				err = st.WriteBatch(context.Background(), batch)
				b.StopTimer()
				if err != nil {
					_ = st.Close()
					b.Fatal(err)
				}
				if err := st.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkListRuns(b *testing.B) {
	for _, n := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprintf("spans-%d", n), func(b *testing.B) {
			st, err := store.Open(filepath.Join(b.TempDir(), "trace.db"))
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = st.Close() })
			if err := st.WriteBatch(context.Background(), testutil.TraceBatch(n)); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				runs, err := st.ListRuns(context.Background())
				if err != nil || len(runs) != (n+99)/100 {
					b.Fatalf("runs = %d, %v", len(runs), err)
				}
			}
		})
	}
}

func BenchmarkWriteBatchLargeToolOutput(b *testing.B) {
	for _, size := range []int{64 << 10, 1 << 20} {
		b.Run(fmt.Sprintf("output-%dKiB", size>>10), func(b *testing.B) {
			batch := testutil.LargeToolBatch(size)
			b.ReportMetric(float64(size), "bytes/output")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				st, err := store.Open(filepath.Join(b.TempDir(), "trace.db"))
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				err = st.WriteBatch(context.Background(), batch)
				b.StopTimer()
				if err != nil {
					_ = st.Close()
					b.Fatal(err)
				}
				if err := st.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkPutResourceSamples(b *testing.B) {
	for _, n := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprintf("samples-%d", n), func(b *testing.B) {
			samples := make([]store.ResourceSample, n)
			for i := range samples {
				cpu := float64(i%100) / 100
				rss := int64(64 << 20)
				samples[i] = store.ResourceSample{
					RunID: "resource-run", SpanID: fmt.Sprintf("span-%06d", i),
					SpanName: "chat", At: time.Unix(0, int64(i)+1),
					CPUUtil: &cpu, RSSBytes: &rss,
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				st, err := store.Open(filepath.Join(b.TempDir(), "trace.db"))
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				err = st.PutResourceSamples(context.Background(), "benchmark", samples)
				b.StopTimer()
				if err != nil {
					_ = st.Close()
					b.Fatal(err)
				}
				if err := st.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
