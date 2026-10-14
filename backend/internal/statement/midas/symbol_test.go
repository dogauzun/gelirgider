package midas

import "testing"

func TestSymbolStripsSeriesSuffix(t *testing.T) {
	for cell, want := range map[string]string{
		"THYAO.E":                      "THYAO",
		"OPT25.F":                      "OPT25",
		"ALTIN.S1":                     "ALTIN",
		"ALTIN.S1 - Altın Sertifikası": "ALTIN",
		"VOO - Vanguard S&P 500 ETF":   "VOO",
	} {
		if got := symbol(cell); got != want {
			t.Errorf("symbol(%q) = %q, want %q", cell, got, want)
		}
	}
}
