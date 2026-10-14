package garantiyatirim_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/statement"
	_ "github.com/dogauzun/gelirgider/backend/internal/statement/garanti"
	"github.com/dogauzun/gelirgider/backend/internal/statement/garantiyatirim"
	_ "github.com/dogauzun/gelirgider/backend/internal/statement/midas"
	"github.com/shopspring/decimal"
)

// header, page1, page2, page3, shares and ledger make a synthetic sample
// statement: the layout is copied from a real Garanti BBVA Yatırım statement,
// but every name, number and date is invented. Real statements never enter
// the repository.
func header() []string {
	return []string{"<broker address>", "<trade registry>", "www.garantibbvayatirim.com.tr", "31/03/2024", "AD SOYAD", "ADRES 1", "ADRES 2", "ADRES 3", "(Birim : 6)"}
}

func page(cells ...string) []string { return append(header(), cells...) }

// page1 is the Hesap Durumu page with the given holding cells; the legal-text
// word cells are shortened to a few.
func page1(holdings ...string) []string {
	cells := []string{
		"\"MART ayına ait saklama ücreti borcunuz 0.00 TL olup, takip eden ayın 10. gününde hesabınıza borç", "geçileceğinden, saklama ücretini hesabınızda bulundurmanızı rica ederiz.\"",
		"Hesap Durumu", "Emanet No", ":", "000000-000", "Tarih", ":", "31/03/2024", "Sayfa Numarası", ":", "1",
		"Cari Bakiye", "Mevcudu", "Rezervi (T + 1)", "7.15", "0.00",
		"MEVCUT MENKUL KIYMETLER", "Kıymetin", "Cinsi", "Kodu /", "Vadesi", "Unvanı /", "Tanımı", "Mevcut", "Nominal / Adet", "Piyasa", "Değeri (TL)", "Rezerv", "Nominal / Adet",
		"Garanti", "BBVA", "İnternet", "MKK", "SMS", "ve", "05.10.2009", "482", "http://www.tspak.org.tr",
		"Portföy değeri", "toplamı(TL ) :", "6,082.30", "Hatalar ve unutmalar istisnadır.", "Sayfa 1 / 3",
	}
	cells = append(cells, holdings...)
	return page(append(cells, "Hisse toplamı :", "83.000", "6,082.30", "0.000")...)
}

func page2() []string {
	return page(
		"Menkul Kıymet Hareket Listesi", "Emanet No", ":", "000000-000", "Dönemi", ":", "01/03/2024 - 31/03/2024", "Sayfa Numarası", ":", "2",
		"Kodu /", "Vadesi", "Adı /", "Tanımı", "Tarihi", "Nominal/Adet", "İşlem", "Kodu", "Fiyatı", "Hatalar ve unutmalar istisnadır.", "Sayfa 2 / 3",
		"ASELS", "ASELSAN ELEKTRONİK SANAYİ", "14/03/2024", "64.000", "HS Alış", "52.850000",
		"ARCLK", "ARÇELİK A.Ş.", "14/03/2024", "19.000", "HS Alış", "142.100000",
	)
}

// page3 is the Hesap Ekstresi page with the given ledger records.
func page3(records ...string) []string {
	return page(append([]string{
		"Hesap Ekstresi", "Emanet No", ":", "000000-000", "Dönemi", ":", "01/03/2024 - 31/03/2024", "Sayfa Numarası", ":", "3",
		"Tarih", "Açıklama", "İşlem", "Kalan", "Hatalar ve unutmalar istisnadır.", "Sayfa 3 / 3",
	}, records...)...)
}

var shares = []string{
	"HS", "ASELS", "ASELSAN ELEKTRONİK SANAYİ", "64.000", "3,382.40", "0.000",
	"HS", "ARCLK", "ARÇELİK A.Ş.", "19.000", "2,699.90", "0.000",
}

var devir = []string{"14/03/2024", "Devir 000000-000 Cari TRY Bakiye Hareketleri", "+0.00", "0.00"}

