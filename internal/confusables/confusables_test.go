package confusables

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// deriveASCIILetters independently re-reads the retained file's direct
// mappings and keeps those whose target is solely ASCII letters.
func deriveASCIILetters(t *testing.T) map[rune]string {
	t.Helper()
	data, err := os.ReadFile("testdata/unicode/17.0.0/confusables.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 745683 || fmt.Sprintf("%x", sha256.Sum256(data)) != "091c7f82fc39ef208faf8f94d29c244de99254675e09de163160c810d13ef22a" {
		t.Fatal("pinned confusables.txt identity")
	}
	out := map[rune]string{}
	records := 0
	for _, line := range strings.Split(string(data), "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Split(line, ";")
		if len(fields) < 3 {
			continue
		}
		records++
		source, err := strconv.ParseInt(strings.TrimSpace(fields[0]), 16, 32)
		if err != nil {
			t.Fatal(err)
		}
		var target strings.Builder
		letters := true
		for _, f := range strings.Fields(fields[1]) {
			c, err := strconv.ParseInt(f, 16, 32)
			if err != nil {
				t.Fatal(err)
			}
			letters = letters && (c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z')
			target.WriteRune(rune(c))
		}
		if letters && target.Len() > 0 {
			out[rune(source)] = target.String()
		}
	}
	if records != 6565 {
		t.Fatalf("records %d", records)
	}
	return out
}

func TestGeneratedTableMatchesRetainedConfusables(t *testing.T) {
	want := deriveASCIILetters(t)
	if len(asciiLetters) != len(want) {
		t.Fatalf("table has %d entries; want %d", len(asciiLetters), len(want))
	}
	for i, e := range asciiLetters {
		if i > 0 && asciiLetters[i-1].source >= e.source {
			t.Fatalf("U+%04X out of order", e.source)
		}
		if want[e.source] != e.mapping {
			t.Fatalf("U+%04X: got %q; want %q", e.source, e.mapping, want[e.source])
		}
	}
	for r, mapping := range want {
		if got, ok := ASCIILetters(r); !ok || got != mapping {
			t.Fatalf("ASCIILetters(U+%04X) = %q, %v; want %q", r, got, ok, mapping)
		}
	}
	for r := rune(0); r <= 0x10FFFF; r++ {
		if _, ok := ASCIILetters(r); ok != (want[r] != "") {
			t.Fatalf("ASCIILetters(U+%04X) presence %v", r, ok)
		}
	}
}

func TestASCIILetters(t *testing.T) {
	for _, tc := range []struct {
		r       rune
		mapping string
	}{
		{0x0430, "a"},  // CYRILLIC SMALL LETTER A
		{0x0261, "g"},  // LATIN SMALL LETTER SCRIPT G
		{0x01C9, "lj"}, // LATIN SMALL LETTER LJ
		{0x217F, "rn"}, // SMALL ROMAN NUMERAL ONE THOUSAND, a direct mapping only
	} {
		if got, ok := ASCIILetters(tc.r); !ok || got != tc.mapping {
			t.Fatalf("U+%04X: %q %v", tc.r, got, ok)
		}
	}
	// Direct mappings to a digit, punctuation, or a letter with punctuation.
	for _, r := range []rune{0x1D7D0, 0x2010, 0x2488, 'a', 0x00E9} {
		if got, ok := ASCIILetters(r); ok {
			t.Fatalf("U+%04X unexpectedly maps to %q", r, got)
		}
	}
	if Version != "17.0.0" {
		t.Fatal(Version)
	}
}
