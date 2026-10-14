package midas_test

import (
	"fmt"
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/statement"
	_ "github.com/dogauzun/gelirgider/backend/internal/statement/garanti"
	_ "github.com/dogauzun/gelirgider/backend/internal/statement/garantiyatirim"
	"github.com/dogauzun/gelirgider/backend/internal/statement/midas"
	"github.com/shopspring/decimal"
)

// usdSample is a USD account statement as pdftext.Rows returns it. It is
// synthetic: the layout is copied from a real statement, but every name,
// number and date is invented. Real statements never enter the repository.
func usdSample() [][]string {
	return [][]string{
		{"Midas Menkul Değerler A.Ş."},
		{"Mersis Numarası:", "0000000000000000"},
		{"Ticari Sicil Numarası:", "000000-0"},
		{"Adres:", "ŞİRKET ADRESİ"},
		{"www.midasmenkul.com"},
		{"01/09/25", "-", "30/09/25", "HESAP EKSTRESİ"},
		{"Müşteri Adı Soyadı", ":", "AD", "SOYAD"},
		{"TCKN", ":", "00000000000"},
		{"Adres", ":", "MÜŞTERİ ADRESİ 1"},
		{"MÜŞTERİ ADRESİ 2"},
		{"Hesap Açılış Tarihi", ":", "15/03/23"},
		{"Nakit Bakiye", ":", "0.74 USD"},
		{"Toplam Portföy Değeri", ":", "2215.36 USD"},
		{"İşlem Hacmi", ":", "4,18 USD"},
		{"PORTFÖY ÖZETİ (", "30/09/25", ")"},
		{"Sermaye Piyasası Aracı", "Adet", "Hisse Başı Ortalama Maliyet", "Kar / Zarar*", "Toplam Değeri (YP)"},
		{"QQQ - NASDAQ 100 Endeks Fo...", "1", "587,42 USD", "21,67 USD", "609,09 USD"},
		{"VTI - ABD Tüm Borsa Endeks...", "3,006218342", "318,54 USD", "33,76 USD", "991,36 USD"},
		{"VOO - S&P 500 Endeks Fonu...", "1,003417529", "597,13 USD", "15,00 USD", "614,17 USD"},
		{"*Kar zarar", "30/09/25", "gün sonu piyasa kapanış fiyatı ile hesaplanmıştır."},
		{"YATIRIM İŞLEMLERİ (", "01/09/25", "-", "30/09/25", ")"},
		{"Tarih", "İşlem Türü", "Sembol", "İşlem Tipi", "İşlem Durumu", "Para Birimi", "Emir Adedi", "Emir Tutarı", "Gerçekleşen Adet", "Ortalama İşlem Fiyatı", "İşlem Ücreti", "İşlem Tutarı"},
		{"26/09/25 18:52:07", "Piyasa Emri", "VTI", "Alış", "Gerçekleşti", "USD", "-", "2,06", "0,006218342", "331,82", "0,00", "2,06"},
		{"26/09/25 21:14:33", "Piyasa Emri", "VOO", "Alış", "Gerçekleşti", "USD", "-", "2,12", "0,003417529", "621,45", "0,00", "2,12"},
		{"HESAP İŞLEMLERİ (", "01/09/25", "-", "30/09/25", ")"},
		{"Talep Tarihi", "İşlem Tarihi", "İşlem Tipi", "İşlem Açıklaması", "İşlem Durumu", "Tutar (YP)"},
		{"11/09/25 10:41:12", "11/09/25 10:41:15", "Para Yatırma", "BANKA ADI - TR000000000000000000000000", "Gerçekleşti", "8237,46 USD"},
		{"11/09/25 10:41:48", "11/09/25 10:41:48", "Döviz Satış", "41,18 USD/TRY kurundan 8.237,46 dolar satış", "Gerçekleşti", "8237,46 USD"},
		{"TEMETTÜ İŞLEMLERİ (", "01/09/25", "-", "30/09/25", ")"},
		{"Ödeme Tarihi", "Sermaye Piyasası Aracı", "Brüt Temettü Tutarı", "Stopaj*", "Net Temettü Tutarı"},
		{"26/09/25", "VOO - S&P 500 Endeks Fonu...", "2,65 USD", "0,53 USD", "2,12 USD"},
		{"26/09/25", "VTI - ABD Tüm Borsa Endeks...", "2,58 USD", "0,52 USD", "2,06 USD"},
		{"30/09/25", "QQQ - NASDAQ 100 Endeks Fo...", "0,93 USD", "0,19 USD", "0,74 USD"},
		{"*Stopaj, Amerika Gelir İdaresi (IRS)’ne adınıza ödenen gelir vergisini temsil etmektedir."},
		{"HİSSE TRANSFERLERİ (", "01/09/25", "-", "30/09/25", ")"},
		{"Kayıt bulunmamaktadır."},
		{"* Saklama hesabınızda bulunan kaydileştirilmiş sermaye piyasası araçlarına ilişkin bakiye ve hesap bilgilerinize", "e-YATIRIMCI", "internet ve mobil uygulaması üzerinden"},
		{"günün her saatinde ulaşabilir", "veya 444 0 655", "(MKK) numaralı MKK İletişim Merkezi (MİM) aracılığı ile hesaplarınızı kolayca takip edebilirsiniz."},
		{"hafta sonu ise saat sınırlaması bulunmamaktadır. Koyduğunuz blokajı dilediğiniz zaman kaldırabilirsiniz."},
		{"1", "/", "1"},
	}
}

