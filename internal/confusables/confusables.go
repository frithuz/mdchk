// Package confusables exposes the subset of Unicode 17.0.0 confusables.txt
// that MD095 uses: code points whose direct mapping is a nonempty sequence of
// ASCII letters A-Z and a-z. Its table is generated from the retained,
// hash-pinned data in testdata/unicode/17.0.0 by tools/unicode_confusables.py.
// No closure, recursion, accent stripping or other transliteration is applied.
package confusables

import "slices"

// Version is the pinned Unicode version of the retained confusables data.
const Version = "17.0.0"

type entry struct {
	source  rune
	mapping string
}

// ASCIILetters returns r's direct confusables.txt mapping when that mapping
// consists solely of ASCII letters.
func ASCIILetters(r rune) (string, bool) {
	i, ok := slices.BinarySearchFunc(asciiLetters, r, func(e entry, r rune) int { return int(e.source - r) })
	if !ok {
		return "", false
	}
	return asciiLetters[i].mapping, true
}