var ledger = append(append([]string{}, devir...),
	"14/03/2024", "12/03/2024 Ref = 100001 HA 52.850000x64.000000 ASELS", "-3,382.40", "-3,382.40",
	"14/03/2024", "12/03/2024 Ref = 100002 HA 142.100000x15.000000 ARCLK", "-2,131.50", "-5,513.90",
	"14/03/2024", "12/03/2024 Ref = 100003 HA 142.100000x4.000000 ARCLK", "-568.40", "-6,082.30",
	"14/03/2024", "Matrah 6082.30 Oran 0.00195 Komisyon", "-11.86", "-6,094.16",
	"14/03/2024", "000000-000 Nolu Hesabın Banka Takas İşlemi", "+6,101.31", "7.15",
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func buy(date, symbol, qty, price, amount, fee string) statement.Trade {
	return statement.Trade{Date: date, Symbol: symbol, Side: "buy", Quantity: d(qty), Price: d(price), Amount: d(amount), Fee: d(fee), Currency: "TRY"}
}

func sell(date, symbol, qty, price, amount, fee string) statement.Trade {
	t := buy(date, symbol, qty, price, amount, fee)
	t.Side = "sell"
	return t
}

// same compares two statements with decimals by value: %+v prints a decimal
// through its String, which drops trailing zeros.
func same(t *testing.T, got, want statement.Statement) {
	t.Helper()
	if (got.CashBalance == nil) != (want.CashBalance == nil) || got.CashBalance != nil && !got.CashBalance.Equal(*want.CashBalance) {
		t.Errorf("CashBalance = %v, want %v", got.CashBalance, want.CashBalance)
	}
	got.CashBalance, want.CashBalance = nil, nil
	if g, w := fmt.Sprintf("%+v", got), fmt.Sprintf("%+v", want); g != w {
		t.Errorf("statement\n got %s\nwant %s", g, w)
	}
}

func TestParse(t *testing.T) {
	balance := d("7.15")
	sample := statement.Statement{
		Broker: "garanti_yatirim", PeriodStart: "2024-03-01", PeriodEnd: "2024-03-31", Currency: "TRY", CashBalance: &balance,
		Trades: []statement.Trade{
			buy("2024-03-12", "ASELS", "64", "52.85", "3382.40", "6.60"),
			buy("2024-03-12", "ARCLK", "15", "142.10", "2131.50", "4.16"),
			buy("2024-03-12", "ARCLK", "4", "142.10", "568.40", "1.10"),
		},
		Cash: []statement.CashMovement{{Date: "2024-03-12", Kind: "deposit", Amount: d("6101.31"), Currency: "TRY", Description: "000000-000 Nolu Hesabın Banka Takas İşlemi"}},
		Holdings: []statement.Holding{
			{Symbol: "ASELS", Quantity: d("64"), Currency: "TRY", Price: d("52.85")},
			{Symbol: "ARCLK", Quantity: d("19"), Currency: "TRY", Price: d("142.1")},
		},
		Skipped: []statement.Skipped{{Section: "ledger", Reason: "opening balance", Row: devir}},
	}

	unknown := []string{"18/03/2024", "Bilinmeyen Hareket", "-1.00", "6.15"}
	withUnknown := sample
	withUnknown.Errors = []statement.ParseError{{Section: "ledger", Row: unknown, Code: "unknown_row", Value: "Bilinmeyen Hareket", Message: "unknown ledger row", Date: "2024-03-18"}}

	feeRow := []string{"19/03/2024", "Matrah 100.00 Oran 0.002 Komisyon", "-0.20", "6.95"}
	withFee := sample
	withFee.Cash = append(append([]statement.CashMovement{}, sample.Cash...),
		statement.CashMovement{Date: "2024-03-19", Kind: "fee", Amount: d("0.20"), Currency: "TRY", Description: feeRow[1]})

	fund := []string{"YF", "ABC", "ABC FONU", "10.000", "100.00", "0.000"}
	withFund := sample
	withFund.Skipped = append([]statement.Skipped{{Section: "holdings", Reason: "unsupported asset type", Row: fund}}, sample.Skipped...)

	// A February statement without the Hareket Listesi: the period comes from
	// the statement date, and a January trade date settling on 1 February
	// takes the value date.
	febBalance := d("49.00")
	feb := statement.Statement{
		Broker: "garanti_yatirim", PeriodStart: "2024-02-01", PeriodEnd: "2024-02-29", Currency: "TRY", CashBalance: &febBalance,
		Trades: []statement.Trade{buy("2024-02-01", "THYAO", "5", "10", "50.00", "0")},
	}
	febStatus := page("Hesap Durumu", "Tarih", ":", "29/02/2024", "Cari Bakiye", "Mevcudu", "Rezervi (T + 1)", "49.00", "0.00", "MEVCUT MENKUL KIYMETLER", "Hisse toplamı :", "0.000", "0.00", "0.000")
	febLedger := page("Hesap Ekstresi", "Tarih", "Açıklama", "İşlem", "Kalan",
		"01/02/2024", "30/01/2024 Ref = 1 HA 10.000000x5.000000 THYAO", "-50.00", "49.00")

	// A trade amount that disagrees with price x quantity is an error, not a
	// trade.
	badRow := []string{"01/02/2024", "30/01/2024 Ref = 1 HA 10.000000x5.000000 THYAO", "-500.00", "49.00"}
	badLedger := page(append([]string{"Hesap Ekstresi", "Tarih", "Açıklama", "İşlem", "Kalan"}, badRow...)...)
	bad := feb
	bad.Trades = nil
	bad.Errors = []statement.ParseError{{Section: "ledger", Row: badRow, Code: "bad_number", Value: "-500.00", Message: "amount is not price x quantity", Date: "2024-02-01"}}

	// A statement printed in Turkish number format; the description prices
	// stay US (GLG-725).
	trBalance := d("-6750.50")
	tr := statement.Statement{
		Broker: "garanti_yatirim", PeriodStart: "2023-11-01", PeriodEnd: "2023-11-30", Currency: "TRY", CashBalance: &trBalance,
		Trades: []statement.Trade{
			buy("2023-11-15", "ISCTR", "300", "20", "6000.00", "0"),
			buy("2023-11-20", "ARCLK", "5", "150.10", "750.50", "0"),
		},
		Holdings: []statement.Holding{{Symbol: "AEFES", Quantity: d("150"), Currency: "TRY", Price: d("120")}},
	}
	trStatus := page("Hesap Durumu", "Tarih", ":", "30/11/2023", "Cari Bakiye", "Mevcudu", "Rezervi (T + 1)", "-6.750,50", "0,00", "MEVCUT MENKUL KIYMETLER",
		"HS", "AEFES", "ANADOLU EFES BİRACILIK VE", "150,000", "18.000,00", "0,000",
		"Hisse toplamı :", "150,000", "18.000,00", "0,000")
	trLedger := page("Hesap Ekstresi", "Dönemi", ":", "01/11/2023 - 30/11/2023", "Tarih", "Açıklama", "İşlem", "Kalan",
		"17/11/2023", "15/11/2023 Ref = 1 HA 20.00x300.00 ISCTR", "-6.000,00", "-6.000,00",
		"22/11/2023", "20/11/2023 Ref = 2 HA 150.10x5.00 ARCLK", "-750,50", "-6.750,50")

	// Sells (HS) and the "Komisyon Tutarı ve BSMV" text of statements from
	// May 2024 on (GLG-728).
	marStatus := page("Hesap Durumu", "Tarih", ":", "31/03/2025", "Cari Bakiye", "Mevcudu", "Rezervi (T + 1)", "0.00", "0.00", "MEVCUT MENKUL KIYMETLER", "Hisse toplamı :", "0.000", "0.00", "0.000")
	marLedger := page("Hesap Ekstresi", "Dönemi", ":", "01/03/2025 - 31/03/2025", "Tarih", "Açıklama", "İşlem", "Kalan",
		"14/03/2025", "12/03/2025 Ref = 11 HS 30.00x200.00 YKBNK", "+6,000.00", "6,000.00",
		"14/03/2025", "12/03/2025 Ref = 12 HS 150.00x40.00 AEFES", "+6,000.00", "12,000.00",
		"14/03/2025", "Matrah 12000.00 Oran 0.00195 Komisyon Tutarı ve BSMV", "-24.57", "11,975.43",
		"14/03/2025", "1234-6000001 Nolu Hesabın Banka Takas İşlemi", "-11,975.43", "0.00",
		"19/03/2025", "17/03/2025 Ref = 13 HA 40.00x125.00 OPT25", "-5,000.00", "-5,000.00",
		"19/03/2025", "Matrah 5000.00 Oran 0.00195 Komisyon Tutarı ve BSMV", "-10.24", "-5,010.24",
		"19/03/2025", "1234-6000001 Nolu Hesabın Banka Takas İşlemi", "+5,010.24", "0.00")
	zero := d("0")
	mar := statement.Statement{
		Broker: "garanti_yatirim", PeriodStart: "2025-03-01", PeriodEnd: "2025-03-31", Currency: "TRY", CashBalance: &zero,
		Trades: []statement.Trade{
			sell("2025-03-12", "YKBNK", "200", "30", "6000.00", "12.29"),
			sell("2025-03-12", "AEFES", "40", "150", "6000.00", "12.28"),
			buy("2025-03-17", "OPT25", "125", "40", "5000.00", "10.24"),
		},
		Cash: []statement.CashMovement{
			{Date: "2025-03-14", Kind: "withdrawal", Amount: d("11975.43"), Currency: "TRY", Description: "1234-6000001 Nolu Hesabın Banka Takas İşlemi"},
			{Date: "2025-03-17", Kind: "deposit", Amount: d("5010.24"), Currency: "TRY", Description: "1234-6000001 Nolu Hesabın Banka Takas İşlemi"},
		},
	}

	// A Banka Takas deposit pays for the buys of its value date: it is dated
	// on their earliest trade date, so cash does not go negative in between;
	// a withdrawal keeps its value date, as in mar (GLG-818).
	julStatus := page("Hesap Durumu", "Tarih", ":", "31/07/2023", "Cari Bakiye", "Mevcudu", "Rezervi (T + 1)", "0.00", "0.00")
	julLedger := page("Hesap Ekstresi", "Dönemi", ":", "01/07/2023 - 31/07/2023", "Tarih", "Açıklama", "İşlem", "Kalan",
		"05/07/2023", "03/07/2023 Ref = 21 HA 10.00x100.00 THYAO", "-1,000.00", "-1,000.00",
		"05/07/2023", "03/07/2023 Ref = 22 HA 20.00x50.00 ASELS", "-1,000.00", "-2,000.00",
		"05/07/2023", "1234-6000001 Nolu Hesabın Banka Takas İşlemi", "+2,000.00", "0.00")
	jul := statement.Statement{
		Broker: "garanti_yatirim", PeriodStart: "2023-07-01", PeriodEnd: "2023-07-31", Currency: "TRY", CashBalance: &zero,
		Trades: []statement.Trade{
			buy("2023-07-03", "THYAO", "100", "10", "1000.00", "0"),
			buy("2023-07-03", "ASELS", "50", "20", "1000.00", "0"),
		},
		Cash: []statement.CashMovement{{Date: "2023-07-03", Kind: "deposit", Amount: d("2000.00"), Currency: "TRY", Description: "1234-6000001 Nolu Hesabın Banka Takas İşlemi"}},
	}

	// A value date with a buy and a sell has one commission per side; each
	// goes to the side whose amounts sum to its Matrah.
	mixedLedger := page("Hesap Ekstresi", "Dönemi", ":", "01/03/2025 - 31/03/2025", "Tarih", "Açıklama", "İşlem", "Kalan",
		"21/03/2025", "19/03/2025 Ref = 14 HA 10.00x100.00 THYAO", "-1,000.00", "-1,000.00",
		"21/03/2025", "19/03/2025 Ref = 15 HS 20.00x150.00 ASELS", "+3,000.00", "2,000.00",
		"21/03/2025", "Matrah 1000.00 Oran 0.002 Komisyon Tutarı ve BSMV", "-2.00", "1,998.00",
		"21/03/2025", "Matrah 3000.00 Oran 0.00195 Komisyon Tutarı ve BSMV", "-6.14", "1,991.86")
	mixed := statement.Statement{
		Broker: "garanti_yatirim", PeriodStart: "2025-03-01", PeriodEnd: "2025-03-31", Currency: "TRY", CashBalance: &zero,
		Trades: []statement.Trade{
			buy("2025-03-19", "THYAO", "100", "10", "1000.00", "2.00"),
			sell("2025-03-19", "ASELS", "150", "20", "3000.00", "6.14"),
		},
	}

	// Buys and sells of equal totals: each side gets one of the two
	// commissions, not the buys both (GLG-747).
	evenLedger := page("Hesap Ekstresi", "Dönemi", ":", "01/03/2025 - 31/03/2025", "Tarih", "Açıklama", "İşlem", "Kalan",
		"21/03/2025", "19/03/2025 Ref = 16 HA 10.00x100.00 AAA", "-1,000.00", "-1,000.00",
		"21/03/2025", "19/03/2025 Ref = 17 HS 20.00x50.00 BBB", "+1,000.00", "0.00",
		"21/03/2025", "Matrah 1000.00 Oran 0.00195 Komisyon Tutarı ve BSMV", "-2.05", "-2.05",
		"21/03/2025", "Matrah 1000.00 Oran 0.00195 Komisyon Tutarı ve BSMV", "-2.05", "-4.10")
	even := statement.Statement{
		Broker: "garanti_yatirim", PeriodStart: "2025-03-01", PeriodEnd: "2025-03-31", Currency: "TRY", CashBalance: &zero,
		Trades: []statement.Trade{
			buy("2025-03-19", "AAA", "100", "10", "1000.00", "2.05"),
			sell("2025-03-19", "BBB", "50", "20", "1000.00", "2.05"),
		},
	}

	// Near-equal totals: the exact Matrah wins over one a kuruş off, so the
	// fees do not swap even when the sell's commission comes first (GLG-753).
	nearLedger := page("Hesap Ekstresi", "Dönemi", ":", "01/03/2025 - 31/03/2025", "Tarih", "Açıklama", "İşlem", "Kalan",
		"21/03/2025", "19/03/2025 Ref = 18 HA 10.00x100.00 ASELS", "-1,000.00", "-1,000.00",
		"21/03/2025", "19/03/2025 Ref = 19 HS 1000.01x1.00 THYAO", "+1,000.01", "0.01",
		"21/03/2025", "Matrah 1000.01 Oran 0.003 Komisyon Tutarı ve BSMV", "-3.00", "-2.99",
		"21/03/2025", "Matrah 1000.00 Oran 0.00195 Komisyon Tutarı ve BSMV", "-2.05", "-5.04")
	near := statement.Statement{
		Broker: "garanti_yatirim", PeriodStart: "2025-03-01", PeriodEnd: "2025-03-31", Currency: "TRY", CashBalance: &zero,
		Trades: []statement.Trade{
			buy("2025-03-19", "ASELS", "100", "10", "1000.00", "2.05"),
			sell("2025-03-19", "THYAO", "1", "1000.01", "1000.01", "3.00"),
		},
	}

	// April 2024 prints the holdings without the name cell (GLG-726).
	aprStatus := page("Hesap Durumu", "Tarih", ":", "30/04/2024", "Cari Bakiye", "Mevcudu", "Rezervi (T + 1)", "0.00", "0.00", "MEVCUT MENKUL KIYMETLER",
		"HS", "AEFES", "100.000", "15,000.00", "0.000", "HS", "AKBNK", "200.000", "12,000.00", "0.000",
		"Hisse toplamı :", "300.000", "27,000.00", "0.000")
	apr := statement.Statement{
		Broker: "garanti_yatirim", PeriodStart: "2024-04-01", PeriodEnd: "2024-04-30", Currency: "TRY", CashBalance: &zero,
		Holdings: []statement.Holding{
			{Symbol: "AEFES", Quantity: d("100"), Currency: "TRY", Price: d("150")},
			{Symbol: "AKBNK", Quantity: d("200"), Currency: "TRY", Price: d("60")},
		},
	}

	// MKK, account-keeping and order-cancel charges and the transfers that
	// pay them; one description wraps into a second cell, and the second
	// ledger page repeats the header (GLG-729).
	feeLedger := page("Hesap Ekstresi", "Dönemi", ":", "01/03/2025 - 31/03/2025", "Tarih", "Açıklama", "İşlem", "Kalan",
		"19/03/2025", "1234-6000001 Nolu Hesabın Banka MKK Masraf Transfer İşlemi", "+5.00", "5.00",
		"19/03/2025", "ŞUBAT 2025  MKK'ya Ödenen Ücret- MKK Hesap Bakım", "-3.00", "2.00",
		"26/03/2025", "ŞUBAT 2025  MKK'ya Ödenen Ücret- MKK Açık Hisse Saklama", "-2.00", "0.00",
		"27/03/2025", "ARALIK 2024  MKK'ya Ödenen Ücret- MKK HS VR BYF ALIM SATIM", "ÜCRETİ", "-0.50", "-0.50",
		"28/03/2025", "Borsa Emir İptal Ücreti ve Vergi-7", "+0.50", "0.00")
	feeLedger2 := page(
		"09/05/2025", "2025 1. Dönem-Hesap bakım ücreti tahsilatı", "-100.00", "-100.00",
		"15/05/2024", "2024  2. Dönem-Hesap bakım ücreti tahsilatı", "-100.00", "-200.00",
		"09/12/2025", "Diğer Bakiye Transferi-1234-6000001", "-50.00", "-250.00")
	fees := statement.Statement{
		Broker: "garanti_yatirim", PeriodStart: "2025-03-01", PeriodEnd: "2025-03-31", Currency: "TRY", CashBalance: &zero,
		Cash: []statement.CashMovement{
			{Date: "2025-03-19", Kind: "deposit", Amount: d("5.00"), Currency: "TRY", Description: "1234-6000001 Nolu Hesabın Banka MKK Masraf Transfer İşlemi"},
			{Date: "2025-03-19", Kind: "fee", Amount: d("3.00"), Currency: "TRY", Description: "ŞUBAT 2025  MKK'ya Ödenen Ücret- MKK Hesap Bakım"},
			{Date: "2025-03-26", Kind: "fee", Amount: d("2.00"), Currency: "TRY", Description: "ŞUBAT 2025  MKK'ya Ödenen Ücret- MKK Açık Hisse Saklama"},
			{Date: "2025-03-27", Kind: "fee", Amount: d("0.50"), Currency: "TRY", Description: "ARALIK 2024  MKK'ya Ödenen Ücret- MKK HS VR BYF ALIM SATIM ÜCRETİ"},
			{Date: "2025-03-28", Kind: "deposit", Amount: d("0.50"), Currency: "TRY", Description: "Borsa Emir İptal Ücreti ve Vergi-7"},
			{Date: "2025-05-09", Kind: "fee", Amount: d("100.00"), Currency: "TRY", Description: "2025 1. Dönem-Hesap bakım ücreti tahsilatı"},
			{Date: "2024-05-15", Kind: "fee", Amount: d("100.00"), Currency: "TRY", Description: "2024  2. Dönem-Hesap bakım ücreti tahsilatı"},
			{Date: "2025-12-09", Kind: "withdrawal", Amount: d("50.00"), Currency: "TRY", Description: "Diğer Bakiye Transferi-1234-6000001"},
		},
	}

	// 2018 statements print a letter before the trade reference, and move a
	// dividend to the bank with "Temettü Transferi işlemi" (GLG-763).
	jun2018Ledger := page("Hesap Ekstresi", "Dönemi", ":", "01/06/2018 - 30/06/2018", "Tarih", "Açıklama", "İşlem", "Kalan",
		"14/06/2018", "12/06/2018 Ref = A12345 HA 5.50x10 AKBNK", "-55.00", "-55.00",
		"14/06/2018", "12/06/2018 Ref = A12346 HS 6.00x10 AKBNK", "+60.00", "5.00",
		"14/06/2018", "Matrah 55.00 Oran 0.00188 Komisyon", "-0.10", "4.90",
		"20/06/2018", "GARAN.E Temettü 10 Adet Temettü Oranı : 2.5", "+0.25", "5.15",
		"20/06/2018", "Temettü Transferi işlemi - 1234567-100 No'lu Hesap", "-0.25", "4.90")
	jun2018 := statement.Statement{
		Broker: "garanti_yatirim", PeriodStart: "2018-06-01", PeriodEnd: "2018-06-30", Currency: "TRY", CashBalance: &zero,
		Trades: []statement.Trade{
			buy("2018-06-12", "AKBNK", "10", "5.50", "55.00", "0.10"),
			sell("2018-06-12", "AKBNK", "10", "6.00", "60.00", "0"),
		},
		Cash:      []statement.CashMovement{{Date: "2018-06-20", Kind: "withdrawal", Amount: d("0.25"), Currency: "TRY", Description: "Temettü Transferi işlemi - 1234567-100 No'lu Hesap"}},
		Dividends: []statement.Dividend{{Date: "2018-06-20", Symbol: "GARAN", Gross: d("0.25"), Tax: d("0"), Currency: "TRY"}},
	}

	// A ledger date whose cells complete no record is a bad_row that quotes
	// only the date, never the cells after it.
	brokenLedger := page("Hesap Ekstresi", "Dönemi", ":", "01/03/2025 - 31/03/2025", "Tarih", "Açıklama", "İşlem", "Kalan",
		"20/03/2025", "AD SOYAD", "ADRES 1",
		"21/03/2025", "Borsa Emir İptal BSMV", "-0.10", "-0.10")
	broken := statement.Statement{
		Broker: "garanti_yatirim", PeriodStart: "2025-03-01", PeriodEnd: "2025-03-31", Currency: "TRY", CashBalance: &zero,
		Cash:   []statement.CashMovement{{Date: "2025-03-21", Kind: "fee", Amount: d("0.10"), Currency: "TRY", Description: "Borsa Emir İptal BSMV"}},
		Errors: []statement.ParseError{{Section: "ledger", Row: []string{"20/03/2025"}, Code: "bad_row", Value: "20/03/2025", Message: "incomplete ledger row", Date: "2025-03-20"}},
	}

	// A cash dividend is paid and moved on to the bank account in two rows;
	// an ETF prints .F and its quantity may be fractional (GLG-730).
	divLedger := page("Hesap Ekstresi", "Dönemi", ":", "01/03/2025 - 31/03/2025", "Tarih", "Açıklama", "İşlem", "Kalan",
		"20/03/2025", "AEFES.E Temettü 100 Adet Temettü Oranı : 95.9090909", "+95.91", "95.91",
		"20/03/2025", "AEFES.E Temettü 100 Adet Temettü Oranı : 95.9090909-1234567-102", "-95.91", "0.00",
		"24/03/2025", "OPT25.F Temettü 1234.567 Adet Temettü Oranı : 10.5", "+129.63", "129.63",
		"24/03/2025", "OPT25.F Temettü 1234.567 Adet Temettü Oranı : 10.5-7654321-3", "-129.63", "0.00")
	divs := statement.Statement{
		Broker: "garanti_yatirim", PeriodStart: "2025-03-01", PeriodEnd: "2025-03-31", Currency: "TRY", CashBalance: &zero,
		Cash: []statement.CashMovement{
			{Date: "2025-03-20", Kind: "withdrawal", Amount: d("95.91"), Currency: "TRY", Description: "AEFES.E Temettü 100 Adet Temettü Oranı : 95.9090909-1234567-102"},
			{Date: "2025-03-24", Kind: "withdrawal", Amount: d("129.63"), Currency: "TRY", Description: "OPT25.F Temettü 1234.567 Adet Temettü Oranı : 10.5-7654321-3"},
		},
		Dividends: []statement.Dividend{
			{Date: "2025-03-20", Symbol: "AEFES", Gross: d("95.91"), Tax: d("0"), Currency: "TRY"},
			{Date: "2025-03-24", Symbol: "OPT25", Gross: d("129.63"), Tax: d("0"), Currency: "TRY"},
		},
	}

	// The Hareket Listesi books a bonus issue as a buy at price 0 and a
	// priced transfer in at its price; HS Alış and HS Satış are the ledger's
	// trades. The page header repeats on the second page (GLG-739).
	augStatus := page("Hesap Durumu", "Tarih", ":", "31/08/2023", "Cari Bakiye", "Mevcudu", "Rezervi (T + 1)", "0.00", "0.00", "MEVCUT MENKUL KIYMETLER", "Hisse toplamı :", "0.000", "0.00", "0.000")
	moves := func(records ...string) []string {
		return page(append([]string{
			"Menkul Kıymet Hareket Listesi", "Emanet No", ":", "000000-000", "Dönemi", ":", "01/08/2023 - 31/08/2023", "Sayfa Numarası", ":", "2",
			"Kodu /", "Vadesi", "Adı /", "Tanımı", "Tarihi", "Nominal/Adet", "İşlem", "Kodu", "Fiyatı", "Hatalar ve unutmalar istisnadır.", "Sayfa 2 / 3",
		}, records...)...)
	}
	movesA := moves(
		"ARCLK", "ARÇELİK A.Ş.", "10/08/2023", "10.000", "HS Alış", "150.000",
		"ASELS", "ASELSAN ELEKTRONİK SANAYİ", "21/08/2023", "100.000", "Bedelsiz", "0.000",
		"ISCTR", "TÜRKİYE İŞ BANKASI A.Ş.", "22/08/2023", "1,500.750", "Bedelsiz", "0.000")
	movesB := moves(
		"YKBNK", "YAPI VE KREDİ BANKASI A.Ş", "23/08/2023", "-50.000", "HS Satış", "30.000",
		"RGYAS", "24/08/2023", "20.000", "HS-Virman Giriş", "100.000")
	noName := moves(
		"ARCLK", "10/08/2023", "10.000", "HS Alış", "150.000",
		"ASELS", "21/08/2023", "100.000", "Bedelsiz", "0.000",
		"ISCTR", "22/08/2023", "1,500.750", "Bedelsiz", "0.000",
		"YKBNK", "23/08/2023", "-50.000", "HS Satış", "30.000",
		"RGYAS", "24/08/2023", "20.000", "HS-Virman Giriş", "100.000")
	aug := statement.Statement{
		Broker: "garanti_yatirim", PeriodStart: "2023-08-01", PeriodEnd: "2023-08-31", Currency: "TRY", CashBalance: &zero,
		Trades: []statement.Trade{
			buy("2023-08-21", "ASELS", "100", "0", "0", "0"),
			buy("2023-08-22", "ISCTR", "1500.75", "0", "0", "0"),
			buy("2023-08-24", "RGYAS", "20", "100", "2000", "0"),
		},
	}
	// An unknown type and a transfer in at price 0 (cost unknown) are left
	// to the user; the error quotes the record, never the page header.
	rights := []string{"AKBNK", "AKBANK T.A.Ş.", "25/08/2023", "10.000", "Rüçhan", "1.000"}
	freeIn := []string{"THYAO", "26/08/2023", "5.000", "HS-Virman Giriş", "0.000"}
	unknownMove := statement.Statement{
		Broker: "garanti_yatirim", PeriodStart: "2023-08-01", PeriodEnd: "2023-08-31", Currency: "TRY", CashBalance: &zero,
		Errors: []statement.ParseError{
			{Section: "trades", Row: rights, Code: "unknown_row", Value: "Rüçhan", Message: "unknown trade list row", Date: "2023-08-25"},
			{Section: "trades", Row: freeIn, Code: "unknown_cost", Value: "HS-Virman Giriş", Message: "transfer in with unknown cost", Date: "2023-08-26"},
		},
	}

	// An IPO's money comes in for the full request and is blocked, the part
	// not allotted comes back and returns to the bank; a fractional share is
	// cashed out as a sell of the fraction (GLG-740).
	ipoLedger := page("Hesap Ekstresi", "Dönemi", ":", "01/04/2024 - 30/04/2024", "Tarih", "Açıklama", "İşlem", "Kalan",
		"10/04/2024", "123456 100 adet RGYAS Halka Arz Tahsil", "+10,000.00", "10,000.00",
		"10/04/2024", "123456 100 adet RGYAS Halka Arz Tahsil", "-10,000.00", "0.00",
		"15/04/2024", "MUSTERI ADI 123456 RGYAS için iade", "+8,000.00", "8,000.00",
		"15/04/2024", "MUSTERI ADI 123456 RGYAS için iade", "-8,000.00", "0.00",
		"10/12/2025", "0.750 adet PGSUS bedeli kurum küsurat hesabından virman", "+150.00", "150.00")
	ipo := statement.Statement{
		Broker: "garanti_yatirim", PeriodStart: "2024-04-01", PeriodEnd: "2024-04-30", Currency: "TRY", CashBalance: &zero,
		Trades: []statement.Trade{sell("2025-12-10", "PGSUS", "0.75", "200", "150.00", "0")},
		Cash: []statement.CashMovement{
			{Date: "2024-04-10", Kind: "deposit", Amount: d("10000.00"), Currency: "TRY", Description: "123456 100 adet RGYAS Halka Arz Tahsil"},
			{Date: "2024-04-15", Kind: "withdrawal", Amount: d("8000.00"), Currency: "TRY", Description: "IPO refund RGYAS"},
		},
		Skipped: []statement.Skipped{
			{Section: "ledger", Reason: "IPO payment, booked by the allotment", Row: []string{"10/04/2024", "123456 100 adet RGYAS Halka Arz Tahsil", "-10,000.00", "0.00"}},
			{Section: "ledger", Reason: "IPO refund", Row: []string{"15/04/2024", "+8,000.00"}},
		},
	}

	// A fractional share cashed out is also a trade list transfer out at
	// price 0: the ledger's sell books it, the transfer out is skipped. Without
	// the ledger row it is a transfer to another institution, left to the user
	// (GLG-767).
	cashOutMove := []string{"THYAO", "TÜRK HAVA YOLLARI A.O.", "11/03/2025", "-0.500", "HS-Virman Çıkış", "0.000000"}
	cashOutNoName := []string{"THYAO", "11/03/2025", "-0.500", "HS-Virman Çıkış", "0.000000"}
	marMoves := func(rec []string) []string {
		return page(append([]string{"Menkul Kıymet Hareket Listesi", "Dönemi", ":", "01/03/2025 - 31/03/2025"}, rec...)...)
	}
	cashOutLedger := page("Hesap Ekstresi", "Tarih", "Açıklama", "İşlem", "Kalan",
		"11/03/2025", "0.500 adet THYAO bedeli kurum küsurat hesabından virman", "+120.00", "120.00")
	cashOut := func(rec []string) statement.Statement {
		return statement.Statement{
			Broker: "garanti_yatirim", PeriodStart: "2025-03-01", PeriodEnd: "2025-03-31", Currency: "TRY", CashBalance: &zero,
			Trades:  []statement.Trade{sell("2025-03-11", "THYAO", "0.5", "240", "120.00", "0")},
			Skipped: []statement.Skipped{{Section: "trades", Reason: "fractional share cash-out, booked from the ledger", Row: rec}},
		}
	}
	transferOut := func(rec []string) statement.Statement {
		return statement.Statement{
			Broker: "garanti_yatirim", PeriodStart: "2025-03-01", PeriodEnd: "2025-03-31", Currency: "TRY", CashBalance: &zero,
			Errors: []statement.ParseError{{Section: "trades", Row: rec, Code: "unknown_row", Value: "HS-Virman Çıkış", Message: "unknown trade list row", Date: "2025-03-11"}},
		}
	}

	// A statement from before the 2019 rebrand prints the old website cell;
	// a 2019 one has a Hesap Durumu page only, so the period comes from the
	// statement date (GLG-727).
	may2019Status := page("Hesap Durumu", "Tarih", ":", "31/05/2019", "Cari Bakiye", "Mevcudu", "Rezervi (T + 1)", "0.00", "0.00", "MEVCUT MENKUL KIYMETLER",
		"HS", "AKBNK", "AKBANK T.A.Ş.", "100.000", "700.00", "0.000",
		"Hisse toplamı :", "100.000", "700.00", "0.000")
	may2019Status[2] = oldWebsite
	may2019 := statement.Statement{
		Broker: "garanti_yatirim", PeriodStart: "2019-05-01", PeriodEnd: "2019-05-31", Currency: "TRY", CashBalance: &zero,
		Holdings: []statement.Holding{{Symbol: "AKBNK", Quantity: d("100"), Currency: "TRY", Price: d("7")}},
	}

	tests := []struct {
		name string
		rows [][]string
		want statement.Statement
	}{
		{"sample", [][]string{page1(shares...), page2(), page3(ledger...)}, sample},
		{"unknown ledger row", [][]string{page1(shares...), page2(), page3(append(append([]string{}, ledger...), unknown...)...)}, withUnknown},
		{"commission without buy", [][]string{page1(shares...), page2(), page3(append(append([]string{}, ledger...), feeRow...)...)}, withFee},
		{"non-share holding", [][]string{page1(append(append([]string{}, fund...), shares...)...), page2(), page3(ledger...)}, withFund},
		{"trade date before period", [][]string{febStatus, febLedger}, feb},
		{"amount not price x quantity", [][]string{febStatus, badLedger}, bad},
		{"turkish number format", [][]string{trStatus, trLedger}, tr},
		{"sells and new commission text", [][]string{marStatus, marLedger}, mar},
		{"takas deposit on its buys' trade date", [][]string{julStatus, julLedger}, jul},
		{"buy and sell commissions on one date", [][]string{marStatus, mixedLedger}, mixed},
		{"buy and sell of equal totals on one date", [][]string{marStatus, evenLedger}, even},
		{"buy and sell a kuruş apart on one date", [][]string{marStatus, nearLedger}, near},
		{"holdings without name", [][]string{aprStatus}, apr},
		{"fee and transfer rows", [][]string{marStatus, feeLedger, feeLedger2}, fees},
		{"2018 trade reference and dividend transfer", [][]string{marStatus, jun2018Ledger}, jun2018},
		{"incomplete ledger row", [][]string{marStatus, brokenLedger}, broken},
		{"dividend and its transfer", [][]string{marStatus, divLedger}, divs},
		{"bonus shares and transfer in", [][]string{augStatus, movesA, movesB}, aug},
		{"trade list without name", [][]string{augStatus, noName}, aug},
		{"IPO and fractional cash-out", [][]string{marStatus, ipoLedger}, ipo},
		{"unknown trade list type", [][]string{augStatus, moves(append(append([]string{}, rights...), freeIn...)...)}, unknownMove},
		{"fractional cash-out transfer out", [][]string{marStatus, marMoves(cashOutMove), cashOutLedger}, cashOut(cashOutMove)},
		{"fractional cash-out transfer out without name", [][]string{marStatus, marMoves(cashOutNoName), cashOutLedger}, cashOut(cashOutNoName)},
		{"transfer out without cash-out", [][]string{marStatus, marMoves(cashOutMove)}, transferOut(cashOutMove)},
		{"transfer out without cash-out or name", [][]string{marStatus, marMoves(cashOutNoName)}, transferOut(cashOutNoName)},
		{"status page only", [][]string{may2019Status}, may2019},
		{"IPO allotment priced from the ledger", ipoRows(ipoMoves("20.000"), ipoRefund("4,000.00")...), partialIPO},
		{"IPO fully allotted", ipoRows(ipoMoves("100.000", "ABCDE", "16/09/2024", "10.000", "HS-Virman Giriş", "25.000000")), fullIPO},
		{"IPO allotment not matching its refund", ipoRows(ipoMoves("20.000"), ipoRefund("3,000.00")...), badIPO},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := garantiyatirim.Parse(tc.rows)
			if err != nil {
				t.Fatal(err)
			}
			same(t, got, tc.want)
		})
		// Every fixture parses the same with the pre-rebrand website cell,
		// including the page header date on a second ledger page (GLG-727).
		t.Run(tc.name+", old website", func(t *testing.T) {
			var rows [][]string
			for _, row := range tc.rows {
				row = append([]string{}, row...)
				for i, c := range row {
					if c == "www.garantibbvayatirim.com.tr" {
						row[i] = oldWebsite
					}
				}
				rows = append(rows, row)
			}
			got, err := garantiyatirim.Parse(rows)
			if err != nil {
				t.Fatal(err)
			}
			same(t, got, tc.want)
		})
	}
}