// trySample is a TRY account statement, synthetic in the same way. Its HESAP
// İŞLEMLERİ section has no column header row.
func trySample() [][]string {
	return [][]string{
		{"Midas Menkul Değerler A.Ş."},
		{"Mersis Numarası:", "0000000000000000"},
		{"Ticari Sicil Numarası:", "000000-0"},
		{"Adres:", "ŞİRKET ADRESİ"},
		{"www.midasmenkul.com"},
		{"01/04/26", "-", "30/04/26", "HESAP EKSTRESİ"},
		{"Müşteri Adı Soyadı", ":", "AD", "SOYAD"},
		{"TCKN", ":", "00000000000"},
		{"Adres", ":", "MÜŞTERİ ADRESİ 1"},
		{"MÜŞTERİ ADRESİ 2"},
		{"Hesap Açılış Tarihi", ":", "15/03/23"},
		{"Nakit Bakiye", ":", "27.46 TRY"},
		{"Toplam Portföy Değeri", ":", "88702.860000 TRY"},
		{"İşlem Hacmi", ":", "831,30 TRY"},
		{"PORTFÖY ÖZETİ (", "30/04/26", ")"},
		{"Sermaye Piyasası Aracı", "Adet", "Hisse Başı Ortalama Maliyet", "Kar / Zarar*", "Toplam Değeri (YP)"},
		{"OPT25.F - Osmanlı Portföy BIST...", "1870", "48,63 TRY", "-2262,70 TRY", "88675,40 TRY"},
		{"*Kar zarar", "30/04/26", "gün sonu piyasa kapanış fiyatı ile hesaplanmıştır."},
		{"YATIRIM İŞLEMLERİ (", "01/04/26", "-", "30/04/26", ")"},
		{"Tarih", "İşlem Türü", "Sembol", "İşlem Tipi", "İşlem Durumu", "Para Birimi", "Emir Adedi", "Emir Tutarı", "Gerçekleşen Adet", "Ortalama İşlem Fiyatı", "İşlem Ücreti", "İşlem Tutarı"},
		{"08/04/26 14:03:19", "Piyasa Emri", "OPT25.F", "Alış", "Gerçekleşti", "TRY", "17", "-", "17", "48,90", "0,00", "831,30"},
		{"HESAP İŞLEMLERİ (", "01/04/26", "-", "30/04/26", ")"},
		{"01/04/26", "30/04/26", "Ücretsiz İşlem", "Sıfır Komisyonlu İşlem", "Gerçekleşti", "0,31827415 TRY"},
		{"22/04/26 15:08:29", "22/04/26 15:08:29", "Döviz Satış", "41,96 USD/TRY kurundan 61.208,77 lira alış", "Gerçekleşti", "61208,77 TRY"},
		{"22/04/26 00:00:00", "22/04/26 00:00:00", "Diğer Gelir", "Aylık Nema Geliri", "Gerçekleşti", "47,93 TRY"},
		{"TEMETTÜ İŞLEMLERİ (", "01/04/26", "-", "30/04/26", ")"},
		{"Ödeme Tarihi", "Sermaye Piyasası Aracı", "Brüt Temettü Tutarı", "Stopaj*", "Net Temettü Tutarı"},
		{"14/04/26", "OPT25.F - Osmanlı Portföy BIST...", "812,47 TRY", "0,00 TRY", "812,47 TRY"},
		{"HİSSE TRANSFERLERİ (", "01/04/26", "-", "30/04/26", ")"},
		{"Kayıt bulunmamaktadır."},
		{"1", "/", "1"},
	}
}

