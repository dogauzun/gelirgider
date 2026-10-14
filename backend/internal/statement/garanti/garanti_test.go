package garanti_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/statement"
	"github.com/dogauzun/gelirgider/backend/internal/statement/garanti"
	_ "github.com/dogauzun/gelirgider/backend/internal/statement/garantiyatirim"
	_ "github.com/dogauzun/gelirgider/backend/internal/statement/midas"
	"github.com/shopspring/decimal"
)

// customer are the cells above the first section title: none may ever reach
// a ParseError.
var customer = []string{
	"<bank address>", "<bank registry>",
	"EYLÜL 2025 YATIRIM EKSTRESİ", "Müşteri Adı Soyadı / Unvanı", ":", "AD SOYAD", "TCKN/YKN/VKN", ":", "00000000000", "Müşteri No", ":", "00000000", "Adres", ":", "ADRES",
}

func legal(page int) []string {
	return []string{"Ekstrenizde yalnızca SGMK ve Yatırım Fonu alım-satımında kullanılan hesapların bakiyeleri gösterilmektedir.", "<legal text 1>", "<legal text 2>", fmt.Sprintf("Sayfa %d / 2", page), "<legal text 3>"}
}

// page1 is the first page with the given movement records.
func page1(moves ...string) []string {
	cells := append(append([]string{}, customer...), legal(1)...)
	cells = append(cells,
		"YATIRIM İŞLEMLERİNDE KULLANILAN HESAP BAKİYELERİ", "Hesap", "IBAN", "Para", "Birimi", "Tutar(YP)", "Tutar(TL)", "0000-0000000", "TR000000000000000000000000", "TL", "100,00", "TOPLAM", "100,00",
		"YATIRIM FONU EKSTRESİ", "YATIRIM FONU BAKİYELERİ", "Hesap", "Fon Adı", "Fon", "Kodu", "Para", "Birimi", "Adet", "Fiyat", "Tutar(YP)", "Tutar(TL)",
		"0000-0000000", "İSTANBUL PORTFÖY", "ONDÖRDÜNCÜ SERBEST", "IIE", "TL", "1.000", "10,074324", "10.074,32",
		"AZİMUT PORTFÖY", "İSTATİSTİKSEL ARBİTR", "SPN", "TL", "2.000", "7,512477", "15.024,95",
		"GP PARA PİYASASI", "SERBEST(TL) FON", "GJH", "TL", "10.000", "2,808972", "28.089,72",
		"TERA PORTFÖY BİRİNCİ", "SERBEST FON", "TLY", "TL", "3", "1.446,711891", "4.340,14",
		"GPY  NEMA PARA PİY (TL)", "FONU", "GNP", "TL", "5.000", "1,055914", "5.279,57",
		"TOPLAM", "62.808,70",
		"YATIRIM FON HAREKETLERİ", "Hesap No : 0000-0000000 - TL", "Tarih", "Zaman", "Fon Adı", "İşlem", "Tipi", "İşlem Detay", "Adet", "Fiyat", "Stopaj", "Tutar",
	)
	return append(cells, moves...)
}

func page2(moves ...string) []string {
	cells := append(legal(2), "YATIRIM FON HAREKETLERİ")
	cells = append(cells, moves...)
	return append(cells, "T. Garanti Bankası A.Ş.")
}

var moves1 = []string{
	"01.09.2025", "09:12:49", "GP PARA PİYASASI SERBEST(TL)", "FON", "ALIŞ", "3.000", "2,721431", "0,00", "8.164,29",
	"01.09.2025", "09:41:43", "GPY  NEMA PARA PİY (TL) FONU", "ALIŞ", "NEMA", "1.000", "1,023062", "0,00", "1.023,06",
	"01.09.2025", "10:56:19", "GP PARA PİYASASI SERBEST(TL)", "FON", "SATIŞ", "1.000", "2,721431", "13,86", "2.721,43",
	"01.09.2025", "10:56:42", "GPY  NEMA PARA PİY (TL) FONU", "ALIŞ", "NEMA", "2.000", "1,023062", "0,00", "2.046,12",
}

