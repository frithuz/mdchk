package parser

import (
	"context"
	"testing"
)

func TestSameMeaningAcrossLineEndWhitespace(t *testing.T) {
	for _, tc := range []struct {
		name, a, b string
		gfm, want  bool
	}{
		{name: "identical", a: "a\nb\n", b: "a\nb\n", want: true},
		{name: "space before soft break", a: "a \nb\n", b: "a\nb\n", want: true},
		{name: "tab before soft break", a: "a\t\nb\n", b: "a\nb\n", want: true},
		{name: "tab-only text before break", a: "*a*\t\nb\n", b: "*a*\nb\n", want: true},
		{name: "normalized hard break", a: "a\t  \nb\n", b: "a  \nb\n", want: true},
		{name: "added hard break", a: "a\nb\n", b: "a  \nb\n"},
		{name: "removed hard break", a: "a  \nb\n", b: "a\nb\n"},
		{name: "break origin", a: "a  \nb\n", b: "a\\\nb\n"},
		{name: "text", a: "a\nb\n", b: "c\nb\n"},
		{name: "kinds", a: "a\n", b: "# a\n"},
		{name: "heading level", a: "# a\n", b: "## a\n"},
		{name: "list tightness", a: "- a\n- b\n", b: "- a\n\n- b\n"},
		{name: "list marker", a: "- a\n", b: "* a\n"},
		{name: "code info", a: "```x\n```\n", b: "```y\n```\n"},
		{name: "link destination", a: "[a](b)\n", b: "[a](c)\n"},
		{name: "link title", a: "[a](b \"t \nu\")\n", b: "[a](b \"t\nu\")\n"},
		{name: "image title", a: "![a](b \"t \nu\")\n", b: "![a](b \"t\nu\")\n"},
		{name: "reference title", a: "[a]: b \"t \nu\"\n", b: "[a]: b \"t\nu\"\n"},
		{name: "reference label", a: "[a]: b\n", b: "[c]: b\n"},
		{name: "reference count", a: "[a]: b\n", b: "[a]: b\n[c]: d\n"},
		{name: "reference label spacing", a: "[a \nb]: c\n\n[a b]\n", b: "[a\nb]: c\n\n[a b]\n", want: true},
		{name: "backslash break created", a: "a\\ \nb\n", b: "a\\\nb\n"},
		{name: "task checked", gfm: true, a: "- [ ] a\n", b: "- [x] a\n"},
		{name: "task lost", gfm: true, a: "- [ ] a\n", b: "- [ a\n"},
		{name: "task separator", gfm: true, a: "- [ ] \n", b: "- [ ]\n", want: true},
		{name: "table alignment", gfm: true, a: "|a|\n|-|\n", b: "|a|\n|:-|\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			budget, err := NewOperationBudget(DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			parse := func(text string) *Document {
				r := ParseWithOptions(context.Background(), NewSource([]byte(text)), budget, ParseOptions{GFM: tc.gfm})
				if r.Document == nil || r.Status != Complete {
					t.Fatal(text, r.Status)
				}
				return r.Document
			}
			got, err := parse(tc.a).SameMeaningAcrossLineEndWhitespace(context.Background(), budget, parse(tc.b))
			if err != nil || got != tc.want {
				t.Fatal(got, err)
			}
		})
	}
}