func dec(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("bad test decimal %q: %v", s, err)
	}
	return d
}

func TestParseUSDStatement(t *testing.T) {
	st, err := midas.Parse(usdSample())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if st.Broker != "midas" || st.Currency != "USD" {
		t.Errorf("broker/currency = %q/%q, want midas/USD", st.Broker, st.Currency)
	}
	if st.PeriodStart != "2025-09-01" || st.PeriodEnd != "2025-09-30" {
		t.Errorf("period = %s..%s, want 2025-09-01..2025-09-30", st.PeriodStart, st.PeriodEnd)
	}
	if len(st.Errors) != 0 {
		t.Fatalf("errors = %+v, want none", st.Errors)
	}

	if len(st.Trades) != 2 {
		t.Fatalf("trades = %d, want 2", len(st.Trades))
	}
	want := statement.Trade{
		Date: "2025-09-26", Symbol: "VTI", Side: "buy",
		Quantity: dec(t, "0.006218342"), Price: dec(t, "331.82"),
		Amount: dec(t, "2.06"), Fee: dec(t, "0"), Currency: "USD",
	}
	if got := st.Trades[0]; got.Date != want.Date || got.Symbol != want.Symbol || got.Side != want.Side ||
		!got.Quantity.Equal(want.Quantity) || !got.Price.Equal(want.Price) ||
		!got.Amount.Equal(want.Amount) || !got.Fee.Equal(want.Fee) || got.Currency != want.Currency {
		t.Errorf("trade[0] = %+v, want %+v", got, want)
	}
	if st.Trades[1].Symbol != "VOO" || !st.Trades[1].Quantity.Equal(dec(t, "0.003417529")) {
		t.Errorf("trade[1] = %+v", st.Trades[1])
	}

	// The deposit, then the Döviz Satış as this account's own leg: the
	// dollars leave. The lira side is on the TRY statement (GLG-363).
	wantCash := []struct{ kind, currency, amount string }{
		{"deposit", "USD", "8237.46"},
		{"withdrawal", "USD", "8237.46"},
	}
	if len(st.Cash) != len(wantCash) {
		t.Fatalf("cash = %+v, want %d rows", st.Cash, len(wantCash))
	}
	for i, w := range wantCash {
		if c := st.Cash[i]; c.Kind != w.kind || c.Currency != w.currency || !c.Amount.Equal(dec(t, w.amount)) || c.Date != "2025-09-11" {
			t.Errorf("cash[%d] = %+v, want %s %s %s", i, c, w.kind, w.amount, w.currency)
		}
	}
	if d := st.Cash[1].Description; d != "41,18 USD/TRY kurundan 8.237,46 dolar satış" {
		t.Errorf("fx description = %q", d)
	}

	if len(st.Skipped) != 0 {
		t.Errorf("skipped = %+v, want none", st.Skipped)
	}

	if len(st.Dividends) != 3 {
		t.Fatalf("dividends = %d, want 3", len(st.Dividends))
	}
	div := st.Dividends[0]
	if div.Date != "2025-09-26" || div.Symbol != "VOO" || !div.Gross.Equal(dec(t, "2.65")) ||
		!div.Tax.Equal(dec(t, "0.53")) || div.Currency != "USD" {
		t.Errorf("dividend[0] = %+v", div)
	}

	if len(st.Holdings) != 3 {
		t.Fatalf("holdings = %d, want 3", len(st.Holdings))
	}
	if h := st.Holdings[1]; h.Symbol != "VTI" || !h.Quantity.Equal(dec(t, "3.006218342")) ||
		!h.AvgCost.Equal(dec(t, "318.54")) || h.Currency != "USD" {
		t.Errorf("holding[1] = %+v", h)
	}
}

