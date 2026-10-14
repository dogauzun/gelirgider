package transaction

import (
	"strings"
	"testing"
	"time"

	"github.com/dogauzun/gelirgider/backend/internal/price"
)

// TestFromRequestRejectsInstrumentOnCashTypes checks that deposit,
// withdrawal and fee reject instrument_id instead of dropping it.
func TestFromRequestRejectsInstrumentOnCashTypes(t *testing.T) {
	id, amount := int64(5), "100"
	for _, typ := range []string{TypeDeposit, TypeWithdrawal, TypeFee} {
		req := Request{Type: typ, Date: "2026-01-02", Amount: &amount, InstrumentID: &id}
		if _, err := fromRequest(req); err == nil {
			t.Errorf("%s with instrument_id: want error, got nil", typ)
		}
		req.InstrumentID = nil
		if _, err := fromRequest(req); err != nil {
			t.Errorf("%s without instrument_id: unexpected error %v", typ, err)
		}
	}
}

// TestFromRequestCapsDecimalMagnitude checks that exponents and oversized
// digit counts are rejected before qty.Mul(price) can overflow.
func TestFromRequestCapsDecimalMagnitude(t *testing.T) {
	id := int64(5)
	mk := func(qty, price, fee string) Request {
		return Request{Type: TypeBuy, Date: "2026-01-02", InstrumentID: &id, Quantity: &qty, Price: &price, Fee: &fee}
	}
	for _, bad := range []Request{
		mk("1e2000000000", "1e2000000000", "0"),
		mk("1234567890123456", "1", "0"),
		mk("1", "1.1234567891", "0"),
		mk("1", "1", "1e9"),
		mk("-1", "1", "0"),
		mk("0", "1", "0"),
	} {
		if _, err := fromRequest(bad); err == nil {
			t.Errorf("qty=%s price=%s fee=%s: want error, got nil", *bad.Quantity, *bad.Price, *bad.Fee)
		}
	}
	tx, err := fromRequest(mk("123456789012345", "0.000000001", "0"))
	if err != nil {
		t.Fatalf("max-width input rejected: %v", err)
	}
	if tx.Amount.String() != "123456.789012345" {
		t.Errorf("amount = %s", tx.Amount.String())
	}

	// Midas prints fractional US shares with 9 decimals, and the
	// amount must stay exact.
	tx, err = fromRequest(mk("0.006218342", "331.82", "0"))
	if err != nil {
		t.Fatalf("9-decimal quantity rejected: %v", err)
	}
	if want := "2.06337024244"; tx.Amount.String() != want {
		t.Errorf("amount = %s, want %s", tx.Amount.String(), want)
	}
}

// TestFromRequestNamesDigitLimit checks that a positive value past the
// digit limit is reported as too long, not as "must be positive", for every
// decimal field; zero and non-numbers keep their own messages.
func TestFromRequestNamesDigitLimit(t *testing.T) {
	id := int64(5)
	mk := func(typ, qty, price, amount, fee string) Request {
		return Request{Type: typ, Date: "2026-01-02", InstrumentID: &id, Quantity: &qty, Price: &price, Amount: &amount, Fee: &fee}
	}
	long := "0.0000000001"
	for want, req := range map[string]Request{
		"quantity: must have at most 15 integer and 9 fraction digits":                      mk(TypeBuy, long, "1", "", ""),
		"price: must have at most 15 integer and 9 fraction digits":                         mk(TypeSell, "1", long, "", ""),
		"amount: must have at most 15 integer and 9 fraction digits":                        mk(TypeBuy, "1", "1", "1234567890123456", ""),
		"fee: must have at most 15 integer and 9 fraction digits":                           mk(TypeBuy, "1", "1", "", long),
		"quantity: must be a positive number with at most 15 integer and 9 fraction digits": mk(TypeBuy, "0", "1", "", ""),
		"fee: must be a non-negative number with at most 15 integer and 9 fraction digits":  mk(TypeBuy, "1", "1", "", "-1"),
	} {
		_, err := fromRequest(req)
		if err == nil || err.Error() != want {
			t.Errorf("got %v, want %q", err, want)
		}
	}
	dividend := Request{Type: TypeDividend, Date: "2026-01-02", InstrumentID: &id, Amount: &long}
	if _, err := fromRequest(dividend); err == nil || !strings.HasPrefix(err.Error(), "amount: must have at most") {
		t.Errorf("dividend: got %v", err)
	}
}

