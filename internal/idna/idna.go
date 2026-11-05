// Package idna implements Unicode 17.0.0 UTS #46 hostname processing with the
// one profile docs/engineering/rule-behavior.md pins for MD095:
// nontransitional processing, UseSTD3ASCIIRules, CheckHyphens, CheckBidi and
// CheckJoiners, with VerifyDnsLength for ToASCII. Its tables are generated
// from the retained, hash-pinned data in testdata/unicode/17.0.0 by
// tools/unicode_idna.py. It runs offline, has no options and never consults
// the Go or host Unicode tables.
package idna

import (
	"fmt"
	"slices"
	"strings"
)

// Error reports the first failed check. Code is the UTS #46 / IdnaTestV2
// status code; Label is the zero-based index of the failed label, or -1 when
// the check applies to the whole name. Error never includes the input.
type Error struct {
	Code  string
	Label int
	check string
}

func (e *Error) Error() string {
	if e.Label < 0 {
		return fmt.Sprintf("%s (%s)", e.check, e.Code)
	}
	return fmt.Sprintf("label %d: %s (%s)", e.Label, e.check, e.Code)
}

// Host is a canonical hostname: ASCII is the lowercase A-label form and
// Unicode the normalized Unicode form, both without a root dot.
type Host struct {
	ASCII   string
	Unicode string
}

// ToUnicode applies UTS #46 ToUnicode. Like the specification it returns the
// converted string even when it also returns an error.
func ToUnicode(s string) (string, error) {
	labels, err := process(s, unicodeMode)
	return joinUnicode(labels), err
}

// ToASCII applies UTS #46 ToASCII with VerifyDnsLength. It returns "" on error.
func ToASCII(s string) (string, error) {
	labels, err := process(s, asciiMode)
	if err != nil {
		return "", err
	}
	return joinASCII(labels), nil
}

// Canonicalize validates a bare hostname for comparison. One final root dot
// (after mapping) is removed; every other empty label is an error, and every
// A-label must round-trip exactly. Whitespace, URL syntax and wildcards fail
// the STD3 rules; nothing is trimmed or percent-decoded.
func Canonicalize(host string) (Host, error) {
	labels, err := process(host, canonicalMode)
	if err != nil {
		return Host{}, err
	}
	return Host{ASCII: joinASCII(labels), Unicode: joinUnicode(labels)}, nil
}

// ValidateHostname is Canonicalize with the result discarded, in the shape of
// engine.ValidationProviders.ValidateHostname.
func ValidateHostname(host string) error {
	_, err := Canonicalize(host)
	return err
}

type mode int

const (
	unicodeMode mode = iota
	asciiMode
	canonicalMode
)

type label struct {
	text   []rune
	aLabel string // the mapped input label when it starts with "xn--"
	ascii  string
}

const (
	zwnj = 0x200C
	zwj  = 0x200D
)

func fail(code, check string) *Error { return &Error{Code: code, Label: -1, check: check} }

func process(s string, m mode) ([]label, error) {
	mapped := make([]rune, 0, len(s))
	for _, r := range s {
		switch e := lookupMapping(r); e.status {
		case statusIgnored:
		case statusMapped:
			mapped = append(mapped, []rune(e.mapping)...)
		default:
			// Valid and, under nontransitional processing, deviation code
			// points stay; disallowed ones stay for validation to reject.
			mapped = append(mapped, r)
		}
	}
	parts := split(nfc(mapped))
	if m == canonicalMode && len(parts) > 1 && len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1]
	}
	labels := make([]label, len(parts))
	errs := make([]*Error, len(parts))
	bidi := false
	for i, p := range parts {
		labels[i].text = p
		errs[i] = convert(&labels[i], p, m != unicodeMode)
		for _, r := range labels[i].text {
			switch bidiClass(r) {
			case bidiR, bidiAL, bidiAN:
				bidi = true
			}
		}
	}
	total := len(parts) - 1
	for i := range labels {
		l := &labels[i]
		switch {
		case errs[i] != nil:
		case len(l.text) == 0:
			if m != unicodeMode {
				errs[i] = fail("A4_2", "empty label")
			} else if i < len(labels)-1 || len(labels) == 1 {
				errs[i] = fail("X4_2", "empty label")
			}
		default:
			errs[i] = validate(l.text, bidi)
		}
		if errs[i] == nil && m != unicodeMode {
			errs[i] = encode(l, m == canonicalMode)
			total += len(l.ascii)
		}
		if errs[i] != nil {
			errs[i].Label = i
			return labels, errs[i]
		}
	}
	if m != unicodeMode && total > 253 {
		return labels, fail("A4_1", "name longer than 253 octets")
	}
	return labels, nil
}

func split(s []rune) [][]rune {
	var parts [][]rune
	for {
		i := slices.Index(s, '.')
		if i < 0 {
			return append(parts, s)
		}
		parts = append(parts, s[:i:i])
		s = s[i+1:]
	}
}

// convert decodes an A-label in place. With VerifyDnsLength, a label longer
// than 63 octets is rejected before Punycode decoding; ToUnicode has no DNS
// length rule and decodes it in O(n log n).
func convert(l *label, p []rune, dnsLength bool) *Error {
	if len(p) < 4 || string(p[:4]) != "xn--" {
		return nil
	}
	l.aLabel = string(p)
	if dnsLength && len(l.aLabel) > 63 {
		return fail("A4_2", "label longer than 63 octets")
	}
	if !isASCII(p) {
		return fail("P4", "A-label contains a non-ASCII code point")
	}
	decoded, err := decodePunycode(l.aLabel[4:])
	if err != nil {
		return fail("P4", "invalid Punycode")
	}
	l.text = decoded
	if len(decoded) == 0 || isASCII(decoded) {
		return fail("P4", "A-label decodes to an empty or ASCII-only label")
	}
	return nil
}

