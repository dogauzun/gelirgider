package deposit

import "testing"

// TestInterest checks the term interest by hand: 100000 x 0.45 x 32 / 365 =
// 3945.2054 -> 3945.21, stopaj 3945.21 x 0.175 = 690.41175 -> 690.41, net
// 3254.80.
func TestInterest(t *testing.T) {
	terms := Terms{Rate: d("0.45"), StopajRate: d("0.175"), StartDate: "2026-01-01", MaturityDate: "2026-02-02"}
	gross, stopaj := Interest(d("100000"), terms)
	if !gross.Equal(d("3945.21")) || !stopaj.Equal(d("690.41")) {
		t.Errorf("gross, stopaj = %s, %s, want 3945.21, 690.41", gross, stopaj)
	}
	if net := gross.Sub(stopaj); !net.Equal(d("3254.80")) {
		t.Errorf("net = %s, want 3254.80", net)
	}
	terms.MaturityDate = "bad"
	if gross, stopaj := Interest(d("100000"), terms); !gross.IsZero() || !stopaj.IsZero() {
		t.Errorf("bad date: gross, stopaj = %s, %s, want 0, 0", gross, stopaj)
	}
}