const oldWebsite = "www.garantiyatirim.com.tr"

// An IPO request of 100 XYZAS at 50.00: the trade list prints the allotment
// at the price of the row above it, 80.00, not the IPO price (GLG-768).
func ipoMoves(allotted string, more ...string) []string {
	return page(append([]string{
		"Menkul Kıymet Hareket Listesi", "Dönemi", ":", "01/09/2024 - 30/09/2024",
		"SAHOL", "20/09/2024", "5.000", "HS Alış", "80.000000",
		"XYZAS", "16/09/2024", allotted, "HS-Virman Giriş", "80.000000",
	}, more...)...)
}

var (
	ipoStatus = page("Hesap Durumu", "Tarih", ":", "30/09/2024", "Cari Bakiye", "Mevcudu", "Rezervi (T + 1)", "0.00", "0.00")
	ipoIn     = []string{"10/09/2024", "12345 100 adet XYZAS Halka Arz Tahsil", "+5,000.00", "5,000.00"}
	ipoPaid   = []string{"10/09/2024", "12345 100 adet XYZAS Halka Arz Tahsil", "-5,000.00", "0.00"}
	ipoMove   = []string{"XYZAS", "16/09/2024", "20.000", "HS-Virman Giriş", "80.000000"}
	ipoZero   = d("0")
	ipoCash   = statement.CashMovement{Date: "2024-09-10", Kind: "deposit", Amount: d("5000.00"), Currency: "TRY", Description: ipoIn[1]}
	ipoSkip   = statement.Skipped{Section: "ledger", Reason: "IPO payment, booked by the allotment", Row: ipoPaid}
)