// TestFromRequestKeepsTradeAmount checks that a statement's rounded
// İşlem Tutarı is stored as sent, and only a trade without one gets
// quantity × price.
func TestFromRequestKeepsTradeAmount(t *testing.T) {
	id, qty, price := int64(5), "0.006218342", "331.82"
	for amount, want := range map[string]string{"2.06": "2.06", "": "2.06337024244"} {
		req := Request{Type: TypeBuy, Date: "2026-01-02", InstrumentID: &id, Quantity: &qty, Price: &price, Amount: &amount}
		tx, err := fromRequest(req)
		if err != nil {
			t.Fatalf("amount %q: unexpected error %v", amount, err)
		}
		if tx.Amount.String() != want {
			t.Errorf("amount %q: stored %s, want %s", amount, tx.Amount, want)
		}
	}

	// A priced trade needs a positive amount, buy or sell; only a
	// buy at price 0 may send amount 0, in any spelling.
	zero := "0"
	for _, typ := range []string{TypeBuy, TypeSell} {
		req := Request{Type: typ, Date: "2026-01-02", InstrumentID: &id, Quantity: &qty, Price: &price, Amount: &zero}
		if _, err := fromRequest(req); err == nil || !strings.HasPrefix(err.Error(), "amount:") {
			t.Errorf("%s with amount 0: got %v, want amount error", typ, err)
		}
	}
	free, zeroCents := "0", "0.00"
	req := Request{Type: TypeBuy, Date: "2026-01-02", InstrumentID: &id, Quantity: &qty, Price: &free, Amount: &zeroCents}
	if _, err := fromRequest(req); err != nil {
		t.Errorf("buy at price 0 with amount 0.00: unexpected error %v", err)
	}
	// A free buy moves no cash, so any other amount is rejected.
	five := "5"
	req.Amount = &five
	if _, err := fromRequest(req); err == nil || err.Error() != "amount: must be 0 when price is 0" {
		t.Errorf("buy at price 0 with amount 5: got %v, want amount error", err)
	}
}

// TestFromRequestAcceptsOwnTradeAmount checks that a stored
// quantity × price amount, which can carry more fraction digits than the parser
// accepts and which the edit form sends back, passes while a typed one stays
// capped.
func TestFromRequestAcceptsOwnTradeAmount(t *testing.T) {
	id, qty, price := int64(5), "0.123456789", "150.25"
	created, err := fromRequest(Request{Type: TypeBuy, Date: "2026-01-02", InstrumentID: &id, Quantity: &qty, Price: &price})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	amount, note := created.Amount.String(), "edited"
	edit := Request{Type: TypeBuy, Date: "2026-01-02", InstrumentID: &id, Quantity: &qty, Price: &price, Amount: &amount, Note: &note}
	tx, err := fromRequest(edit)
	if err != nil {
		t.Fatalf("edit with stored amount %s: %v", amount, err)
	}
	if !tx.Amount.Equal(created.Amount) {
		t.Errorf("amount = %s, want %s", tx.Amount, created.Amount)
	}

	other := "18.54938254726"
	edit.Amount = &other
	if _, err := fromRequest(edit); err == nil || err.Error() != "amount: "+errTooManyDigits.Error() {
		t.Errorf("other over-precise amount: got %v, want digit error", err)
	}
}

// TestFromRequestAllowsZeroPriceBuy checks that a buy at price 0 records
// units received for free (ADR 0007); a sell at price 0 stays rejected.
func TestFromRequestAllowsZeroPriceBuy(t *testing.T) {
	id, qty, zero := int64(5), "0.001", "0"
	req := Request{Type: TypeBuy, Date: "2026-01-02", InstrumentID: &id, Quantity: &qty, Price: &zero}
	tx, err := fromRequest(req)
	if err != nil {
		t.Fatalf("buy at price 0: unexpected error %v", err)
	}
	if !tx.Amount.IsZero() {
		t.Errorf("amount = %s, want 0", tx.Amount)
	}
	req.Type = TypeSell
	if _, err := fromRequest(req); err == nil {
		t.Error("sell at price 0: want error, got nil")
	}
}

// TestFromRequestRejectsLongNote checks that a note over 500 characters
// is rejected instead of stretching the transactions table off screen.
func TestFromRequestRejectsLongNote(t *testing.T) {
	amount := "100"
	ok := strings.Repeat("a", 500)
	req := Request{Type: TypeDeposit, Date: "2026-01-02", Amount: &amount, Note: &ok}
	if _, err := fromRequest(req); err != nil {
		t.Errorf("500-char note: unexpected error %v", err)
	}

	tooLong := strings.Repeat("a", 501)
	req.Note = &tooLong
	if _, err := fromRequest(req); err == nil {
		t.Error("501-char note: want error, got nil")
	}

	// 500 'ş' runes are 1000 UTF-8 bytes; the limit must count runes, not
	// bytes, or Turkish notes the frontend already accepted get rejected.
	turkish := strings.Repeat("ş", 500)
	req.Note = &turkish
	if _, err := fromRequest(req); err != nil {
		t.Errorf("500-rune Turkish note: unexpected error %v", err)
	}

	tooLongTurkish := strings.Repeat("ş", 501)
	req.Note = &tooLongTurkish
	if _, err := fromRequest(req); err == nil {
		t.Error("501-rune Turkish note: want error, got nil")
	}
}