// TestParseHoldingPrice covers GLG-816: a holding's price is Toplam Değeri /
// Adet rounded to 6 places, the period-end price an opening position falls
// back on (GLG-737). An unreadable value leaves it zero and is no error.
func TestParseHoldingPrice(t *testing.T) {
	rows := usdSample()
	rows[18] = []string{"VOO - S&P 500 Endeks Fonu...", "1,003417529", "597,13 USD", "15,00 USD", "-"}
	st, err := midas.Parse(rows)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(st.Errors) != 0 {
		t.Fatalf("errors = %+v, want none", st.Errors)
	}
	want := []string{"609.09", "329.769793", "0"}
	if len(st.Holdings) != len(want) {
		t.Fatalf("holdings = %+v, want %d", st.Holdings, len(want))
	}
	for i, w := range want {
		if h := st.Holdings[i]; !h.Price.Equal(dec(t, w)) || !h.Quantity.IsPositive() {
			t.Errorf("holding[%d] %s price = %s, want %s", i, h.Symbol, h.Price, w)
		}
	}
}

func TestParseTRYStatement(t *testing.T) {
	st, err := midas.Parse(trySample())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if st.Currency != "TRY" || st.PeriodStart != "2026-04-01" || st.PeriodEnd != "2026-04-30" {
		t.Errorf("currency/period = %q %s..%s", st.Currency, st.PeriodStart, st.PeriodEnd)
	}
	if len(st.Errors) != 0 {
		t.Fatalf("errors = %+v, want none", st.Errors)
	}

	if len(st.Trades) != 1 {
		t.Fatalf("trades = %d, want 1", len(st.Trades))
	}
	trade := st.Trades[0]
	if trade.Symbol != "OPT25" || trade.Side != "buy" || trade.Date != "2026-04-08" ||
		!trade.Quantity.Equal(dec(t, "17")) || !trade.Price.Equal(dec(t, "48.90")) ||
		!trade.Amount.Equal(dec(t, "831.30")) || !trade.Fee.Equal(dec(t, "0")) || trade.Currency != "TRY" {
		t.Errorf("trade[0] = %+v", trade)
	}

	// The header-less HESAP İŞLEMLERİ section still reads its first row,
	// the commission rebate, as data instead of mistaking it for a column
	// header. Döviz Satış on the TRY account: only the lira bought arrive.
	// Then the monthly interest (Diğer Gelir), a dividend with no instrument
	// that keeps its description (GLG-533).
	if len(st.Cash) != 3 {
		t.Fatalf("cash = %+v, want the rebate, the lira leg and the interest", st.Cash)
	}
	if c := st.Cash[0]; c.Kind != "deposit" || !c.Amount.Equal(dec(t, "0.31827415")) || c.Description != "Sıfır Komisyonlu İşlem" {
		t.Errorf("cash[0] = %+v", c)
	}
	if c := st.Cash[1]; c.Kind != "deposit" || c.Currency != "TRY" || !c.Amount.Equal(dec(t, "61208.77")) || c.Date != "2026-04-22" ||
		c.Description != "41,96 USD/TRY kurundan 61.208,77 lira alış" {
		t.Errorf("cash[1] = %+v", c)
	}
	if c := st.Cash[2]; c.Kind != "dividend" || c.Currency != "TRY" || !c.Amount.Equal(dec(t, "47.93")) || c.Date != "2026-04-22" ||
		c.Description != "Aylık Nema Geliri" {
		t.Errorf("cash[2] = %+v", c)
	}
	if len(st.Skipped) != 0 {
		t.Fatalf("skipped = %+v, want none", st.Skipped)
	}

	if len(st.Dividends) != 1 {
		t.Fatalf("dividends = %d, want 1", len(st.Dividends))
	}
	if d := st.Dividends[0]; d.Symbol != "OPT25" || d.Date != "2026-04-14" ||
		!d.Gross.Equal(dec(t, "812.47")) || !d.Tax.Equal(dec(t, "0")) || d.Currency != "TRY" {
		t.Errorf("dividend[0] = %+v", d)
	}

	if len(st.Holdings) != 1 {
		t.Fatalf("holdings = %d, want 1", len(st.Holdings))
	}
	if h := st.Holdings[0]; h.Symbol != "OPT25" || !h.Quantity.Equal(dec(t, "1870")) || !h.AvgCost.Equal(dec(t, "48.63")) {
		t.Errorf("holding[0] = %+v", h)
	}
}

