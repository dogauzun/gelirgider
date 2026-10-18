package instrument_test

import (
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/instrument"
)

func TestSectorKey(t *testing.T) {
	tests := []struct {
		name, market, raw, want string
	}{
		{"energy", "US", "Energy", "energy"},
		{"materials", "US", "Metals & Mining", "materials"},
		{"industrials", "US", "Machinery", "industrials"},
		{"consumer discretionary", "US", "Automobiles", "consumer_discretionary"},
		{"consumer staples", "US", "Food Products", "consumer_staples"},
		{"health care", "US", "Pharmaceuticals", "health_care"},
		{"financials", "US", "Banking", "financials"},
		{"information technology", "US", "Technology", "information_technology"},
		{"communication services", "US", "Telecommunication", "communication_services"},
		{"utilities", "US", "Utilities", "utilities"},
		{"real estate", "US", "Real Estate", "real_estate"},
		{"case folded", "US", "bANKING", "financials"},
		{"diacritic folded", "US", " BANKİNG ", "financials"},
		{"unmapped", "US", "Space Mining", "unknown"},
		{"empty", "US", "", "unknown"},
		{"market without a map", "BIST", "Banking", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := instrument.SectorKey(tt.market, tt.raw); got != tt.want {
				t.Errorf("SectorKey(%q, %q) = %q, want %q", tt.market, tt.raw, got, tt.want)
			}
		})
	}
}
