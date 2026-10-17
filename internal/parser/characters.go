package parser

import "unicode"

// IsBlankContent accepts only ASCII spaces/tabs or empty content, excluding
// terminators. It does not decode, normalize or classify an entire physical line.
func IsBlankContent(content string) bool {
	for i := range len(content) {
		if content[i] != ' ' && content[i] != '\t' {
			return false
		}
	}
	return true
}

// IsUnicodeWhitespace implements CommonMark 0.31.2's Zs plus TAB/LF/FF/CR
// definition, deliberately narrower than unicode.IsSpace.
func IsUnicodeWhitespace(r rune) bool {
	return r == '\t' || r == '\n' || r == '\f' || r == '\r' || unicode.Is(unicode.Zs, r)
}

// IsUnicodePunctuation implements CommonMark 0.31.2's P or S categories.
func IsUnicodePunctuation(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) }

func IsASCIIControl(r rune) bool { return r >= 0 && r <= 0x1f || r == 0x7f }

func IsASCIIPunctuation(r rune) bool {
	return r >= 0x21 && r <= 0x2f || r >= 0x3a && r <= 0x40 || r >= 0x5b && r <= 0x60 || r >= 0x7b && r <= 0x7e
}