var moves2 = []string{
	"02.09.2025", "14:26:17", "GPY  NEMA PARA PİY (TL) FONU", "SATIŞ", "NEMA", "1.500", "1,024597", "2,63", "1.536,90",
	"02.09.2025", "14:26:17", "AZİMUT PORTFÖY İSTATİSTİKSEL", "ARBİTR", "ALIŞ", "200", "7,290689", "0,00", "1.458,14",
	"03.09.2025", "10:00:00", "AZİMUT PYŞ KAR PAYI ÖDEYEN", "HİSSE SE", "SATIŞ", "100", "3,500000", "0,00", "350,00",
}

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// tefas fills the fields every trade of the statement shares.
func tefas(tr statement.Trade) statement.Trade {
	tr.Currency, tr.Market = "TRY", "TEFAS"
	return tr
}

func cash(date, kind, amount string) statement.CashMovement {
	return statement.CashMovement{Date: date, Kind: kind, Amount: d(amount), Currency: "TRY"}
}

func sample() statement.Statement {
	return statement.Statement{
		Broker: "garanti", PeriodStart: "2025-09-01", PeriodEnd: "2025-09-30", Currency: "TRY", Market: "TEFAS",
		Trades: []statement.Trade{
			tefas(statement.Trade{Date: "2025-09-01", Symbol: "GJH", Side: "buy", Quantity: d("2000"), Price: d("2.721431"), Amount: d("5442.86"), Fee: d("13.86")}),
			tefas(statement.Trade{Date: "2025-09-01", Symbol: "GNP", Side: "buy", Quantity: d("3000"), Price: d("1.023062"), Amount: d("3069.18")}),
			tefas(statement.Trade{Date: "2025-09-02", Symbol: "GNP", Side: "sell", Quantity: d("1500"), Price: d("1.024597"), Amount: d("1536.90"), Fee: d("2.63")}),
			tefas(statement.Trade{Date: "2025-09-02", Symbol: "SPN", Side: "buy", Quantity: d("200"), Price: d("7.290689"), Amount: d("1458.14")}),
			tefas(statement.Trade{Date: "2025-09-03", Symbol: "AZİMUT PYŞ KAR PAYI ÖDEYEN HİSSE SE", Side: "sell", Quantity: d("100"), Price: d("3.5"), Amount: d("350.00")}),
		},
		Cash: []statement.CashMovement{
			cash("2025-09-01", "deposit", "8525.90"),
			cash("2025-09-02", "withdrawal", "76.13"),
			cash("2025-09-03", "withdrawal", "350.00"),
		},
		Holdings: []statement.Holding{
			{Symbol: "IIE", Quantity: d("1000"), Currency: "TRY", Price: d("10.074324"), Name: "İSTANBUL PORTFÖY ONDÖRDÜNCÜ SERBEST"},
			{Symbol: "SPN", Quantity: d("2000"), Currency: "TRY", Price: d("7.512477"), Name: "AZİMUT PORTFÖY İSTATİSTİKSEL ARBİTR"},
			{Symbol: "GJH", Quantity: d("10000"), Currency: "TRY", Price: d("2.808972"), Name: "GP PARA PİYASASI SERBEST(TL) FON"},
			{Symbol: "TLY", Quantity: d("3"), Currency: "TRY", Price: d("1446.711891"), Name: "TERA PORTFÖY BİRİNCİ SERBEST FON"},
			{Symbol: "GNP", Quantity: d("5000"), Currency: "TRY", Price: d("1.055914"), Name: "GPY NEMA PARA PİY (TL) FONU"},
		},
	}
}

// same compares two statements with decimals by value: %+v prints a decimal
// through its String, which drops trailing zeros.
func same(t *testing.T, got, want statement.Statement) {
	t.Helper()
	if g, w := fmt.Sprintf("%+v", got), fmt.Sprintf("%+v", want); g != w {
		t.Errorf("statement\n got %s\nwant %s", g, w)
	}
}

