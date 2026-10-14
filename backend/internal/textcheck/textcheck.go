// Package textcheck holds the invisible-character and visible-rune checks
// shared by the free-text fields and the email address. It imports nothing from the app,
// so auth can use it although portfolio imports auth (GLG-978).
package textcheck

import "unicode"

// HasInvisible reports whether s holds a control or format character, a
// line or paragraph separator, or a blank filler. Portfolio names, the
// transaction note (GLG-354), instrument symbols and names, and the email
// address at registration (GLG-978) reject them.
//
// U+2028 and U+2029 are categories Zl and Zp, not Cc, so IsControl misses
// them, but they are line breaks like newline and both fields are single-line
// (GLG-683).
//
// The fillers are letters or symbols to Unicode, so no category test catches
// them, but they render as empty space (GLG-429): Hangul fillers U+115F,
// U+1160, U+3164, U+FFA0 and the Braille blank U+2800.
//
// The zero-width joiner U+200D is a format character too, but between two
// emoji it glues them into one visible emoji (family, rainbow flag), so it
// passes there and only there (GLG-901).
//
// Tag characters U+E0020-E007F are format characters as well, but after a
// black flag U+1F3F4 the tag letters "gbeng", "gbsct" or "gbwls" closed by
// CANCEL TAG U+E007F make one visible subdivision flag (England, Scotland,
// Wales), so those three runs pass. Any other tag run renders as a plain
// black flag with a hidden payload, which GLG-142 and GLG-334 refuse, so it
// and every other tag character do not (GLG-907).
func HasInvisible(s string) bool {
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch r {
		case '\u115f', '\u1160', '\u3164', '\uffa0', '\u2800':
			return true
		case '\U0001F3F4':
			i = flagTagsEnd(rs, i)
			continue
		case '\u200d':
			if !joinsEmoji(rs, i) {
				return true
			}
			continue
		}
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return true
		}
	}
	return false
}

// flagTagsEnd returns the index of the CANCEL TAG closing one of the three
// subdivision flag tag runs that follows the black flag at rs[i], or i when
// none follows.
func flagTagsEnd(rs []rune, i int) int {
	if i+7 > len(rs) {
		return i
	}
	switch string(rs[i+1 : i+7]) {
	case "\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F",
		"\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F",
		"\U000E0067\U000E0062\U000E0077\U000E006C\U000E0073\U000E007F":
		return i + 6
	}
	return i
}

// joinsEmoji reports whether the zero-width joiner at rs[i] sits between two
// emoji. The emoji before it may carry a U+FE0F presentation selector or a
// skin-tone modifier (U+1F3FB-U+1F3FF).
//
// Emoji means the pictograph blocks (U+1F300-1F64F, 1F680-1F6FF, 1F7E0-1F7EB,
// 1F90C-1F9FF, 1FA70-1FAFF) plus the few older pictographs that emoji ZWJ
// sequences use (health worker, pirate flag, black cat...). Neighbouring
// symbol blocks such as enclosed letters, alchemical or chess symbols and
// arrows are left out, as is the symbol category as a whole: "★" + ZWJ + "★"
// looks like "★★" but is stored differently, which GLG-142 and GLG-334 refuse.
func joinsEmoji(rs []rune, i int) bool {
	j := i - 1
	for j >= 0 && (rs[j] == '\ufe0f' || (rs[j] >= 0x1f3fb && rs[j] <= 0x1f3ff)) {
		j--
	}
	return j >= 0 && i+1 < len(rs) && isEmoji(rs[j]) && isEmoji(rs[i+1])
}

func isEmoji(r rune) bool {
	switch r {
	case '\u2194', '\u2195', '\u2620', '\u2640', '\u2642', '\u2695', '\u2696', '\u26a7',
		'\u26d3', '\u26f9', '\u2708', '\u2744', '\u2764', '\u27a1', '\u2b1b':
		return true
	}
	return r >= 0x1f300 && r <= 0x1f64f || r >= 0x1f680 && r <= 0x1f6ff ||
		r >= 0x1f7e0 && r <= 0x1f7eb || r >= 0x1f90c && r <= 0x1f9ff ||
		r >= 0x1fa70 && r <= 0x1faff
}

// IsVisible reports whether r draws something on its own: not whitespace and
// not a combining mark, which only decorates the character before it. A text
// with no visible rune renders blank: portfolio names (GLG-429), the
// transaction note and instrument symbols and names (GLG-1003) refuse it.
func IsVisible(r rune) bool {
	return !unicode.IsSpace(r) && !unicode.Is(unicode.M, r)
}
