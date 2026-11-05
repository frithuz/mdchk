package parser

import (
	"context"
	"reflect"
	"testing"
)

func TestSuspiciousSequencesMatching(t *testing.T) {
	defaults := []string{"Ã", "Â", "â€"}
	for _, tc := range []struct {
		name, source string
		sequences    []string
		want         [][2]int
	}{
		{"inside-word", "cafÃ©", defaults, [][2]int{{3, 5}}},
		{"word-start", "Ãa", defaults, [][2]int{{0, 2}}},
		{"word-end", "aÃ", defaults, [][2]int{{1, 3}}},
		{"word-neighbors", "aÃb", defaults, [][2]int{{1, 3}}},
		{"accepted", "café", defaults, nil},
		{"case-sensitive", "cafã©", defaults, nil},
		{"each-default", "Â© donâ€™t Ã", defaults, [][2]int{{0, 2}, {8, 13}, {18, 20}}},
		{"longest", "cafÃ©", []string{"Ã", "Ã©"}, [][2]int{{3, 7}}},
		{"longest-reverse", "cafÃ©", []string{"Ã©", "Ã"}, [][2]int{{3, 7}}},
		{"nested", "â€™", []string{"€", "â€", "â€™"}, [][2]int{{0, 8}}},
		{"leftmost-consumed", "abc", []string{"ab", "bc"}, [][2]int{{0, 2}}},
		{"adjacent", "ÃÃ", defaults, [][2]int{{0, 2}, {2, 4}}},
		{"duplicates", "Ã", []string{"Ã", "Ã"}, [][2]int{{0, 2}}},
		{"several", "Ã x Ã", defaults, [][2]int{{0, 2}, {5, 7}}},
		{"no-sequences", "cafÃ©", nil, nil},
		{"empty-sequences", "cafÃ©", []string{}, nil},
		{"empty-entry", "cafÃ©", []string{""}, nil},
		{"empty-ignored", "cafÃ©", []string{"", "Ã"}, [][2]int{{3, 5}}},
		{"no-cross-line", "a\nb", []string{"a\nb"}, nil},
		{"fenced-code", "~~~\nÃ\n~~~", defaults, nil},
		{"indented-code", "    Ã", defaults, nil},
		{"code-span", "`Ã`", defaults, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, b, index := sectionParse(t, tc.source)
			got, err := d.SuspiciousSequences(context.Background(), b, index, ParseOptions{}, tc.sequences)
			if err != nil {
				t.Fatal(err)
			}
			var ranges [][2]int
			for _, s := range got {
				ranges = append(ranges, [2]int{s.start, s.end})
			}
			if !reflect.DeepEqual(ranges, tc.want) {
				t.Fatal(ranges, tc.want)
			}
		})
	}
}

func TestSuspiciousSequencesUTF8(t *testing.T) {
	d, b, index := sectionParse(t, "cafÃ©")
	if got, err := d.SuspiciousSequences(context.Background(), b, index, ParseOptions{}, []string{"Ã", string([]byte{255})}); err == nil || got != nil {
		t.Fatal(got, err)
	}
}