func TestParse(t *testing.T) {
	unknown := []string{"04.09.2025", "11:00:00", "GP PARA PİYASASI SERBEST(TL)", "FON", "VİRMAN", "10", "2,800000", "0,00", "28,00"}
	short := []string{"04.09.2025", "11:30:00", "GP PARA PİYASASI SERBEST(TL)", "FON", "ALIŞ", "10", "2,800000"}
	withErrors := sample()
	withErrors.Errors = []statement.ParseError{
		{Section: "movements", Row: unknown, Code: "unknown_type", Value: "VİRMAN", Message: "unknown fund movement type VİRMAN", Date: "2025-09-04"},
		{Section: "movements", Row: short, Code: "bad_row", Message: "incomplete fund movement", Date: "2025-09-04"},
	}

	// A buy and a sell of the same units at the same price leave only the
	// stopaj, and the day's cash brings it in.
	zero := []string{
		"05.09.2025", "09:00:00", "GP PARA PİYASASI SERBEST(TL)", "FON", "ALIŞ", "1.000", "2,800000", "0,00", "2.800,00",
		"05.09.2025", "10:00:00", "GP PARA PİYASASI SERBEST(TL)", "FON", "SATIŞ", "1.000", "2,800000", "1,25", "2.800,00",
	}
	withFee := sample()
	withFee.Cash = append(withFee.Cash, cash("2025-09-05", "fee", "1.25"), cash("2025-09-05", "deposit", "1.25"))

	// A negative stopaj is a refund: the sell books no fee and the day's
	// withdrawal is the gross Tutar.
	refund := []string{"04.09.2025", "14:40:18", "ÖRNEK PORTFÖY YABANCI", "TEKNOLOJ", "SATIŞ", "12", "500,250000", "-                 9,87", "6.003,00"}
	withRefund := sample()
	withRefund.Trades = append(withRefund.Trades, tefas(statement.Trade{Date: "2025-09-04", Symbol: "ÖRNEK PORTFÖY YABANCI TEKNOLOJ", Side: "sell", Quantity: d("12"), Price: d("500.25"), Amount: d("6003.00")}))
	withRefund.Cash = append(withRefund.Cash, cash("2025-09-04", "withdrawal", "6003.00"))

	// Adet, Fiyat and Tutar carry no sign (ADR 0018): a signed SATIŞ is
	// unreadable, never a buy (GLG-1304).
	signed := []string{"04.09.2025", "15:00:00", "AZİMUT PORTFÖY İSTATİSTİKSEL", "ARBİTR", "SATIŞ", "-200", "7,600000", "3,00", "-1.520,00"}
	withSigned := sample()
	withSigned.Errors = []statement.ParseError{{Section: "movements", Row: signed, Code: "bad_number", Value: "quantity", Message: "unreadable quantity", Date: "2025-09-04"}}

	tests := []struct {
		name string
		rows [][]string
		want statement.Statement
	}{
		{"sample", [][]string{page1(moves1...), page2(moves2...)}, sample()},
		{"unknown type and cut short", [][]string{page1(moves1...), page2(slices.Concat(moves2, unknown, short)...)}, withErrors},
		{"net zero with stopaj", [][]string{page1(moves1...), page2(slices.Concat(moves2, zero)...)}, withFee},
		{"negative stopaj", [][]string{page1(moves1...), page2(slices.Concat(moves2, refund)...)}, withRefund},
		{"signed sell", [][]string{page1(moves1...), page2(slices.Concat(moves2, signed)...)}, withSigned},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := garanti.Parse(tc.rows)
			if err != nil {
				t.Fatal(err)
			}
			same(t, got, tc.want)
			for _, e := range got.Errors {
				for _, c := range customer {
					if strings.Contains(e.Value, c) || slices.Contains(e.Row, c) {
						t.Errorf("error %+v carries customer cell %q", e, c)
					}
				}
			}
		})
	}
}

