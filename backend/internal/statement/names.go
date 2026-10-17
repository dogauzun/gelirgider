package statement

import (
	"context"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/dogauzun/gelirgider/backend/internal/instrument"
)

// fundAbbrevs are the words Garanti abbreviates its fund names with, and
// what they stand for. Cut words such as PİY or HİS need no entry: a word
// matches any fund name word it starts (see fundByName).
var fundAbbrevs = map[string][]string{
	"GPY": {"GARANTİ", "PORTFÖY"},
	"GP":  {"GARANTİ", "PORTFÖY"},
	"1.":  {"BİRİNCİ"},
	"2.":  {"İKİNCİ"},
	"3.":  {"ÜÇÜNCÜ"},
}

// namedFund is a TEFAS fund with its name's fundWords.
type namedFund struct {
	instrument.Instrument
	words []string
}

// nameInstrument returns the instrument a fund name that is no symbol
// resolves to: the one of the learned names it matches (see learnedName),
// else, for a garanti TEFAS row, the one TEFAS fund whose name it is cut or
// abbreviated from (see fundByName). A nil learned is a symbol
// the statement's own balances keep unresolved, which stays so. The TEFAS
// funds are read and split into words once per request, into s.funds.
func (h *handler) nameInstrument(ctx context.Context, s *lookupSession, learned *nameIndex, row *previewRow) (instrument.Instrument, error) {
	if id := learnedName(row.Symbol, learned); id != 0 {
		return h.instruments.Get(ctx, id)
	}
	if learned == nil || s.broker != "garanti" || row.Market != "TEFAS" {
		return instrument.Instrument{}, instrument.ErrNotFound
	}
	if s.funds == nil {
		all, err := h.instruments.All(ctx, "TEFAS")
		if err != nil {
			return instrument.Instrument{}, err
		}
		s.funds = make([]namedFund, 0, len(all))
		for _, f := range all {
			s.funds = append(s.funds, namedFund{f, fundWords(f.Name, false)})
		}
	}
	if in, ok := fundByName(row.Symbol, s.funds); ok {
		return in, nil
	}
	return instrument.Instrument{}, instrument.ErrNotFound
}

// fundByName returns the one fund whose name the statement name is cut or
// abbreviated from, the abbreviations expanded and case, spacing and
// punctuation ignored. The closest kind of match wins: the same words, else
// the same words but a last one cut mid-word ("ŞİRKETL"), else every word
// starting the fund name's word in the same place ("PİY"). So "AK PORTFÖY"
// names AK PORTFÖY's fund, not AKTİF PORTFÖY's. No fund, or two of the
// closest kind, is false.
func fundByName(name string, funds []namedFund) (instrument.Instrument, bool) {
	words := fundWords(name, true)
	if len(words) == 0 {
		return instrument.Instrument{}, false
	}
	var match instrument.Instrument
	best, count := 3, 0
	for _, f := range funds {
		k := matchKind(words, f.words)
		switch {
		case k < best:
			best, count, match = k, 1, f.Instrument
		case k == best && k < 3 && f.ID != match.ID:
			count++
		}
	}
	if count != 1 {
		return instrument.Instrument{}, false
	}
	return match, true
}

// matchKind is how closely the statement's words match a fund's: 0 the
// same words, 1 the same but a cut last one, 2 each a start of the fund's
// word in the same place, 3 no match.
func matchKind(words, fund []string) int {
	if len(fund) < len(words) {
		return 3
	}
	k := 0
	for i, w := range words {
		switch {
		case fund[i] == w:
		case !strings.HasPrefix(fund[i], w):
			return 3
		case i == len(words)-1:
			k = max(k, 1)
		default:
			k = 2
		}
	}
	if k == 0 && len(fund) > len(words) {
		k = 1
	}
	return k
}

