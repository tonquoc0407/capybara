package intake

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/testutil"
)

func BenchmarkImportJSONL(b *testing.B) {
	for _, n := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprintf("fresh-db/spans-%d", n), func(b *testing.B) {
			fixture := testutil.JSONL(n, 0)
			b.ReportMetric(float64(len(fixture)), "bytes/input")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				st, err := store.Open(filepath.Join(b.TempDir(), "trace.db"))
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				err = ImportJSONL(context.Background(), st, strings.NewReader(fixture), true)
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

		b.Run(fmt.Sprintf("repeated-db/spans-%d", n), func(b *testing.B) {
			fixture := testutil.JSONL(n, 0)
			st, err := store.Open(filepath.Join(b.TempDir(), "trace.db"))
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = st.Close() })
			b.ReportMetric(float64(len(fixture)), "bytes/input")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := ImportJSONL(context.Background(), st, strings.NewReader(fixture), true); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkImportJSONLLargeToolOutput(b *testing.B) {
	for _, size := range []int{64 << 10, 1 << 20} {
		b.Run(fmt.Sprintf("output-%dKiB", size>>10), func(b *testing.B) {
			fixture := testutil.JSONL(3, size)
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
				err = ImportJSONL(context.Background(), st, strings.NewReader(fixture), true)
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
