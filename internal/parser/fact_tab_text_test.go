package parser

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestInlineTextTabsSelectsInlineText(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   []int
	}{
		{"a\tb\n", []int{1}},
		{"a\t\tb\tc\n", []int{1, 2, 4}},
		{"# A\tB\n", []int{3}},
		{"- a\tb\n", []int{3}},
		{"> a\tb\n", []int{3}},
		{"[a\tb](u)\n", []int{2}},
		{"\ta\n", nil},
		{"-\ta\n", nil},
		{">\ta\n", nil},
		{"a\t\nb\n", nil},
		{"a `b\tc` d\n", nil},
		{"    a\tb\n", nil},
		{"[a](<b\tc> \"d\te\")\n", nil},
		{"[a]:\t/u\n", nil},
		{"![a\tb](i)\n", nil},
	} {
		d, b, index := sectionParse(t, tc.source)
		got, err := d.InlineTextTabs(context.Background(), b, index, ParseOptions{})
		if err != nil {
			t.Fatal(tc.source, err)
		}
		var starts []int
		for _, s := range got {
			starts = append(starts, s.Start())
		}
		if !reflect.DeepEqual(starts, tc.want) {
			t.Fatalf("%q: %v", tc.source, starts)
		}
	}
}

func TestInlineTextTabsDirectQuota(t *testing.T) {
	for _, source := range []string{"", "a\tb\n", strings.Repeat("a\tb\n", 256)} {
		d, b, index := sectionParse(t, source)
		b.limits.WorkUnits = b.work + 1
		var limit *LimitExceeded
		if got, err := d.InlineTextTabs(context.Background(), b, index, ParseOptions{}); !errors.As(err, &limit) || got != nil {
			t.Fatal(source, got, err)
		}
	}
}

func TestEqualAcrossTabs(t *testing.T) {
	for _, tc := range []struct {
		a, b   string
		spaces int
		want   bool
	}{
		{"a\tb", "a    b", 4, true},
		{"a\tb", "a b", 1, true},
		{"a\t\tb", "a        b", 4, true},
		{"a\tb", "a   b", 4, false},
		{"a\tb", "a     b", 4, false},
		{"a\tb", "a\tb", 4, true},
		{"a\tb", "a    c", 4, false},
		{"", "", 4, true},
		{"\t", "", 1, false},
	} {
		if got := equalAcrossTabs(tc.a, tc.b, tc.spaces); got != tc.want {
			t.Errorf("%q %q %d: %v", tc.a, tc.b, tc.spaces, got)
		}
	}
}