// fundWords upper-cases name the Turkish way and splits it into its runs of
// letters and digits, expanding fundAbbrevs when expand is set.
func fundWords(name string, expand bool) []string {
	var out []string
	for _, field := range strings.Fields(strings.ToUpperSpecial(unicode.TurkishCase, name)) {
		if long, ok := fundAbbrevs[field]; ok && expand {
			out = append(out, long...)
			continue
		}
		for _, w := range strings.FieldsFunc(field, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
			if long, ok := fundAbbrevs[w]; ok && expand {
				out = append(out, long...)
				continue
			}
			out = append(out, w)
		}
	}
	return out
}

// nameIndex is a set of learned fund names, searchable by prefix. sorted
// holds the names in byte order, so those that start with a symbol are one
// run, and sortedIDs their instrument ids; sameUntil[i] is the first index
// after i whose id differs from sortedIDs[i], so a run is of one instrument
// when sameUntil[start] reaches its end. ids maps each name to its
// instrument; maxLen is the longest name.
type nameIndex struct {
	sorted    []string
	sortedIDs []int64
	sameUntil []int
	ids       map[string]int64
	maxLen    int
}

// newNameIndex indexes the names of ids, each with its instrument id.
func newNameIndex(ids map[string]int64) *nameIndex {
	x := &nameIndex{sorted: make([]string, 0, len(ids)), ids: ids}
	for name := range ids {
		x.sorted = append(x.sorted, name)
		x.maxLen = max(x.maxLen, len(name))
	}
	slices.Sort(x.sorted)
	x.sortedIDs = make([]int64, len(x.sorted))
	x.sameUntil = make([]int, len(x.sorted))
	for i := len(x.sorted) - 1; i >= 0; i-- {
		x.sortedIDs[i] = ids[x.sorted[i]]
		x.sameUntil[i] = i + 1
		if i+1 < len(x.sorted) && x.sortedIDs[i+1] == x.sortedIDs[i] {
			x.sameUntil[i] = x.sameUntil[i+1]
		}
	}
	return x
}

// learnedName returns the instrument id of the learned names that equal
// symbol, start with it or that it starts with: the rule the garanti parser
// maps a movement to a balance by inside one statement, as statement names
// are cut to a fixed width. It returns 0 for no match, or when the matches
// name more than one instrument. A nil learned has no names. The cost
// depends on symbol, not on how many names are stored: the names it starts
// with are its shorter prefixes, each one map lookup, and the names that
// start with it are one run of the sorted names, found by two binary
// searches and checked for one instrument in constant time.
func learnedName(symbol string, learned *nameIndex) int64 {
	if symbol == "" || learned == nil {
		return 0
	}
	var match int64
	found := false
	add := func(id int64) bool {
		if found && match != id {
			return false
		}
		match, found = id, true
		return true
	}
	for n := 0; n < len(symbol) && n <= learned.maxLen; n++ {
		if id, ok := learned.ids[symbol[:n]]; ok && !add(id) {
			return 0
		}
	}
	lo, _ := slices.BinarySearch(learned.sorted, symbol)
	hi := lo + sort.Search(len(learned.sorted)-lo, func(i int) bool {
		return !strings.HasPrefix(learned.sorted[lo+i], symbol)
	})
	if lo < hi {
		if learned.sameUntil[lo] < hi || !add(learned.sortedIDs[lo]) {
			return 0
		}
	}
	return match
}

// statementNames is what st's balances say of each fund name they print:
// the instrument of its code, or 0 when they print it for two codes (a
// holding is one per code), like the parser's code() leaves such a name
// unresolved, or when its code resolves to no instrument. holdIns are st's
// holdings' instruments.
func statementNames(st Statement, holdIns []instrument.Instrument) map[string]int64 {
	out := map[string]int64{}
	for i, hold := range st.Holdings {
		if hold.Name == "" {
			continue
		}
		if _, seen := out[hold.Name]; seen {
			out[hold.Name] = 0
			continue
		}
		out[hold.Name] = holdIns[i].ID
	}
	return out
}
