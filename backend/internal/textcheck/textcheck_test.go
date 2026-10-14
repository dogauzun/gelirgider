package textcheck

import "testing"

// TestHasInvisibleAllowsEmojiJoiner checks that a zero-width joiner
// between emoji (family, rainbow flag, health worker with a skin tone, black
// cat) is one visible emoji, while a joiner anywhere else, including between
// two non-emoji symbols such as stars or enclosed letters, stays invisible.
func TestHasInvisibleAllowsEmojiJoiner(t *testing.T) {
	for s, want := range map[string]bool{
		"\U0001F468\u200d\U0001F469\u200d\U0001F467 Aile": false,
		"\U0001F3F3\ufe0f\u200d\U0001F308":                false,
		"\U0001F469\U0001F3FD\u200d\u2695\ufe0f":          false,
		"a\u200db":                                        true,
		"\U0001F468\u200d":                                true,
		"\u200d\U0001F468":                                true,
		"\U0001F468\u200d\u200d\U0001F469":                true,
		"\U0001F130\u200d\U0001F131":                      true,
		"\u2605\u200d\u2605":                              true,
		"\U0001F408\u200d\u2b1b":                          false,
		"\u26f9\ufe0f\u200d\u2640\ufe0f":                  false,
		"\U0001F468\u200da":                               true,
	} {
		if got := HasInvisible(s); got != want {
			t.Errorf("HasInvisible(%q) = %v, want %v", s, got, want)
		}
	}
}

// TestHasInvisibleAllowsSubdivisionFlag checks that the England,
// Scotland and Wales flags (black flag, tag letters, cancel tag) are visible
// emoji, while any other tag run or a stray tag character stays invisible.
func TestHasInvisibleAllowsSubdivisionFlag(t *testing.T) {
	for s, want := range map[string]bool{
		"\U0001F3F4\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F Fon": false,
		"\U0001F3F4\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F":     false,
		"\U0001F3F4\U000E0067\U000E0062\U000E0077\U000E006C\U000E0073\U000E007F":     false,
		"\U0001F3F4":                         false,
		"\U0001F3F4\U000E0041\U000E007F Fon": true,
		"\U0001F3F4\U000E0067\U000E0062\U000E0065\U000E006E\U000E0068\U000E007F": true,
		"\U000E0067":                               true,
		"a\U000E0067\U000E007F":                    true,
		"\U0001F3F4\U000E007F":                     true,
		"\U0001F3F4\U000E0067\U000E0062":           true,
		"\U0001F3F4\U000E0067\U000E007F\U000E007F": true,
		"\U0001F3F4\U000E0067\U000E007F\U000E0067": true,
	} {
		if got := HasInvisible(s); got != want {
			t.Errorf("HasInvisible(%q) = %v, want %v", s, got, want)
		}
	}
}