// ipoRefund is the part of the request not allotted coming back and
// returning to the bank; it starts with the customer's name.
func ipoRefund(amount string) []string {
	return []string{
		"12/09/2024", "MUSTERI ADI 12345 XYZAS için iade", "+" + amount, amount,
		"12/09/2024", "MUSTERI ADI 12345 XYZAS için iade", "-" + amount, "0.00",
	}
}

func ipoRows(moves []string, refund ...string) [][]string {
	ledger := append([]string{"Hesap Ekstresi"}, ipoIn...)
	ledger = append(ledger, ipoPaid...)
	return [][]string{ipoStatus, moves, page(append(ledger, refund...)...)}
}

// partialIPO: 20 of 100 allotted at 50.00, 4,000.00 refunded.
var partialIPO = statement.Statement{
	Broker: "garanti_yatirim", PeriodStart: "2024-09-01", PeriodEnd: "2024-09-30", Currency: "TRY", CashBalance: &ipoZero,
	Trades: []statement.Trade{buy("2024-09-16", "XYZAS", "20", "50", "1000.00", "0")},
	Cash: []statement.CashMovement{ipoCash,
		{Date: "2024-09-12", Kind: "withdrawal", Amount: d("4000.00"), Currency: "TRY", Description: "IPO refund XYZAS"}},
	Skipped: []statement.Skipped{ipoSkip, {Section: "ledger", Reason: "IPO refund", Row: []string{"12/09/2024", "+4,000.00"}}},
}