// TestDetect covers GLG-764: with every PDF parser registered, a Garanti
// fund statement is detected as garanti and parses as an explicit pick does.
func TestDetect(t *testing.T) {
	rows := [][]string{page1(moves1...), page2(moves2...)}
	key, got, err := statement.Detect(rows)
	if err != nil || key != "garanti" {
		t.Fatalf("Detect = %q, %v; want garanti", key, err)
	}
	want, _ := garanti.Parse(rows)
	same(t, got, want)
}

func TestParseNotGaranti(t *testing.T) {
	rows := [][]string{page1(moves1...)[3:], page2(moves2...)}
	if _, err := garanti.Parse(rows); err == nil || err.Error() != "not a garanti fund statement" {
		t.Fatalf("err = %v, want not a garanti fund statement", err)
	}
}

// A fund held in two accounts is listed once per account: its movements
// resolve to its code and the snapshot holds it once, units summed (GLG-724).
func TestParseFundInTwoAccounts(t *testing.T) {
	cells := append(append([]string{}, customer...),
		"YATIRIM FONU BAKİYELERİ", "Hesap", "Fon Adı", "Fon", "Kodu", "Para", "Birimi", "Adet", "Fiyat", "Tutar(YP)", "Tutar(TL)",
		"1234-6000001", "ÖRNEK PORTFÖY", "YABANCI TEKNOLOJ", "YAY", "TL", "40", "700,000000", "28.000,00",
		"1234-6000002", "ÖRNEK PORTFÖY", "YABANCI TEKNOLOJ", "YAY", "TL", "15", "700,000000", "10.500,00",
		"TOPLAM", "38.500,00",
		"YATIRIM FON HAREKETLERİ",
		"01.09.2025", "10:00:00", "ÖRNEK PORTFÖY YABANCI", "TEKNOLOJ", "ALIŞ", "10", "700,000000", "0,00", "7.000,00",
	)
	got, err := garanti.Parse([][]string{cells})
	if err != nil {
		t.Fatal(err)
	}
	same(t, got, statement.Statement{
		Broker: "garanti", PeriodStart: "2025-09-01", PeriodEnd: "2025-09-30", Currency: "TRY", Market: "TEFAS",
		Trades:   []statement.Trade{tefas(statement.Trade{Date: "2025-09-01", Symbol: "YAY", Side: "buy", Quantity: d("10"), Price: d("700"), Amount: d("7000")})},
		Cash:     []statement.CashMovement{cash("2025-09-01", "deposit", "7000")},
		Holdings: []statement.Holding{{Symbol: "YAY", Quantity: d("55"), Currency: "TRY", Price: d("700"), Name: "ÖRNEK PORTFÖY YABANCI TEKNOLOJ"}},
	})
}

// A crafted statement with many funds and movements must parse in time
// linear in its size: matching every movement against every fund balance
// record took seconds of CPU per upload (GLG-1270).
func TestParseManyFunds(t *testing.T) {
	const n = 30000
	const digits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	code := func(i int) string { return string([]byte{digits[i/36/36], digits[i/36%36], digits[i%36]}) }
	cells := []string{"OCAK 2026 YATIRIM EKSTRESİ", "YATIRIM FONU BAKİYELERİ"}
	for i := range n {
		cells = append(cells, fmt.Sprintf("FON %05d", i), code(i), "TL", "1", "2", "3")
	}
	cells = append(cells, "TOPLAM", "YATIRIM FON HAREKETLERİ")
	for i := range n {
		cells = append(cells, "05.01.2026", "10:00:00", fmt.Sprintf("FON %05d", i), "ALIŞ", "1", "1", "0", "5")
	}
	start := time.Now()
	got, err := garanti.Parse([][]string{cells})
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Parse took %v", elapsed)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Trades) != n || len(got.Holdings) != n || len(got.Errors) != 0 {
		t.Fatalf("got %d trades, %d holdings, %d errors, want %d, %d, 0", len(got.Trades), len(got.Holdings), len(got.Errors), n, n)
	}
	for i, tr := range got.Trades {
		if tr.Symbol != code(i) || got.Holdings[i].Symbol != code(i) {
			t.Fatalf("row %d: trade %s, holding %s, want %s", i, tr.Symbol, got.Holdings[i].Symbol, code(i))
		}
	}
}
