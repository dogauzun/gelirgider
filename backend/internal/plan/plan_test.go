package plan

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestBookingAndMissed(t *testing.T) {
	d := decimal.RequireFromString
	tests := []struct {
		name        string
		amount, cls string
		market      string
		qty, booked string
	}{
		{"BIST rounds to whole shares", "1000", "123.45", "BIST", "8", "987.60"},
		{"TEFAS keeps 6 places", "1000", "12.345678", "TEFAS", "81.000006", "1000.00"},
		{"amount buys no unit", "10", "100", "BIST", "0", "0"},
		{"unknown market rounds to 0 places", "1000", "300", "OTHER", "3", "900"},
		{"zero close buys nothing", "1000", "0", "BIST", "0", "0"},
	}
	for _, tt := range tests {
		qty, booked := Booking(d(tt.amount), d(tt.cls), tt.market)
		if !qty.Equal(d(tt.qty)) || !booked.Equal(d(tt.booked)) {
			t.Errorf("%s: Booking = %s, %s, want %s, %s", tt.name, qty, booked, tt.qty, tt.booked)
		}
	}
	if got := Missed("2026-09-05", "2026-11-04", 5); got != 2 {
		t.Errorf("Missed over two months = %d, want 2", got)
	}
	if got := Missed("2026-11-05", "2026-11-05", 5); got != 1 {
		t.Errorf("Missed on the day = %d, want 1", got)
	}
}

func TestOccurrenceAndNext(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"day 31 in February", Occurrence(2026, time.February, 31), "2026-02-28"},
		{"day 29 in a leap February", Occurrence(2028, time.February, 29), "2028-02-29"},
		{"day 5 after the 5th", After("2026-01-05", 5), "2026-02-05"},
		{"day 5 on or after the 5th", FirstOnOrAfter("2026-01-05", 5), "2026-01-05"},
		{"day 5 on or after the 6th", FirstOnOrAfter("2026-01-06", 5), "2026-02-05"},
		{"day 31 after Jan 31", After("2026-01-31", 31), "2026-02-28"},
		{"day 31 after Feb 28", After("2026-02-28", 31), "2026-03-31"},
		{"across the year end", After("2026-12-20", 5), "2027-01-05"},
		{"bad date", After("soon", 5), ""},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestEndedAndDue(t *testing.T) {
	end := "2026-06-30"
	p := Plan{NextDate: "2026-06-05", EndDate: &end}
	if p.Ended() || !p.Due("2026-06-05") || p.Due("2026-06-04") {
		t.Errorf("plan on its last occurrence: ended %v, due today %v", p.Ended(), p.Due("2026-06-05"))
	}
	p.NextDate = "2026-07-05"
	if !p.Ended() || p.Due("2026-08-01") {
		t.Errorf("plan past its end: ended %v, due %v", p.Ended(), p.Due("2026-08-01"))
	}
}
