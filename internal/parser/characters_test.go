package parser_test

import (
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestCommonMarkCharacterDefinitions(t *testing.T) {
	for _, s := range []string{"", " ", "\t", " \t\t "} {
		if !parser.IsBlankContent(s) {
			t.Errorf("blank content %q rejected", s)
		}
	}
	for _, s := range []string{"\n", "\r", "\r\n", " \n", "\f", "\v", "\u0085", "\u00a0", "\u2028", "\x00", "x", "\xff"} {
		if parser.IsBlankContent(s) {
			t.Errorf("nonblank content %q accepted", s)
		}
	}
	tests := []struct {
		name string
		fn   func(rune) bool
		yes  []rune
		no   []rune
	}{
		{"Unicode whitespace", parser.IsUnicodeWhitespace, []rune{' ', '\t', '\n', '\f', '\r', '\u00a0', '\u1680', '\u2007', '\u202f', '\u3000'}, []rune{'\v', '\u0085', '\u2028', '\u2029', '\x00', 'x', -1, 0x110000}},
		{"Unicode punctuation", parser.IsUnicodePunctuation, []rune{'+', '€', '$', '!', '—', '。', '^', '🙂'}, []rune{'a', 'é', '0', ' ', '\t', '\u0301', -1, 0x110000}},
		{"ASCII controls", parser.IsASCIIControl, []rune{0, 1, '\t', '\n', 0x1f, 0x7f}, []rune{' ', '!', '~', 0x80, 0x85, -1, 0x110000}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, r := range tt.yes {
				if !tt.fn(r) {
					t.Errorf("U+%04X rejected", r)
				}
			}
			for _, r := range tt.no {
				if tt.fn(r) {
					t.Errorf("U+%04X accepted", r)
				}
			}
		})
	}
	const asciiPunctuation = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"
	for r := rune(-1); r <= 0x100; r++ {
		if got, want := parser.IsASCIIPunctuation(r), strings.ContainsRune(asciiPunctuation, r); got != want {
			t.Errorf("ASCII punctuation U+%04X = %v, want %v", r, got, want)
		}
	}
}
