package statement

import "testing"

func TestGrouped(t *testing.T) {
	for _, c := range []struct {
		s, sep, mark string
		want         bool
	}{
		{"1.234,5", ".", ",", true},
		{"-1.234.567,8", ".", ",", true},
		{"1,234.56", ",", ".", true},
		{"0,5", ".", ",", true},
		{"0.5", ",", ".", true},
		{"10.000", ".", ",", true},
		{"1.2.3,4", ".", ",", false},
		// A first group starting with 0 is not grouping, or "0.234,5"
		// reads as 234.5.
		{"0.234,5", ".", ",", false},
		{"012.345,6", ".", ",", false},
		{"0,234.5", ",", ".", false},
		{"-0.234,5", ".", ",", false},
	} {
		if got := Grouped(c.s, c.sep, c.mark); got != c.want {
			t.Errorf("Grouped(%q, %q, %q) = %v, want %v", c.s, c.sep, c.mark, got, c.want)
		}
	}
}

func TestParseNumber(t *testing.T) {
	for _, c := range []struct {
		cell, sep, mark string
		want            string // "" means unreadable
	}{
		// US format.
		{"1,250.000", ",", ".", "1250"},
		{"-3,382.40", ",", ".", "-3382.4"},
		{"+12,615.20", ",", ".", "12615.2"},
		{"88702.860000", ",", ".", "88702.86"},
		// Turkish format.
		{"89.997,72", ".", ",", "89997.72"},
		{"33.070", ".", ",", "33070"},
		{"2,721431", ".", ",", "2.721431"},
		{"-3.382,40", ".", ",", "-3382.4"},
		{"+1.250,000", ".", ",", "1250"},
		// Whitespace around the cell.
		{"  1.234,5 ", ".", ",", "1234.5"},
		{"\t-7.5\n", ",", ".", "-7.5"},
		// Bad thousands groups.
		{"1.2.3,4", ".", ",", ""},
		{"0.234,5", ".", ",", ""},
		{"1,234.5,6", ",", ".", ""},
		// Not the transaction API's shape: exponent, overlong, junk.
		{"1e2000000000", ",", ".", ""},
		{"12345678901234567890123", ",", ".", ""},
		{"1.2345678901234", ",", ".", ""},
		{"", ".", ",", ""},
		{"-", ".", ",", ""},
		{"abc", ".", ",", ""},
		{"1 234,5", ".", ",", ""},
		{"--5", ",", ".", ""},
	} {
		got, err := ParseNumber(c.cell, c.sep, c.mark)
		switch {
		case c.want == "" && err == nil:
			t.Errorf("ParseNumber(%q, %q, %q) = %s, want unreadable", c.cell, c.sep, c.mark, got)
		case c.want != "" && err != nil:
			t.Errorf("ParseNumber(%q, %q, %q): %v, want %s", c.cell, c.sep, c.mark, err, c.want)
		case c.want != "" && got.String() != c.want:
			t.Errorf("ParseNumber(%q, %q, %q) = %s, want %s", c.cell, c.sep, c.mark, got, c.want)
		}
	}
}

func TestMaskIdentifiers(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		// Midas deposits name the sending bank and its IBAN.
		{"Garanti BBVA - TR000000000000000000001234", "Garanti BBVA - TR** **** 1234"},
		{"BANKA ADI - TR00 0000 0000 0000 0000 0012 34", "BANKA ADI - TR** **** 1234"},
		// Garanti BBVA Yatırım transfers name the bank account.
		{"123456-100 Nolu Hesabın Banka Takas İşlemi", "***6-100 Nolu Hesabın Banka Takas İşlemi"},
		{"Diğer Bakiye Transferi-1234-6000001", "Diğer Bakiye Transferi-***0001"},
		{"Temettü Transferi işlemi - 1234567-100 No'lu Hesap", "Temettü Transferi işlemi - ***7-100 No'lu Hesap"},
		{"OPT25.F Temettü 1234.567 Adet Temettü Oranı : 10.5-7654321-3", "OPT25.F Temettü 1234.567 Adet Temettü Oranı : 10.5-***321-3"},
		// Nothing else in a description changes.
		{"41,18 USD/TRY kurundan 8.237,46 dolar satış", "41,18 USD/TRY kurundan 8.237,46 dolar satış"},
		{"Borsa Emir İptal Ücreti ve Vergi-7", "Borsa Emir İptal Ücreti ve Vergi-7"},
		{"123456 100 adet RGYAS Halka Arz Tahsil", "123456 100 adet RGYAS Halka Arz Tahsil"},
		// A date is no account number: a CSV row's raw date cell.
		{"2025-09-29", "2025-09-29"},
		{"2025-09", "2025-09"},
		{"29.09.2025", "29.09.2025"},
		// An amount after a spaced IBAN stays out of the mask.
		{"BANKA ADI - TR00 0000 0000 0000 0000 0012 34 1000 TL", "BANKA ADI - TR** **** 1234 1000 TL"},
	} {
		if got := MaskIdentifiers(c.in); got != c.want {
			t.Errorf("MaskIdentifiers(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