// Ücretsiz İşlem is a credit Midas pays into the cash account; skipping it
// left cash below the statement's Nakit Bakiye (GLG-1187).
func TestParseCommissionRebate(t *testing.T) {
	rows := append(trySample()[:21:21],
		[]string{"HESAP İŞLEMLERİ (", "01/02/26", "-", "28/02/26", ")"},
		[]string{"01/02/26", "28/02/26", "Ücretsiz İşlem", "Sıfır Komisyonlu İşlem", "Gerçekleşti", "12,34500000 TRY"},
	)
	st, err := midas.Parse(rows)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(st.Errors) != 0 || len(st.Skipped) != 0 {
		t.Fatalf("errors = %+v, skipped = %+v, want none", st.Errors, st.Skipped)
	}
	if len(st.Cash) != 1 {
		t.Fatalf("cash = %+v, want one rebate", st.Cash)
	}
	if c := st.Cash[0]; c.Kind != "deposit" || c.Currency != "TRY" || !c.Amount.Equal(dec(t, "12.345")) || c.Date != "2026-02-28" ||
		c.Description != "Sıfır Komisyonlu İşlem" {
		t.Errorf("cash[0] = %+v", c)
	}
}

func TestParsePartialRows(t *testing.T) {
	rows := append(trySample()[:21:21],
		[]string{"06/04/26 10:00:00", "Limit Emri", "OPT25.F", "Alış", "İptal Edildi", "TRY", "10", "-", "0", "0,00", "0,00", "0,00"},
		[]string{"07/04/26 10:00:00", "Piyasa Emri", "OPT25.F", "Alış", "Gerçekleşti", "TRY", "5", "-", "beş", "48,90", "0,00", "244,50"},
		[]string{"08/04/26 10:00:00", "Piyasa Emri", "OPT25.F", "Satış", "Gerçekleşti", "TRY", "1", "-", "1", "49,05", "0,00", "49,05"},
	)
	st, err := midas.Parse(rows)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// The good rows before and after the broken one still parse.
	if len(st.Trades) != 2 || st.Trades[1].Side != "sell" || st.Trades[1].Date != "2026-04-08" {
		t.Fatalf("trades = %+v, want the two executed rows", st.Trades)
	}
	if len(st.Skipped) != 1 || st.Skipped[0].Reason != "not executed" {
		t.Errorf("skipped = %+v, want one not executed", st.Skipped)
	}
	if len(st.Errors) != 1 {
		t.Fatalf("errors = %+v, want one", st.Errors)
	}
	if e := st.Errors[0]; e.Date != "2026-04-07" || e.Code != "unreadable_quantity" || e.Message != "unreadable quantity" || e.Section != "YATIRIM İŞLEMLERİ" {
		t.Errorf("error[0] = %+v", e)
	}
}

// An exponent is not a number the statement prints: unchecked, "1e2000000000"
// panics on Mul and "1e3000000" renders a multi-MB preview (GLG-629).
func TestParseExponentIsUnreadable(t *testing.T) {
	for _, cell := range []string{"1e2000000000", "1e3000000", "-1E5"} {
		rows := append(trySample()[:20:20],
			[]string{"07/04/26 10:00:00", "Piyasa Emri", "OPT25.F", "Alış", "Gerçekleşti", "TRY", "5", "-", cell, "48,90", "0,00", "244,50"},
		)
		st, err := midas.Parse(rows)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if len(st.Trades) != 0 || len(st.Errors) != 1 || st.Errors[0].Code != "unreadable_quantity" {
			// Counts only: printing the trade would materialize the exponent.
			t.Errorf("%q: %d trades, %d errors, want one unreadable_quantity error", cell, len(st.Trades), len(st.Errors))
		}
	}
}

