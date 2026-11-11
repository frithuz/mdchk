package idna

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCanonicalize(t *testing.T) {
	label63 := strings.Repeat("a", 63)
	host253 := strings.Join([]string{label63, label63, label63, strings.Repeat("b", 61)}, ".")
	for _, tc := range []struct{ in, ascii, unicode string }{
		{"Faß.de", "xn--fa-hia.de", "faß.de"},
		{"xn--fa-hia.de", "xn--fa-hia.de", "faß.de"},
		{"XN--FA-HIA.DE.", "xn--fa-hia.de", "faß.de"},
		{"faß.de。", "xn--fa-hia.de", "faß.de"},
		{"\u0430pple.com", "xn--pple-43d.com", "\u0430pple.com"},
		{"\u0410PPLE.com", "xn--pple-43d.com", "\u0430pple.com"},
		{"xn--pple-43d.com", "xn--pple-43d.com", "\u0430pple.com"},
		{"Xn--Pple-43D.Com.", "xn--pple-43d.com", "\u0430pple.com"},
		{"apple.com", "apple.com", "apple.com"},
		{"ｅｘａｍｐｌｅ.com", "example.com", "example.com"},
		{"example.com.", "example.com", "example.com"},
		{"Example.COM", "example.com", "example.com"},
		{"192.0.2.1", "192.0.2.1", "192.0.2.1"},
		{label63 + ".com", label63 + ".com", label63 + ".com"},
		{host253, host253, host253},
		{host253 + ".", host253, host253},
	} {
		got, err := Canonicalize(tc.in)
		if err != nil || got != (Host{ASCII: tc.ascii, Unicode: tc.unicode}) {
			t.Errorf("Canonicalize(%+q) = %+q, %v; want %q, %q", tc.in, got, err, tc.ascii, tc.unicode)
		}
		if err := ValidateHostname(tc.in); err != nil {
			t.Errorf("ValidateHostname(%+q) = %v", tc.in, err)
		}
	}
	for _, tc := range []struct{ in, code string }{
		{"", "A4_2"},
		{".", "A4_2"},
		{"。", "A4_2"},
		{"example.com..", "A4_2"},
		{".example.com", "A4_2"},
		{"a..b", "A4_2"},
		{"-a.com", "V3"},
		{"a-.com", "V3"},
		{"ab--c.com", "V2"},
		{"xn--a.com", "V7"},
		{"xn--fa-hia-.de", "P4"},
		{"xn--ASCII-.com", "P4"},
		{"a\u200dz.com", "C2"},
		{"example.com:443", "U1"},
		{"https://example.com", "U1"},
		{"*.example.com", "U1"},
		{"user@example.com", "U1"},
		{"[::1]", "U1"},
		{"example.com/path", "U1"},
		{"exa%6dple.com", "U1"},
		{" example.com", "U1"},
		{"example.com ", "U1"},
		{"example.com\t", "U1"},
		{"a\u0300\u05d0.com", "B5"},
		{label63 + "a.com", "A4_2"},
		{"xn--" + label63 + ".com", "A4_2"},
		{host253 + "b", "A4_1"},
		{"\xff.com", "V7"},
	} {
		_, err := Canonicalize(tc.in)
		var e *Error
		if !errors.As(err, &e) || e.Code != tc.code {
			t.Errorf("Canonicalize(%+q) error = %v; want *Error %s", tc.in, err, tc.code)
		}
		if ValidateHostname(tc.in) == nil {
			t.Errorf("ValidateHostname(%+q) accepted", tc.in)
		}
	}
}

func TestCanonicalizeErrorText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"-a.com", "label 0: leading hyphen (V3)"},
		{"example.com:443", "label 1: ASCII character other than a-z, 0-9 or hyphen (U1)"},
		{"a\x00\x1b[31m.com", "label 0: ASCII character other than a-z, 0-9 or hyphen (U1)"},
		{strings.Repeat("a.", 127) + "a", "name longer than 253 octets (A4_1)"},
	} {
		if _, err := Canonicalize(tc.in); err == nil || err.Error() != tc.want {
			t.Errorf("Canonicalize(%+q) error = %v; want %q", tc.in, err, tc.want)
		}
	}
}

func TestCanonicalizeLargeInput(t *testing.T) {
	for _, in := range []string{
		strings.Repeat("a", 1<<20),
		"xn--" + strings.Repeat("a", 1<<20),
		"a" + strings.Repeat("\u0300", 1<<19),
		strings.Repeat("a.", 1<<19),
	} {
		start := time.Now()
		if _, err := Canonicalize(in); err == nil {
			t.Errorf("accepted %d-byte input", len(in))
		}
		if _, err := ToUnicode(in); err == nil && strings.HasPrefix(in, "xn--") {
			t.Errorf("ToUnicode accepted %d-byte A-label", len(in))
		}
		// The bound guards against quadratic work; race instrumentation alone
		// can exceed it, so only the rejections are checked under -race.
		if d := time.Since(start); !raceEnabled && d > 10*time.Second {
			t.Errorf("%d-byte input took %v", len(in), d)
		}
	}
}

func TestPunycodeRoundTrip(t *testing.T) {
	for _, tc := range []struct{ unicode, encoded string }{
		{"faß", "fa-hia"},
		{"\u0430pple", "pple-43d"},
		{"bücher", "bcher-kva"},
		{"日本語", "wgv71a119e"},
		{"ü", "tda"},
	} {
		if got := encodePunycode([]rune(tc.unicode)); got != tc.encoded {
			t.Errorf("encode(%q) = %q; want %q", tc.unicode, got, tc.encoded)
		}
		if got, err := decodePunycode(tc.encoded); err != nil || string(got) != tc.unicode {
			t.Errorf("decode(%q) = %q, %v; want %q", tc.encoded, string(got), err, tc.unicode)
		}
	}
}
