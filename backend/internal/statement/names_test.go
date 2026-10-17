package statement

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestLearnedName checks the prefix rule: equal, cut shorter or
// longer resolves; matches of two instruments or none do not.
func TestLearnedName(t *testing.T) {
	learned := map[string]int64{
		"ALFA PORTFÖY DENGELİ DEĞİŞKEN FON":   1,
		"BETA PORTFÖY KISA VADELİ BORÇLANMA":  2,
		"BETA PORTFÖY KISA VADELİ SERBEST FO": 3,
		"BETA PORTFÖY KISA VADELİ SERBEST":    3,
	}
	for symbol, want := range map[string]int64{
		"ALFA PORTFÖY DENGELİ DEĞİŞKEN FON":      1,
		"ALFA PORTFÖY DENGELİ":                   1,
		"ALFA PORTFÖY DENGELİ DEĞİŞKEN FON (TL)": 1,
		"BETA PORTFÖY KISA VADELİ SERBEST":       3,
		"BETA PORTFÖY KISA VADELİ":               0,
		"GAMA PORTFÖY":                           0,
		"":                                       0,
	} {
		if got := learnedName(symbol, newNameIndex(learned)); got != want {
			t.Errorf("learnedName(%q) = %d, want %d", symbol, got, want)
		}
	}
}

// TestLearnedNameManyNames checks that matching a symbol costs the file, not
// the user's stored names: 5000 unknown symbols against 100k learned names
// must finish in well under the second the linear scan took tens of.
func TestLearnedNameManyNames(t *testing.T) {
	learned := make(map[string]int64, 100000)
	for i := 0; i < 100000; i++ {
		learned[fmt.Sprintf("ZQ%d%s", i, strings.Repeat("X", 150))] = 1
	}
	start := time.Now()
	names := newNameIndex(learned)
	for i := 0; i < 5000; i++ {
		if got := learnedName(fmt.Sprintf("QQ%d", i), names); got != 0 {
			t.Fatalf("learnedName matched %d", got)
		}
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("5000 lookups in %v, want under 1s", d)
	}
}

// TestLearnedNameSharedPrefix checks the worst case for the names a symbol is
// a prefix of: 100k names of one instrument that share a long prefix, and a
// file whose symbols are that prefix cut at every length. Each symbol matches
// every name, so a lookup that visits the matches costs the stored names.
func TestLearnedNameSharedPrefix(t *testing.T) {
	prefix := strings.Repeat("A", 190)
	learned := make(map[string]int64, 100000)
	for i := 0; i < 100000; i++ {
		learned[fmt.Sprintf("%s%d", prefix, i)] = 1
	}
	start := time.Now()
	names := newNameIndex(learned)
	for n := 1; n < len(prefix); n++ {
		if got := learnedName(prefix[:n], names); got != 1 {
			t.Fatalf("learnedName(%d As) = %d, want 1", n, got)
		}
	}
	learned[prefix+"x"] = 2
	names = newNameIndex(learned)
	if got := learnedName(prefix[:50], names); got != 0 {
		t.Errorf("learnedName with two instruments = %d, want 0", got)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("lookups in %v, want under 1s", d)
	}
}