// TestFromRequestTrimsNote checks that surrounding whitespace is not stored and
// does not count against the 500-character limit. fromRequest is the only note
// path, so create and update are both covered.
func TestFromRequestTrimsNote(t *testing.T) {
	amount := "100"
	mk := func(note string) Request {
		return Request{Type: TypeDeposit, Date: "2026-01-02", Amount: &amount, Note: &note}
	}
	tx, err := fromRequest(mk("   "))
	if err != nil {
		t.Fatalf("whitespace-only note: unexpected error %v", err)
	}
	if tx.Note != "" {
		t.Errorf("whitespace-only note = %q, want empty", tx.Note)
	}

	if tx, err = fromRequest(mk("  hello  ")); err != nil {
		t.Fatalf("padded note: unexpected error %v", err)
	}
	if tx.Note != "hello" {
		t.Errorf("padded note = %q, want %q", tx.Note, "hello")
	}

	// 500 characters plus padding: the limit counts the trimmed note.
	if _, err = fromRequest(mk("  " + strings.Repeat("a", 500) + "  ")); err != nil {
		t.Errorf("padded 500-char note: unexpected error %v", err)
	}
}

// TestFromRequestRejectsInvisibleNote checks that control and format
// characters in a note are rejected like in a portfolio name; plain Turkish
// text passes.
func TestFromRequestRejectsInvisibleNote(t *testing.T) {
	amount := "100"
	for _, note := range []string{"a\u0000b\u0007c", "line\nbreak", "zero\u200bwidth", "a\u2028b", "a\u2029b"} {
		req := Request{Type: TypeDeposit, Date: "2026-01-02", Amount: &amount, Note: &note}
		if _, err := fromRequest(req); err == nil || err.Error() != "note: must not contain control characters" {
			t.Errorf("note %q: err = %v, want control characters error", note, err)
		}
	}
	ok := "Şubat temettüsü"
	if _, err := fromRequest(Request{Type: TypeDeposit, Date: "2026-01-02", Amount: &amount, Note: &ok}); err != nil {
		t.Errorf("note %q: unexpected error %v", ok, err)
	}
}

// TestFromRequestRejectsBlankLookingNote checks that a note made only of
// combining marks (and spaces between them) renders blank, so it gets the
// same 400 as a portfolio name. A mark on a base letter and an
// empty note still pass.
func TestFromRequestRejectsBlankLookingNote(t *testing.T) {
	amount := "100"
	for _, note := range []string{"́́́", "̶̶̶", "́ ⃝"} {
		req := Request{Type: TypeDeposit, Date: "2026-01-02", Amount: &amount, Note: &note}
		if _, err := fromRequest(req); err == nil || err.Error() != "note: must not contain control characters" {
			t.Errorf("note %q: err = %v, want control characters error", note, err)
		}
	}
	for _, note := range []string{"é", "", "  "} {
		req := Request{Type: TypeDeposit, Date: "2026-01-02", Amount: &amount, Note: &note}
		if _, err := fromRequest(req); err != nil {
			t.Errorf("note %q: unexpected error %v", note, err)
		}
	}
}

// TestFromRequestRejectsFutureDate checks that a date after today
// (Europe/Istanbul) is rejected; today itself is accepted.
func TestFromRequestRejectsFutureDate(t *testing.T) {
	amount := "100"
	today := price.Today()
	if _, err := fromRequest(Request{Type: TypeDeposit, Date: today, Amount: &amount}); err != nil {
		t.Errorf("today %s: unexpected error %v", today, err)
	}
	d, _ := time.Parse(time.DateOnly, today)
	tomorrow := d.AddDate(0, 0, 1).Format(time.DateOnly)
	if _, err := fromRequest(Request{Type: TypeDeposit, Date: tomorrow, Amount: &amount}); err == nil {
		t.Errorf("tomorrow %s: want error, got nil", tomorrow)
	}
}

// TestFromRequestRejectsAncientDate checks that a mistyped year before
// price.MinDate is rejected; price.MinDate itself is accepted.
func TestFromRequestRejectsAncientDate(t *testing.T) {
	amount := "100"
	if _, err := fromRequest(Request{Type: TypeDeposit, Date: price.MinDate, Amount: &amount}); err != nil {
		t.Errorf("%s: unexpected error %v", price.MinDate, err)
	}
	if _, err := fromRequest(Request{Type: TypeDeposit, Date: "0001-01-01", Amount: &amount}); err == nil {
		t.Error("0001-01-01: want error, got nil")
	}
}