// fullIPO: all 100 allotted, no refund; a transfer in of a symbol without an
// IPO row keeps its printed price.
var fullIPO = statement.Statement{
	Broker: "garanti_yatirim", PeriodStart: "2024-09-01", PeriodEnd: "2024-09-30", Currency: "TRY", CashBalance: &ipoZero,
	Trades: []statement.Trade{
		buy("2024-09-16", "XYZAS", "100", "50", "5000.00", "0"),
		buy("2024-09-16", "ABCDE", "10", "25", "250", "0"),
	},
	Cash:    []statement.CashMovement{ipoCash},
	Skipped: []statement.Skipped{ipoSkip},
}

// badIPO: 20 allotted at 50.00 is 1,000.00, but 5,000.00 less a 3,000.00
// refund is 2,000.00; the error quotes the trade list record only.
var badIPO = statement.Statement{
	Broker: "garanti_yatirim", PeriodStart: "2024-09-01", PeriodEnd: "2024-09-30", Currency: "TRY", CashBalance: &ipoZero,
	Cash: []statement.CashMovement{ipoCash,
		{Date: "2024-09-12", Kind: "withdrawal", Amount: d("3000.00"), Currency: "TRY", Description: "IPO refund XYZAS"}},
	Skipped: []statement.Skipped{ipoSkip, {Section: "ledger", Reason: "IPO refund", Row: []string{"12/09/2024", "+3,000.00"}}},
	Errors:  []statement.ParseError{{Section: "trades", Row: ipoMove, Code: "bad_number", Value: "20.000", Message: "IPO allotment does not match the ledger's payment and refund", Date: "2024-09-16"}},
}