// A section that runs over a page break meets the company header and the
// period row again in the middle of its rows.
func TestParsePageBreakInsideSection(t *testing.T) {
	rows := append(trySample()[:21:21],
		[]string{"1", "/", "2"},
		[]string{"Midas Menkul Değerler A.Ş."},
		[]string{"Mersis Numarası:", "0000000000000000"},
		[]string{"Adres:", "ŞİRKET ADRESİ"},
		[]string{"www.midasmenkul.com"},
		[]string{"01/04/26", "-", "30/04/26", "HESAP EKSTRESİ"},
		[]string{"06/04/26 10:00:00", "Piyasa Emri", "OPT25.F", "Satış", "Gerçekleşti", "TRY", "1", "-", "1", "49,05", "0,00", "49,05"},
		[]string{"2", "/", "2"},
	)
	st, err := midas.Parse(rows)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if st.PeriodStart != "2026-04-01" || st.PeriodEnd != "2026-04-30" {
		t.Errorf("period = %s..%s", st.PeriodStart, st.PeriodEnd)
	}
	if len(st.Errors) != 0 || len(st.Skipped) != 0 {
		t.Fatalf("errors = %+v, skipped = %+v, want none", st.Errors, st.Skipped)
	}
	if len(st.Trades) != 2 || st.Trades[1].Side != "sell" {
		t.Fatalf("trades = %+v, want the rows from both pages", st.Trades)
	}
}

func TestParseUnknownCashType(t *testing.T) {
	rows := append(trySample()[:22:22],
		[]string{"01/04/26", "02/04/26", "Bilinmeyen İşlem", "açıklama", "Gerçekleşti", "1,00 TRY"},
	)
	st, err := midas.Parse(rows)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(st.Errors) != 1 || st.Errors[0].Code != "unknown_cash_type" || st.Errors[0].Value != "Bilinmeyen İşlem" || st.Errors[0].Date != "2026-04-02" {
		t.Fatalf("errors = %+v", st.Errors)
	}
}

// Diğer Gider is a tax or expense debit, so far only the Stopaj on the
// monthly interest: a fee for the positive amount, keeping its description
// (GLG-1177).
func TestParseOtherExpenseIsFee(t *testing.T) {
	rows := append(trySample()[:22:22],
		[]string{"23/09/26 00:00:00", "23/09/26 00:00:00", "Diğer Gider", "Stopaj", "Gerçekleşti", "-66,15 TRY"},
	)
	st, err := midas.Parse(rows)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(st.Errors) != 0 {
		t.Fatalf("errors = %+v, want none", st.Errors)
	}
	if len(st.Cash) != 1 {
		t.Fatalf("cash = %+v, want one fee", st.Cash)
	}
	if c := st.Cash[0]; c.Kind != "fee" || !c.Amount.Equal(dec(t, "66.15")) || c.Currency != "TRY" || c.Description != "Stopaj" ||
		c.Date != "2026-09-23" || !c.Fee.IsZero() {
		t.Errorf("cash[0] = %+v", c)
	}
}

// A conversion books only the amount cell's own currency (GLG-363). The first
// two rows are the same conversion as printed on the USD and on the TRY
// account: importing both statements gives each currency one leg.
func TestParseFXConversionOwnLeg(t *testing.T) {
	cases := []struct {
		name, kind, desc, amount string
		want                     string // "kind amount currency", or "" for skipped
	}{
		{"USD account", "Döviz Satış", "43,25 USD/TRY kurundan 8.237,46 dolar satış", "8237,46 USD", "withdrawal 8237.46 USD"},
		{"TRY account", "Döviz Satış", "43,25 USD/TRY kurundan 356.270,15 lira alış", "356270,15 TRY", "deposit 356270.15 TRY"},
		{"dollars bought", "Döviz Alış", "43,25 USD/TRY kurundan 100,00 dolar alış", "100,00 USD", "deposit 100 USD"},
		{"lira sold", "Döviz Alış", "43,25 USD/TRY kurundan 4.325,00 lira satış", "4325,00 TRY", "withdrawal 4325 TRY"},
		{"unknown currency", "Döviz Alış", "50,10 EUR/TRY kurundan 100,00 avro alış", "100,00 EUR", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows := append(trySample()[:22:22],
				[]string{"09/02/26 10:00:00", "09/02/26 10:00:00", c.kind, c.desc, "Gerçekleşti", c.amount},
			)
			st, err := midas.Parse(rows)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if c.want == "" {
				if len(st.Cash) != 0 || len(st.Skipped) != 1 || st.Skipped[0].Reason != "fx conversion" {
					t.Fatalf("cash = %+v, skipped = %+v, want one fx conversion skip", st.Cash, st.Skipped)
				}
				return
			}
			if len(st.Cash) != 1 || st.Cash[0].Date != "2026-02-09" || st.Cash[0].Description != c.desc {
				t.Fatalf("cash = %+v, want one leg on 2026-02-09 with the printed description", st.Cash)
			}
			if got := st.Cash[0].Kind + " " + st.Cash[0].Amount.String() + " " + st.Cash[0].Currency; got != c.want {
				t.Errorf("leg = %s, want %s", got, c.want)
			}
		})
	}
}

