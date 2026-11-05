package idna

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

func parsePoints(t *testing.T, field string) []rune {
	t.Helper()
	var out []rune
	for _, f := range strings.Fields(field) {
		v, err := strconv.ParseInt(f, 16, 32)
		if err != nil {
			t.Fatalf("code point %q: %v", f, err)
		}
		out = append(out, rune(v))
	}
	return out
}

func dataRecords(text string) [][]string {
	var out [][]string
	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Split(line, ";")
		if len(fields) < 2 {
			continue
		}
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		out = append(out, fields)
	}
	return out
}

func TestGeneratedMappingMatchesIdnaMappingTable(t *testing.T) {
	statuses := map[string]status{"valid": statusValid, "mapped": statusMapped, "deviation": statusDeviation, "disallowed": statusDisallowed, "ignored": statusIgnored}
	next := rune(0)
	for _, fields := range dataRecords(readTestData(t, "IdnaMappingTable.txt")) {
		lo, hi, _ := strings.Cut(fields[0], "..")
		if hi == "" {
			hi = lo
		}
		bounds := parsePoints(t, lo+" "+hi)
		want, ok := statuses[fields[1]]
		if !ok || bounds[0] != next {
			t.Fatalf("unexpected record %q", fields)
		}
		var mapping string
		if len(fields) > 2 && (want == statusMapped || want == statusDeviation) {
			mapping = string(parsePoints(t, fields[2]))
		}
		for r := bounds[0]; r <= bounds[1]; r++ {
			if got := lookupMapping(r); got.status != want || got.mapping != mapping {
				t.Fatalf("U+%04X: got status %d mapping %+q; want %d %+q", r, got.status, got.mapping, want, mapping)
			}
		}
		next = bounds[1] + 1
	}
	if next != 0x110000 {
		t.Fatalf("mapping table ends at U+%04X", next)
	}
}

func TestNFCNormalizationTest(t *testing.T) {
	part1 := map[rune]bool{}
	part, lines := "", 0
	for _, line := range strings.Split(readTestData(t, "NormalizationTest.txt"), "\n") {
		if strings.HasPrefix(line, "@") {
			part = strings.Fields(line)[0]
			continue
		}
		fields := dataRecords(line)
		if len(fields) == 0 {
			continue
		}
		lines++
		c := make([][]rune, 5)
		for i := range c {
			c[i] = parsePoints(t, fields[0][i])
		}
		if part == "@Part1" {
			part1[c[0][0]] = true
		}
		for i, want := range []int{1, 1, 1, 3, 3} {
			if got := nfc(slices.Clone(c[i])); !slices.Equal(got, c[want]) {
				t.Fatalf("%s: NFC(c%d %U) = %U; want c%d %U", part, i+1, c[i], got, want+1, c[want])
			}
		}
	}
	if lines < 19000 {
		t.Fatalf("read only %d NormalizationTest.txt lines", lines)
	}
	// Every code point outside Part 1 is its own NFC.
	for r := rune(0); r <= 0x10FFFF; r++ {
		if r >= 0xD800 && r <= 0xDFFF || part1[r] {
			continue
		}
		if got := nfc([]rune{r}); len(got) != 1 || got[0] != r {
			t.Fatalf("NFC(%U) = %U", r, got)
		}
	}
}