// TestParseIPONetCash checks that a partly allotted IPO's buy, cash rows and
// skipped rows net to the statement's 0.00 balance (GLG-768).
func TestParseIPONetCash(t *testing.T) {
	got, err := garantiyatirim.Parse(ipoRows(ipoMoves("20.000"), ipoRefund("4,000.00")...))
	if err != nil {
		t.Fatal(err)
	}
	net := decimal.Zero
	for _, tr := range got.Trades {
		net = net.Sub(tr.Amount)
	}
	for _, c := range got.Cash {
		if c.Kind == "deposit" {
			net = net.Add(c.Amount)
		} else {
			net = net.Sub(c.Amount)
		}
	}
	if !net.IsZero() || len(got.Errors) != 0 {
		t.Fatalf("net cash %s with %d errors, want 0 with none", net, len(got.Errors))
	}
}

// TestParseRefundHidesName checks that the customer's name at the start of
// an IPO refund description reaches no output, also when the row is an error.
func TestParseRefundHidesName(t *testing.T) {
	status := page("Hesap Durumu", "Tarih", ":", "30/04/2024", "Cari Bakiye", "Mevcudu", "Rezervi (T + 1)", "0.00", "0.00")
	ledger := page("Hesap Ekstresi", "Tarih", "Açıklama", "İşlem", "Kalan",
		"15/04/2024", "MUSTERI ADI 123456 RGYAS için iade", "+8,000.00", "8,000.00",
		"15/04/2024", "MUSTERI ADI 123456 RGYAS için iade", "-8,000.00", "0.00",
		"16/04/2024", "MUSTERI ADI 123456 RGYAS için iade", "-1234567890123456.00", "0.00",
		"17/04/2024", "MUSTERI ADI 123456 RGYAS için iade", "+0.00", "0.00")
	got, err := garantiyatirim.Parse([][]string{status, ledger})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Cash) != 1 || len(got.Skipped) != 1 || len(got.Errors) != 2 {
		t.Fatalf("got %d cash, %d skipped, %d errors, want 1, 1, 2", len(got.Cash), len(got.Skipped), len(got.Errors))
	}
	var out []string
	for _, c := range got.Cash {
		out = append(out, c.Description)
	}
	for _, s := range got.Skipped {
		out = append(out, s.Row...)
	}
	for _, e := range got.Errors {
		out = append(append(out, e.Row...), e.Value)
	}
	for _, s := range out {
		if strings.Contains(s, "MUSTERI") || strings.Contains(s, "ADI") {
			t.Errorf("output %q quotes the customer's name", s)
		}
	}
}

