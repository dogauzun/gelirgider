package deposit

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// TestClose checks the net simple interest, actual/365, by hand:
// 0.45 x 32 days x (1 - 0.175) = 11.88 over 365; 0.40 x 365 / 365 x 0.85 = 0.34.
func TestClose(t *testing.T) {
	short := Terms{Rate: d("0.45"), StopajRate: d("0.175"), StartDate: "2026-01-01", MaturityDate: "2026-02-02"}
	year := Terms{Rate: d("0.40"), StopajRate: d("0.15"), StartDate: "2026-01-01", MaturityDate: "2027-01-01"}
	tests := []struct {
		name   string
		terms  Terms
		day    string
		want   string
		wantOK bool
	}{
		{"before start", short, "2025-12-31", "", false},
		{"on start", short, "2026-01-01", "1", true},
		{"one day in", short, "2026-01-02", "1.0010171232876712", true},  // 0.45 x 0.825 / 365
		{"on maturity", short, "2026-02-02", "1.0325479452054795", true}, // 1 + 11.88/365
		{"past maturity", short, "2026-03-01", "1.0325479452054795", true},
		{"365-day term at maturity", year, "2027-01-01", "1.34", true},
	}
	for _, tt := range tests {
		got, ok := Close(tt.terms, tt.day)
		if ok != tt.wantOK {
			t.Errorf("%s: ok = %v, want %v", tt.name, ok, tt.wantOK)
			continue
		}
		if ok && !got.Equal(d(tt.want)) {
			t.Errorf("%s: close = %s, want %s", tt.name, got, tt.want)
		}
	}
}