func TestSameMeaningRequiresCompletedParsesInThisOperation(t *testing.T) {
	budget, _ := NewOperationBudget(DefaultLimits())
	foreign, _ := NewOperationBudget(DefaultLimits())
	a := ParseWithOptions(context.Background(), NewSource([]byte("a\n")), budget, ParseOptions{}).Document
	if _, err := a.SameMeaningAcrossLineEndWhitespace(context.Background(), foreign, a); err == nil {
		t.Fatal("foreign operation accepted")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.SameMeaningAcrossLineEndWhitespace(canceled, budget, a); err == nil {
		t.Fatal("canceled context accepted")
	}
}

func TestTrailingSuffixFacts(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		gfm, code    bool
		spaces       int
		want         TrailingSuffix
	}{
		{name: "plain", source: "a \n", want: TrailingSuffix{}},
		{name: "hard", source: "a   \nb\n", want: TrailingSuffix{HardBreak: true}},
		{name: "mixed hard", source: "a\t  \nb\n", want: TrailingSuffix{HardBreak: true}},
		{name: "not a break", source: "a  \t\nb\n", want: TrailingSuffix{}},
		{name: "eof", source: "a ", want: TrailingSuffix{EOF: true}},
		{name: "literal", source: "```\na \n```\n", code: true, want: TrailingSuffix{Literal: true}},
		{name: "odd backslash", source: "a\\ \nb\n", want: TrailingSuffix{Withheld: true}},
		{name: "even backslash", source: "a\\\\ \nb\n", want: TrailingSuffix{}},
		{name: "backslash hard", source: "a\\   \nb\n", want: TrailingSuffix{HardBreak: true}},
		{name: "link title", source: "[a](b \"t \nu\")\n", want: TrailingSuffix{Withheld: true}},
		{name: "after link title", source: "[a](b \"t\") \nc\n", want: TrailingSuffix{}},
		{name: "before link title", source: "[a](b \n\"t\")\n", want: TrailingSuffix{}},
		{name: "definition title", source: "[a]: b \"t \nu\"\n", want: TrailingSuffix{Withheld: true}},
		{name: "eof task", gfm: true, source: "- [x] ", want: TrailingSuffix{Withheld: true, EOF: true}},
		{name: "eof task content", gfm: true, source: "- [x] a ", want: TrailingSuffix{EOF: true}},
		{name: "core eof", source: "- [x] ", want: TrailingSuffix{EOF: true}},
		{name: "line task", gfm: true, source: "- [x] \n- b\n", want: TrailingSuffix{}},
		{name: "task continuation", gfm: true, source: "- [x] \n  b\n", want: TrailingSuffix{Withheld: true}},
		{name: "grow at limit", source: "a  \nb\n", spaces: MaxGrownHardBreakSpaces, want: TrailingSuffix{HardBreak: true}},
		{name: "grow past limit", source: "a  \nb\n", spaces: MaxGrownHardBreakSpaces + 1, want: TrailingSuffix{HardBreak: true, Oversized: true}},
		{name: "shrink past limit", source: "a          \nb\n", spaces: MaxGrownHardBreakSpaces + 1, want: TrailingSuffix{HardBreak: true}},
		{name: "removal ignores limit", source: "a \n\nb\n", spaces: MaxGrownHardBreakSpaces + 1, want: TrailingSuffix{}},
		{name: "core task continuation", source: "- [x] \n  b\n", want: TrailingSuffix{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			budget, _ := NewOperationBudget(DefaultLimits())
			source := NewSource([]byte(tc.source))
			options := ParseOptions{GFM: tc.gfm}
			r := ParseWithOptions(context.Background(), source, budget, options)
			if tc.spaces == 0 {
				tc.spaces = 2
			}
			got, err := r.Document.TrailingWhitespace(context.Background(), budget, NewLineIndex(source), options, tc.spaces, false, tc.code)
			if err != nil || len(got) != 1 {
				t.Fatal(got, err)
			}
			got[0].Span = Span{}
			if got[0] != tc.want {
				t.Fatalf("%+v", got[0])
			}
		})
	}
}

func TestSameMeaningAcrossListSeparators(t *testing.T) {
	for _, tc := range []struct {
		name, a, b string
		markers    []int
		want       bool
	}{
		{"owning list retightens", "- a\n- b\n", "- a\n\n- b\n", []int{4}, true},
		{"unlisted list retightens", "- a\n- b\n", "- a\n\n- b\n", nil, false},
		{"other list retightens", "- a\n- b\n\n* c\n* d\n", "- a\n- b\n\n* c\n\n* d\n", []int{4}, false},
		{"nested owner only", "- a\n  - x\n  - y\n", "- a\n  - x\n\n  - y\n", []int{12}, true},
		{"text changes", "- a\n- b\n", "- a\n- c\n", []int{4}, false},
		{"trailing break whitespace is exact", "a  \nb\n", "a\nb\n", nil, false},
		{"item count changes", "- a\n- b\n", "- a\n- b\n- c\n", []int{4}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			budget, err := NewOperationBudget(DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			parse := func(text string) *Document {
				r := ParseWithOptions(context.Background(), NewSource([]byte(text)), budget, ParseOptions{})
				if r.Document == nil || r.Status != Complete {
					t.Fatal(text, r.Status)
				}
				return r.Document
			}
			markers := map[int]bool{}
			for _, m := range tc.markers {
				markers[m] = true
			}
			got, err := parse(tc.a).SameMeaningAcrossListSeparators(context.Background(), budget, parse(tc.b), markers)
			if err != nil || got != tc.want {
				t.Fatal(got, err)
			}
		})
	}
}