// TestDetect covers GLG-764: with every PDF parser registered, a Garanti
// BBVA Yatırım statement is detected as garanti_yatirim and parses as an
// explicit pick does.
func TestDetect(t *testing.T) {
	rows := [][]string{page1(shares...), page2(), page3(ledger...)}
	key, got, err := statement.Detect(rows)
	if err != nil || key != "garanti_yatirim" {
		t.Fatalf("Detect = %q, %v; want garanti_yatirim", key, err)
	}
	want, _ := garantiyatirim.Parse(rows)
	if g, w := fmt.Sprintf("%+v", got), fmt.Sprintf("%+v", want); g != w {
		t.Errorf("statement\n got %s\nwant %s", g, w)
	}
}

func TestParseNotGarantiYatirim(t *testing.T) {
	rows := [][]string{page2(), page3(ledger...)}
	if _, err := garantiyatirim.Parse(rows); err == nil || err.Error() != "not a garanti_yatirim statement" {
		t.Fatalf("err = %v, want not a garanti_yatirim statement", err)
	}
}

// A crafted statement with many trades and Komisyon rows must parse in time
// linear in its size: each Komisyon scanned every trade, and a day with many
// Komisyon rows re-split its trades for each, so an upload cost seconds of
// CPU (GLG-1271). Half the trades sit one per value date with its Komisyon,
// half on one date with as many Komisyon rows.
func TestParseManyCommissions(t *testing.T) {
	const n = 5000
	cells := []string{"Hesap Ekstresi", "Dönemi", ":", "01/01/1950 - 31/12/2099", "Tarih", "Açıklama", "İşlem", "Kalan"}
	row := func(day time.Time, i int) {
		date := day.Format("02/01/2006")
		cells = append(cells,
			date, fmt.Sprintf("%s Ref = %d HA 10.00x1.00 AAA", date, i), "-10.00", "0.00",
			date, "Matrah 10.00 Oran 0.002 Komisyon", "-0.02", "0.00")
	}
	day := time.Date(1950, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range n {
		row(day.AddDate(0, 0, i), i)
		row(day, n+i)
	}
	start := time.Now()
	got, err := garantiyatirim.Parse([][]string{page("Hesap Durumu", "Tarih", ":", "31/12/2099", "Cari Bakiye", "Mevcudu", "Rezervi (T + 1)", "0.00", "0.00"), page(cells...)})
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Parse took %v", elapsed)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Trades) != 2*n || len(got.Errors) != 0 {
		t.Fatalf("got %d trades, %d errors, want %d, 0", len(got.Trades), len(got.Errors), 2*n)
	}
	fees := decimal.Zero
	for _, tr := range got.Trades {
		fees = fees.Add(tr.Fee)
	}
	if want := d("0.02").Mul(decimal.NewFromInt(2 * n)); !fees.Equal(want) {
		t.Errorf("fees sum to %s, want %s", fees, want)
	}
}
