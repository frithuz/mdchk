package parser

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestProhibitedTermsMatching(t *testing.T) {
	for _, tc := range []struct {
		name, source  string
		terms         []string
		caseSensitive bool
		want          [][2]int
	}{
		{"insensitive", "Use BlackList.", []string{"blacklist"}, false, [][2]int{{4, 13}}},
		{"insensitive-full-fold", "Use STRASSE.", []string{"straße"}, false, [][2]int{{4, 11}}},
		{"sensitive-miss", "Use BlackList.", []string{"blacklist"}, true, nil},
		{"sensitive-hit", "Use blacklist.", []string{"blacklist"}, true, [][2]int{{4, 13}}},
		{"sensitive-no-fold", "Use STRASSE.", []string{"straße"}, true, nil},
		{"exact-spelling-reported", "Use blacklist.", []string{"blacklist"}, false, [][2]int{{4, 13}}},
		{"no-terms", "Use blacklist.", nil, false, nil},
		{"empty-terms", "Use blacklist.", []string{"", ""}, false, nil},
		{"empty-ignored", "Use blacklist.", []string{"", "blacklist"}, true, [][2]int{{4, 13}}},
		{"longest", "Use blacklist.", []string{"black", "blacklist"}, false, [][2]int{{4, 13}}},
		{"longest-reverse", "Use blacklist.", []string{"blacklist", "black"}, false, [][2]int{{4, 13}}},
		{"leftmost-consumed", "black list item", []string{"black list", "list item"}, false, [][2]int{{0, 10}}},
		{"fold-equal-duplicates", "blacklist", []string{"blacklist", "BlackList"}, false, [][2]int{{0, 9}}},
		{"word-suffix", "blacklisted", []string{"blacklist"}, false, nil},
		{"word-prefix", "myblacklist", []string{"blacklist"}, false, nil},
		{"word-underscore", "_blacklist", []string{"blacklist"}, false, nil},
		{"word-mark", "blacklist́", []string{"blacklist"}, false, nil},
		{"word-punctuation", "(blacklist)", []string{"blacklist"}, false, [][2]int{{1, 10}}},
		{"fenced-code", "~~~\nblacklist\n~~~", []string{"blacklist"}, false, nil},
		{"indented-code", "    blacklist", []string{"blacklist"}, false, nil},
		{"code-span", "`blacklist`", []string{"blacklist"}, false, nil},
		{"several", "blacklist blacklist", []string{"blacklist"}, false, [][2]int{{0, 9}, {10, 19}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, b, index := sectionParse(t, tc.source)
			got, err := d.ProhibitedTerms(context.Background(), b, index, ParseOptions{}, tc.terms, tc.caseSensitive)
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

func TestProhibitedTermsOriginalAuthorityAndUTF8(t *testing.T) {
	d, b, _ := sectionParse(t, "blacklist")
	if got, err := d.ProhibitedTerms(context.Background(), b, NewLineIndex(NewSource([]byte("blacklist"))), ParseOptions{}, []string{"blacklist"}, false); !errors.Is(err, ErrModel) || got != nil {
		t.Fatal(got, err)
	}
	for _, caseSensitive := range []bool{false, true} {
		d, b, index := sectionParse(t, "blacklist")
		if got, err := d.ProhibitedTerms(context.Background(), b, index, ParseOptions{}, []string{string([]byte{255})}, caseSensitive); err == nil || got != nil {
			t.Fatal(got, err)
		}
	}
}
