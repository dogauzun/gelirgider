package csv_test

import (
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/statement"
	"github.com/dogauzun/gelirgider/backend/internal/statement/csv"
	"github.com/shopspring/decimal"
)

func TestParse(t *testing.T) {
	d := decimal.RequireFromString
	export := []string{"date", "type", "symbol", "quantity", "price", "amount", "fee", "currency", "note", "market"}
	tests := []struct {
		name string
		rows [][]string
		want statement.Statement
	}{
		{
			name: "export layout, one row of each type",
			rows: [][]string{
				export,
				{"2025-03-05", "buy", "thyao.e", "10", "300.5", "", "1.5", "try", "a note", "bist"},
				{"2025-03-06", "sell", "AAPL", "2", "180", "360", "1", "USD", "", "US"},
				{"2025-03-07", "dividend", "THYAO", "", "", "50", "5", "", "", "BIST"},
				{"2025-03-08", "dividend", "", "", "", "12", "", "", "", ""},
				{"2025-03-09", "interest", "", "", "", "3", "", "", "", ""},
				{"2025-03-01", "deposit", "", "", "", "1000", "", "", "", ""},
				{"2025-03-10", "withdrawal", "", "", "", "200", "2", "", "", ""},
				{"2025-03-11", "fee", "", "", "", "7", "", "", "", ""},
				{"", "", "", "", "", "", "", "", "", ""},
			},
			want: statement.Statement{
				Broker: "csv", Currency: "TRY",
				Trades: []statement.Trade{
					{Date: "2025-03-05", Symbol: "THYAO", Side: "buy", Quantity: d("10"), Price: d("300.5"), Amount: d("3005"), Fee: d("1.5"), Currency: "TRY", Market: "BIST"},
					{Date: "2025-03-06", Symbol: "AAPL", Side: "sell", Quantity: d("2"), Price: d("180"), Amount: d("360"), Fee: d("1"), Currency: "USD", Market: "US"},
				},
				Dividends: []statement.Dividend{
					{Date: "2025-03-07", Symbol: "THYAO", Gross: d("50"), Tax: d("5"), Market: "BIST"},
				},
				Cash: []statement.CashMovement{
					{Date: "2025-03-08", Kind: "dividend", Amount: d("12"), Currency: "TRY"},
					{Date: "2025-03-09", Kind: "dividend", Amount: d("3"), Currency: "TRY"},
					{Date: "2025-03-01", Kind: "deposit", Amount: d("1000"), Currency: "TRY"},
					{Date: "2025-03-10", Kind: "withdrawal", Amount: d("200"), Fee: d("2"), Currency: "TRY"},
					{Date: "2025-03-11", Kind: "fee", Amount: d("7"), Currency: "TRY"},
				},
			},
		},
		{
			name: "Turkish header and numbers, errors do not stop the rest",
			rows: [][]string{
				{" Tarih ", "İşlem", "Sembol", "Adet", "Fiyat", "Tutar", "Komisyon", "Başka"},
				{"05.03.2025", "ALIŞ", "SISE", "1.5", "1,5", "1.234,56", "1,234.56", "x"},
				{"05.03.2025", "alış", "SISE", "1.000", "1", "", "", ""},
				{"05.03.2025", "alış", "SISE", "1", "1,000", "", "", ""},
				{"05.03.2025", "yatırma", "", "", "", "1.000.000", "", ""},
				{"06/03/2025", "takas", "SISE", "1", "1", "", "", ""},
				{"07/03/2025", "Satış", "SISE", "", "1", "", "", ""},
				{"2025-13-01", "alış", "SISE", "1", "1", "", "", ""},
				{"08.03.2025", "sat", "SISE", "abc", "1", "", "", ""},
				{"09.03.2025", "ücret", "", "", "", "5", "1", ""},
				{"10.03.2025", "alış", "SISE", "1e2000000000", "1e2000000000", "", "", ""},
				{"10.03.2025", "yatırma", "", "", "", "-500", "", ""},
				{"10.03.2025", "yatırma", "", "", "", "1234567890123456", "", ""},
				{"11.03.2025", "alış", "SISE", "1.2.3,4", "1", "", "", ""},
				{"12.03.2025", "alış", "SISE", "1", "1,2,3.4", "", "", ""},
			},
			want: statement.Statement{
				Broker: "csv", Currency: "TRY",
				Trades: []statement.Trade{
					{Date: "2025-03-05", Symbol: "SISE", Side: "buy", Quantity: d("1.5"), Price: d("1.5"), Amount: d("1234.56"), Fee: d("1234.56")},
				},
				Errors: []statement.ParseError{
					{Code: "ambiguous_number", Value: "quantity", Date: "2025-03-05"},
					{Code: "ambiguous_number", Value: "price", Date: "2025-03-05"},
					{Code: "ambiguous_number", Value: "amount", Date: "2025-03-05"},
					{Code: "unknown_type", Value: "takas", Date: "2025-03-06"},
					{Code: "missing_field", Value: "quantity", Date: "2025-03-07"},
					{Code: "bad_date"},
					{Code: "bad_number", Value: "quantity", Date: "2025-03-08"},
					{Code: "fee_on_fee", Date: "2025-03-09"},
					{Code: "bad_number", Value: "quantity", Date: "2025-03-10"},
					{Code: "bad_number", Value: "amount", Date: "2025-03-10"},
					{Code: "bad_number", Value: "amount", Date: "2025-03-10"},
					{Code: "bad_number", Value: "quantity", Date: "2025-03-11"},
					{Code: "bad_number", Value: "price", Date: "2025-03-12"},
				},
			},
		},
		{
			name: "gold certificate series suffix",
			rows: [][]string{
				{"date", "type", "symbol", "quantity", "price"},
				{"2025-03-05", "buy", "ALTIN.S1", "10", "30"},
				{"2025-03-06", "buy", "altin.s1", "5", "31"},
			},
			want: statement.Statement{
				Broker: "csv", Currency: "TRY",
				Trades: []statement.Trade{
					{Date: "2025-03-05", Symbol: "ALTIN", Side: "buy", Quantity: d("10"), Price: d("30"), Amount: d("300")},
					{Date: "2025-03-06", Symbol: "ALTIN", Side: "buy", Quantity: d("5"), Price: d("31"), Amount: d("155")},
				},
			},
		},
		{
			name: "market names the app shows, and an unknown market",
			rows: [][]string{
				{"date", "type", "symbol", "quantity", "price", "amount", "Piyasa"},
				{"2026-04-01", "buy", "BTC-TRY", "1", "10", "", "Kripto"},
				{"2026-04-01", "buy", "GRAM", "1", "10", "", "Altın"},
				{"2026-04-01", "buy", "GRAM", "1", "10", "", "ALTIN"},
				{"2026-04-01", "buy", "AAPL", "1", "10", "", "ABD"},
				{"2026-04-01", "buy", "THYAO", "1", "10", "", "bist"},
				{"2026-04-01", "buy", "GRAM", "1", "10", "", "GOLD"},
				{"2026-04-01", "buy", "THYAO", "1", "10", "", ""},
				{"2026-04-02", "dividend", "THYAO", "", "", "5", "XYZ"},
				{"2026-04-03", "deposit", "", "", "", "5", "xyz"},
			},
			want: statement.Statement{
				Broker: "csv", Currency: "TRY",
				Trades: []statement.Trade{
					{Date: "2026-04-01", Symbol: "BTC-TRY", Side: "buy", Quantity: d("1"), Price: d("10"), Amount: d("10"), Market: "CRYPTO"},
					{Date: "2026-04-01", Symbol: "GRAM", Side: "buy", Quantity: d("1"), Price: d("10"), Amount: d("10"), Market: "GOLD"},
					{Date: "2026-04-01", Symbol: "GRAM", Side: "buy", Quantity: d("1"), Price: d("10"), Amount: d("10"), Market: "GOLD"},
					{Date: "2026-04-01", Symbol: "AAPL", Side: "buy", Quantity: d("1"), Price: d("10"), Amount: d("10"), Market: "US"},
					{Date: "2026-04-01", Symbol: "THYAO", Side: "buy", Quantity: d("1"), Price: d("10"), Amount: d("10"), Market: "BIST"},
					{Date: "2026-04-01", Symbol: "GRAM", Side: "buy", Quantity: d("1"), Price: d("10"), Amount: d("10"), Market: "GOLD"},
					{Date: "2026-04-01", Symbol: "THYAO", Side: "buy", Quantity: d("1"), Price: d("10"), Amount: d("10")},
				},
				Errors: []statement.ParseError{
					{Code: "unknown_market", Value: "XYZ", Date: "2026-04-02"},
					{Code: "unknown_market", Value: "xyz", Date: "2026-04-03"},
				},
			},
		},
		{
			name: "majority currency",
			rows: [][]string{
				{"DATE", "TYPE", "AMOUNT", "CURRENCY"},
				{"2025-01-02", "DEPOSIT", "1", "usd"},
				{"2025-01-03", "deposit", "1", "USD"},
				{"2025-01-01", "deposit", "1", ""},
			},
			want: statement.Statement{
				Broker: "csv", Currency: "USD",
				Cash: []statement.CashMovement{
					{Date: "2025-01-02", Kind: "deposit", Amount: d("1"), Currency: "USD"},
					{Date: "2025-01-03", Kind: "deposit", Amount: d("1"), Currency: "USD"},
					{Date: "2025-01-01", Kind: "deposit", Amount: d("1"), Currency: "TRY"},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := csv.Parse(tc.rows)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got.Broker != tc.want.Broker || got.PeriodStart != tc.want.PeriodStart ||
				got.PeriodEnd != tc.want.PeriodEnd || got.Currency != tc.want.Currency || got.CashBalance != nil {
				t.Errorf("header = %s %s %s %s, want %s %s %s %s", got.Broker, got.PeriodStart, got.PeriodEnd, got.Currency,
					tc.want.Broker, tc.want.PeriodStart, tc.want.PeriodEnd, tc.want.Currency)
			}
			equal(t, "trades", got.Trades, tc.want.Trades, func(a, b statement.Trade) bool {
				return a.Date == b.Date && a.Symbol == b.Symbol && a.Side == b.Side && a.Quantity.Equal(b.Quantity) &&
					a.Price.Equal(b.Price) && a.Amount.Equal(b.Amount) && a.Fee.Equal(b.Fee) &&
					a.Currency == b.Currency && a.Market == b.Market
			})
			equal(t, "dividends", got.Dividends, tc.want.Dividends, func(a, b statement.Dividend) bool {
				return a.Date == b.Date && a.Symbol == b.Symbol && a.Gross.Equal(b.Gross) && a.Tax.Equal(b.Tax) &&
					a.Currency == b.Currency && a.Market == b.Market
			})
			equal(t, "cash", got.Cash, tc.want.Cash, func(a, b statement.CashMovement) bool {
				return a.Date == b.Date && a.Kind == b.Kind && a.Amount.Equal(b.Amount) && a.Fee.Equal(b.Fee) &&
					a.Currency == b.Currency && a.Description == ""
			})
			equal(t, "errors", got.Errors, tc.want.Errors, func(a, b statement.ParseError) bool {
				return a.Code == b.Code && a.Value == b.Value && a.Date == b.Date
			})
			if len(got.Holdings) != 0 {
				t.Errorf("holdings = %v, want none", got.Holdings)
			}
		})
	}
}

func equal[T any](t *testing.T, what string, got, want []T, eq func(a, b T) bool) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %+v, want %+v", what, got, want)
		return
	}
	for i := range got {
		if !eq(got[i], want[i]) {
			t.Errorf("%s[%d] = %+v, want %+v", what, i, got[i], want[i])
		}
	}
}

func TestParseNotACSVStatement(t *testing.T) {
	for _, rows := range [][][]string{
		nil,
		{{"type", "symbol", "amount"}, {"buy", "THYAO", "1"}},
		{{"tarih", "sembol"}, {"2025-01-01", "THYAO"}},
	} {
		if _, err := csv.Parse(rows); err == nil || err.Error() != "not a csv statement" {
			t.Errorf("Parse(%v) error = %v, want not a csv statement", rows, err)
		}
	}
}

// TestParseNoRows checks that a header with no data row under it has no
// period, so it is refused instead of previewing as an empty statement.
func TestParseNoRows(t *testing.T) {
	for _, rows := range [][][]string{
		{{"date", "type", "amount"}},
		{{"tarih", "işlem"}, {"", ""}, {" "}},
	} {
		if _, err := csv.Parse(rows); err == nil || err.Error() != "no rows" {
			t.Errorf("Parse(%v) error = %v, want no rows", rows, err)
		}
	}
}

func TestParserRegistered(t *testing.T) {
	if statement.Parsers["csv"] == nil {
		t.Fatal(`statement.Parsers["csv"] is nil`)
	}
}
