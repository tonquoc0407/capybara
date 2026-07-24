package analyze

import (
	"testing"

	"github.com/tonquoc0407/capybara/internal/store"
)

func TestFindingLineLabelsOnlyWhatItCanWord(t *testing.T) {
	cases := []struct {
		name string
		f    store.Finding
		want string
	}{
		{
			name: "worded type is labelled",
			f:    store.Finding{Type: "drift", Detail: `{"missing":["currency"]}`},
			want: "drift: missing field: currency",
		},
		{
			name: "unparseable detail falls back without stuttering",
			f:    store.Finding{Type: "drift", Detail: "not json"},
			want: "drift",
		},
		{
			name: "unknown type falls back without stuttering",
			f:    store.Finding{Type: "tool_error", Detail: `{"status":502}`},
			want: "tool_error",
		},
		{
			name: "summary that opens with its type is not labelled again",
			f:    store.Finding{Type: "improvised", Detail: `{"tool":"get_stock_price"}`},
			want: "improvised after get_stock_price failure",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FindingLine(c.f); got != c.want {
				t.Errorf("FindingLine = %q, want %q", got, c.want)
			}
		})
	}
}