// Table cells are Turkish even without a comma: "1.000" is a thousand
// (GLG-498).
func TestParseTurkishThousands(t *testing.T) {
	cases := map[string]string{
		"1.000 TRY":        "1000",
		"+1.000 TRY":       "1000",
		"1.000,00 TRY":     "1000",
		"1.234.567,89 TRY": "1234567.89",
		"47,93 TRY":        "47.93",
		"500 TRY":          "500",
		"0.740 TRY":        "0.74",
	}
	for cell, want := range cases {
		rows := append(trySample()[:22:22],
			[]string{"09/02/26 10:00:00", "09/02/26 10:00:00", "Para Yatırma", "BANKA ADI", "Gerçekleşti", cell},
		)
		st, err := midas.Parse(rows)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if len(st.Cash) != 1 || !st.Cash[0].Amount.Equal(dec(t, want)) {
			t.Errorf("%q: cash = %+v, want %s", cell, st.Cash, want)
		}
	}
}

// With a comma the dots are thousands, so they must be 3-digit groups: a
// broken group fails the row instead of booking another number (GLG-908).
func TestParseMalformedThousandsIsUnreadable(t *testing.T) {
	for _, cell := range []string{"1.2.3,4 TRY", "1000.000,5 TRY", "1.23,4 TRY"} {
		rows := append(trySample()[:22:22],
			[]string{"09/02/26 10:00:00", "09/02/26 10:00:00", "Para Yatırma", "BANKA ADI", "Gerçekleşti", cell},
		)
		st, err := midas.Parse(rows)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if len(st.Cash) != 0 || len(st.Errors) != 1 || st.Errors[0].Code != "unreadable_amount" {
			t.Errorf("%q: cash = %+v, errors = %+v, want one unreadable_amount error", cell, st.Cash, st.Errors)
		}
	}
}

// The header block prints plain decimals, so a Nakit Bakiye of "1.500 USD"
// is one and a half, unlike a table cell's "1.000" (GLG-419).
func TestParseCashBalance(t *testing.T) {
	cases := map[string]string{
		"0.74 USD":         "0.74",
		"27.46 TRY":        "27.46",
		"88702.860000 TRY": "88702.86",
		"1.500 USD":        "1.5",
		"1.500,25 TRY":     "1500.25",
	}
	for cell, want := range cases {
		rows := trySample()
		rows[11] = []string{"Nakit Bakiye", ":", cell}
		st, err := midas.Parse(rows)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if st.CashBalance == nil || !st.CashBalance.Equal(dec(t, want)) {
			t.Errorf("%q: cash balance = %v, want %s", cell, st.CashBalance, want)
		}
	}
}

func TestParseNotAStatement(t *testing.T) {
	if _, err := midas.Parse([][]string{{"bir"}, {"iki", "üç"}}); err == nil {
		t.Fatal("want an error when there is no period row")
	}
}

// TestDetect covers GLG-764: with every PDF parser registered, a Midas
// statement is detected as midas and parses as an explicit pick does.
func TestDetect(t *testing.T) {
	key, got, err := statement.Detect(usdSample())
	if err != nil || key != "midas" {
		t.Fatalf("Detect = %q, %v; want midas", key, err)
	}
	want, _ := midas.Parse(usdSample())
	if g, w := fmt.Sprintf("%+v", got), fmt.Sprintf("%+v", want); g != w {
		t.Errorf("statement\n got %s\nwant %s", g, w)
	}
}

func TestParserRegistered(t *testing.T) {
	if statement.Parsers["midas"] == nil {
		t.Fatal(`statement.Parsers["midas"] is nil`)
	}
}
