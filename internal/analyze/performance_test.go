package analyze

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
	"github.com/tonquoc0407/capybara/internal/testutil"
)

func BenchmarkSweep(b *testing.B) {
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
				if err := st.WriteBatch(context.Background(), batch); err != nil {
					_ = st.Close()
					b.Fatal(err)
				}
				// Avoid a developer's pricing overrides changing the workload.
				prices, err := loadPricing(filepath.Join(b.TempDir(), "missing.json"))
				if err != nil {
					_ = st.Close()
					b.Fatal(err)
				}
				a := &Analyzer{st: st, prices: prices, versions: make(map[string]int64)}
				b.StartTimer()
				err = a.Sweep(context.Background())
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