// encode sets the ToASCII form of a valid label and checks its DNS length.
// A Unicode label of more than 59 code points cannot encode within 63
// octets, so it is rejected before Punycode encoding.
func encode(l *label, roundTrip bool) *Error {
	if isASCII(l.text) {
		l.ascii = string(l.text)
	} else if len(l.text) > 59 {
		return fail("A4_2", "label longer than 63 octets")
	} else {
		l.ascii = "xn--" + encodePunycode(l.text)
	}
	if len(l.ascii) > 63 {
		return fail("A4_2", "label longer than 63 octets")
	}
	if roundTrip && l.aLabel != "" && l.aLabel != l.ascii {
		return fail("P4", "A-label does not round-trip")
	}
	return nil
}

func isASCII(s []rune) bool {
	for _, r := range s {
		if r >= 0x80 {
			return false
		}
	}
	return true
}

// validate applies the UTS #46 validity criteria to a nonempty label.
func validate(l []rune, bidi bool) *Error {
	if !slices.Equal(nfc(l), l) {
		return fail("V1", "not in Normalization Form C")
	}
	if len(l) >= 4 && l[2] == '-' && l[3] == '-' {
		return fail("V2", "hyphens in the third and fourth positions")
	}
	if l[0] == '-' {
		return fail("V3", "leading hyphen")
	}
	if l[len(l)-1] == '-' {
		return fail("V3", "trailing hyphen")
	}
	if slices.Contains(l, '.') {
		return fail("V5", "contains a full stop")
	}
	if isMark(l[0]) {
		return fail("V6", "begins with a combining mark")
	}
	for _, r := range l {
		if s := lookupMapping(r).status; s != statusValid && s != statusDeviation {
			return fail("V7", "disallowed code point")
		}
		if r < 0x80 && !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return fail("U1", "ASCII character other than a-z, 0-9 or hyphen")
		}
	}
	if err := checkJoiners(l); err != nil {
		return err
	}
	if bidi {
		return checkBidi(l)
	}
	return nil
}

// checkJoiners applies the RFC 5892 CONTEXTJ rules. The joining context for
// U+200C is computed once per label, keeping the check linear.
func checkJoiners(l []rune) *Error {
	var before, after []bool
	for i, r := range l {
		if r != zwnj && r != zwj || i > 0 && combiningClass(l[i-1]) == virama {
			continue
		}
		if r == zwj {
			return fail("C2", "ZERO WIDTH JOINER not after a virama")
		}
		if before == nil {
			before, after = joiningContext(l)
		}
		if !before[i] || !after[i] {
			return fail("C1", "ZERO WIDTH NON-JOINER outside a joining context")
		}
	}
	return nil
}

// joiningContext reports, for each position, whether the nearest preceding
// non-transparent code point has Joining_Type L or D, and whether the nearest
// following one has Joining_Type R or D.
func joiningContext(l []rune) (before, after []bool) {
	before, after = make([]bool, len(l)), make([]bool, len(l))
	joins := false
	for i, r := range l {
		before[i] = joins
		if t := joiningType(r); t != joiningT {
			joins = t == joiningL || t == joiningD
		}
	}
	joins = false
	for i := len(l) - 1; i >= 0; i-- {
		after[i] = joins
		if t := joiningType(l[i]); t != joiningT {
			joins = t == joiningR || t == joiningD
		}
	}
	return before, after
}

// checkBidi applies the six conditions of the RFC 5893 bidi rule.
func checkBidi(l []rune) *Error {
	rtl := false
	switch bidiClass(l[0]) {
	case bidiR, bidiAL:
		rtl = true
	case bidiL:
	default:
		return fail("B1", "bidi label does not begin with a letter of a strong direction")
	}
	en, an := false, false
	for _, r := range l {
		c := bidiClass(r)
		en, an = en || c == bidiEN, an || c == bidiAN
		switch {
		case rtl && c != bidiR && c != bidiAL && c != bidiAN && !neutral(c):
			return fail("B2", "right-to-left label contains a disallowed bidi class")
		case !rtl && c != bidiL && !neutral(c):
			return fail("B5", "left-to-right label contains a disallowed bidi class")
		}
	}
	end := len(l) - 1
	for bidiClass(l[end]) == bidiNSM {
		end--
	}
	switch c := bidiClass(l[end]); {
	case rtl && c != bidiR && c != bidiAL && c != bidiEN && c != bidiAN:
		return fail("B3", "right-to-left label ends with a disallowed bidi class")
	case rtl && en && an:
		return fail("B4", "right-to-left label mixes European and Arabic digits")
	case !rtl && c != bidiL && c != bidiEN:
		return fail("B6", "left-to-right label ends with a disallowed bidi class")
	}
	return nil
}

// neutral reports the classes RFC 5893 allows in labels of either direction.
func neutral(c uint8) bool {
	switch c {
	case bidiEN, bidiES, bidiCS, bidiET, bidiON, bidiBN, bidiNSM:
		return true
	}
	return false
}

func joinUnicode(labels []label) string {
	parts := make([]string, len(labels))
	for i, l := range labels {
		parts[i] = string(l.text)
	}
	return strings.Join(parts, ".")
}

func joinASCII(labels []label) string {
	parts := make([]string, len(labels))
	for i, l := range labels {
		parts[i] = l.ascii
	}
	return strings.Join(parts, ".")
}