// TestParseLimit checks that the list page size defaults when absent, is
// clamped to the maximum rather than letting one request pull a whole
// history, and refuses a non-positive value.
func TestParseLimit(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"", defaultListLimit},
		{"1", 1},
		{"500", maxListLimit},
		{"5000", maxListLimit},
	} {
		got, err := parseLimit(tc.in)
		if err != nil {
			t.Errorf("parseLimit(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseLimit(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
	for _, bad := range []string{"0", "-1", "abc", "1.5"} {
		if _, err := parseLimit(bad); err == nil {
			t.Errorf("parseLimit(%q): want error, got nil", bad)
		}
	}
}

// TestParseTypes checks that an unknown or empty entry is a 400 rather
// than a silently wider result, and an absent filter stays unfiltered.
func TestParseTypes(t *testing.T) {
	got, err := parseTypes("deposit,withdrawal,fee")
	if err != nil {
		t.Fatalf("parseTypes: unexpected error %v", err)
	}
	if len(got) != 3 || got[0] != TypeDeposit || got[2] != TypeFee {
		t.Errorf("parseTypes = %v, want [deposit withdrawal fee]", got)
	}
	if got, err := parseTypes(""); err != nil || got != nil {
		t.Errorf("parseTypes(\"\") = %v, %v; want no filter and no error", got, err)
	}
	for _, bad := range []string{"buy,", ",buy", "buy,,sell", "purchase", "BUY"} {
		if _, err := parseTypes(bad); err == nil {
			t.Errorf("parseTypes(%q): want error, got nil", bad)
		}
	}
}

// TestParseCursor checks that the cursor carries both halves of the page
// order (date, id), and a malformed one is rejected instead of quietly
// restarting the list from the newest row.
func TestParseCursor(t *testing.T) {
	got, err := parseCursor("2026-01-02,42")
	if err != nil {
		t.Fatalf("parseCursor: unexpected error %v", err)
	}
	if got != (Cursor{Date: "2026-01-02", ID: 42}) {
		t.Errorf("parseCursor = %+v, want {2026-01-02 42}", got)
	}
	if got, err := parseCursor(""); err != nil || got != (Cursor{}) {
		t.Errorf("parseCursor(\"\") = %+v, %v; want the zero cursor and no error", got, err)
	}
	for _, bad := range []string{"42", "2026-01-02", "2026-13-02,42", "2026-01-02,0", "2026-01-02,abc", ",42"} {
		if _, err := parseCursor(bad); err == nil {
			t.Errorf("parseCursor(%q): want error, got nil", bad)
		}
	}
}

// TestFromRequestRejectsFeeAboveAmount checks that, for the types whose fee is
// deducted from the amount, a larger fee is rejected: it would book a negative
// dividend or cash movement. Buy keeps accepting it: there the fee is added to
// the cost.
func TestFromRequestRejectsFeeAboveAmount(t *testing.T) {
	id, amount, big, equal := int64(5), "100", "200", "100"
	for _, typ := range []string{TypeDividend, TypeDeposit, TypeWithdrawal} {
		req := Request{Type: typ, Date: "2026-01-02", Amount: &amount, Fee: &big}
		if typ == TypeDividend {
			req.InstrumentID = &id
		}
		if _, err := fromRequest(req); err == nil || !strings.Contains(err.Error(), "fee: must not exceed amount") {
			t.Errorf("%s with fee > amount: want fee error, got %v", typ, err)
		}
		req.Fee = &equal
		if _, err := fromRequest(req); err != nil {
			t.Errorf("%s with fee == amount: unexpected error %v", typ, err)
		}
	}

	// A buy's fee is added to the cost, so it is unbounded by the amount.
	one := "1"
	buy := Request{Type: TypeBuy, Date: "2026-01-02", InstrumentID: &id, Quantity: &one, Price: &one, Fee: &big}
	if _, err := fromRequest(buy); err != nil {
		t.Errorf("buy with fee > amount: unexpected error %v", err)
	}
}

// TestFromRequestRejectsFeeOnFeeType checks that a fee row's amount is
// the fee, so a fee on top of it (amount 1, fee 1000) is rejected instead of
// taking 1001 out of cash. A zero or absent fee is still accepted.
func TestFromRequestRejectsFeeOnFeeType(t *testing.T) {
	amount, big, zero := "1", "1000", "0.00"
	req := Request{Type: TypeFee, Date: "2026-01-02", Amount: &amount, Fee: &big}
	if _, err := fromRequest(req); err == nil || err.Error() != "fee: must be 0 for a fee transaction" {
		t.Errorf("fee type with fee 1000: want fee error, got %v", err)
	}
	for _, fee := range []*string{nil, &zero} {
		req.Fee = fee
		if _, err := fromRequest(req); err != nil {
			t.Errorf("fee type with zero fee: unexpected error %v", err)
		}
	}
}
