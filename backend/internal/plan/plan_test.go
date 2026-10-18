package plan

import (
	"testing"
	"time"
)

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
