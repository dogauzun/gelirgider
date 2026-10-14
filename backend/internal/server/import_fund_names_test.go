package server

import (
	"fmt"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/statement"
)

// TestParseStatementMatchesGarantiFundNames covers GLG-1188: a Garanti fund
// name cut to its column or abbreviated the bank's way resolves to the one
// TEFAS fund it names, marked like a learned name; a name two funds start
// with stays unknown.
func TestParseStatementMatchesGarantiFundNames(t *testing.T) {
	e := newTransactionTestEnv(t)
	session := e.login("fund-names@example.com")
	portfolioID := e.createPortfolio(session, "Funds")
	for symbol, name := range map[string]string{
		"BDS": "PARDUS PORTFÖY BIST 30 DIŞI ŞİRKETLER HİSSE SENEDİ (TL) FONU (HİSSE SENEDİ YOĞUN FON)",
		"GSP": "AZİMUT PYŞ KAR PAYI ÖDEYEN HİSSE SENEDİ FONU (HİSSE SENEDİ YOĞUN FON)",
		"GTL": "GARANTİ PORTFÖY BİRİNCİ PARA PİYASASI (TL) FONU",
		"GNP": "GARANTİ PORTFÖY NEMA PARA PİYASASI (TL) FONU",
		"GJH": "GARANTİ PORTFÖY PARA PİYASASI SERBEST (TL) FON",
		"GKH": "GARANTİ PORTFÖY PARA PİYASASI KATILIM SERBEST (TL) FON",
		"PIP": "GARANTİ PORTFÖY İKİNCİ PARA PİYASASI SERBEST (TL) FON",
		"GPN": "GARANTİ PORTFÖY PARA PİYASASI KATILIM (TL) FONU",
		"ARL": "AK PORTFÖY BİRİNCİ FON SEPETİ FONU",
		"MJB": "AKTİF PORTFÖY BİRİNCİ FON SEPETİ FONU",
		"AK3": "AK PORTFÖY HİSSE SENEDİ (TL) FONU (HİSSE SENEDİ YOĞUN FON)",
		"ASJ": "AKTİF PORTFÖY HİSSE SENEDİ (TL) FONU (HİSSE SENEDİ YOĞUN FON)",
	} {
		if _, err := e.db.Exec(`INSERT INTO instruments (symbol, market, name, currency) VALUES (?, 'TEFAS', ?, 'TRY')`, symbol, name); err != nil {
			t.Fatalf("insert instrument: %v", err)
		}
	}
	cases := []struct{ name, want string }{
		{"PARDUS PORTFÖY BIST 30 DIŞI ŞİRKETL", "BDS"},
		{"AZİMUT PYŞ KAR PAYI ÖDEYEN HİSSE SE", "GSP"},
		{"GPY 1. PARA PİYASASI (TL) FONU", "GTL"},
		{"GPY NEMA PARA PİY (TL) FONU", "GNP"},
		{"GP PARA PİYASASI SERBEST(TL) FON", "GJH"},
		// An exact name and a cut one win over AKTİF PORTFÖY's funds.
		{"AK PORTFÖY BİRİNCİ FON SEPETİ FONU", "ARL"},
		{"AK PORTFÖY HİSSE SENEDİ (TL) FONU", "AK3"},
		// GJH, GKH and GPN all start so.
		{"GARANTİ PORTFÖY PARA PİYASASI", ""},
	}
	var trades []statement.Trade
	for i, c := range cases {
		trades = append(trades, statement.Trade{Date: fmt.Sprintf("2025-04-%02d", i+1), Symbol: c.name, Side: "buy", Quantity: dec("4"), Price: dec("2.5"), Amount: dec("10"), Fee: dec("0"), Currency: "TRY", Market: "TEFAS"})
	}
	original := statement.Parsers["garanti"]
	t.Cleanup(func() { statement.Parsers["garanti"] = original })
	statement.Parsers["garanti"] = func([][]string) (statement.Statement, error) {
		return statement.Statement{Broker: "garanti", PeriodStart: "2025-04-01", PeriodEnd: "2025-04-30", Currency: "TRY", Market: "TEFAS", Trades: trades}, nil
	}

	p := parsePreview(t, upload(e, session, "garanti", portfolioID, emptyPDF(1)))
	got := map[string]string{}
	for _, row := range p.Rows {
		got[row.Symbol] = fmt.Sprintf("%s %s %s", row.Status, row.ResolvedBy, row.InstrSymbol)
	}
	for _, c := range cases {
		want := "unknown_instrument  "
		if c.want != "" {
			want = "ok name " + c.want
		}
		if got[c.name] != want {
			t.Errorf("%q = %q, want %q", c.name, got[c.name], want)
		}
	}
}
