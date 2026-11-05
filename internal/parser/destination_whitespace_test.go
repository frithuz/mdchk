package parser

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

const md062Example = "[Guide](my guide.md)"

func md062Positive(t *testing.T, gfm bool) {
	t.Helper()
	d, b, index, options := md011Parse(t, md062Example, gfm)
	got, err := d.DestinationWhitespace(context.Background(), b, index, options)
	if err != nil || got.Availability != Available || len(got.Spans) != 1 || got.Spans[0].start != 0 || got.Spans[0].end != 20 || got.Spans[0].SourceToken() != index.source.Token() {
		t.Fatal("actual original positive prerequisite", got, err)
	}
}

func TestDestinationWhitespaceCandidates(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		gfmOnly      bool
		want         [][2]int
	}{
		{"page-bad", "[Guide](my guide.md)", false, [][2]int{{0, 20}}},
		{"page-good", "[Guide](my%20guide.md)", false, nil},
		{"image", "![Logo](my logo.png)", false, [][2]int{{0, 20}}},
		{"tab", "[a](b\tc)", false, [][2]int{{0, 8}}},
		{"long-run", "[a](b   c)", false, [][2]int{{0, 10}}},
		{"padded", "[a]( b c )", false, [][2]int{{0, 10}}},
		{"empty-label", "[](b c)", false, [][2]int{{0, 7}}},
		{"blank-label", "[ ](b c)", false, [][2]int{{0, 8}}},
		{"valid-link", `[a](b "t")`, false, nil},
		{"valid-padded", "[a]( b )", false, nil},
		{"valid-angle", "[a](<my file>)", false, nil},
		{"angle", "[a](<my file> x)", false, nil},
		{"broken-double-title", `[a](my "broken)`, false, nil},
		{"broken-single-title", "[a](my 'broken)", false, nil},
		{"backslash", `[a](my\ file)`, false, nil},
		{"backslash-later-run", `[a](my\ file x)`, false, [][2]int{{0, 15}}},
		{"entity-space", "[a](my&#32;file)", false, nil},
		{"entity-tab", "[a](my&#9;file)", false, nil},
		{"percent", "[a](my%20file)", false, nil},
		{"unicode-space", "[a](my file)", false, nil},
		{"nested", "[a]([b](c d))", false, nil},
		{"nested-label", "[a [b]](c d)", false, nil},
		{"after-paren", "x)[a](b c)", false, nil},
		{"after-close", "x][a](b c)", false, nil},
		{"after-link", "[x](y)[a](b c)", false, nil},
		{"after-image", "![x](y)[a](b c)", false, nil},
		{"after-link-space", "[x](y) [a](b c)", false, [][2]int{{7, 15}}},
		{"chain-paren", "[a](b c)(d)", false, nil},
		{"chain-bracket", "[a](b c)[d]", false, nil},
		{"before-close-paren", "[a](b c))", false, nil},
		{"before-close-bracket", "[a](b c)]", false, nil},
		{"escaped-bang", `\![a](b c)`, false, [][2]int{{2, 10}}},
		{"escaped-open", `\[a](b c)`, false, nil},
		{"escaped-close", `[a\](b c)`, false, nil},
		{"escaped-paren", `[a]\(b c)`, false, nil},
		{"escaped-final", `[a](b c\)`, false, nil},
		{"entity-bracket", "[a&#93;(b c)", false, nil},
		{"spaced", "[a] (b c)", false, nil},
		{"defined-label", "[a](b c)\n\n[a]: u", false, nil},
		{"two", "[a](b c) and [d](e f)", false, [][2]int{{0, 8}, {13, 21}}},
		{"line-break", "[a](b\nc d)", false, nil},
		{"emphasis", "*[a](b c)*", false, [][2]int{{1, 9}}},
		{"emphasis-label", "[*a*](b c)", false, nil},
		{"emphasis-destination", "[a](*b* c)", false, nil},
		{"code-span-destination", "[a](`b` c)", false, nil},
		{"code-span", "`[a](b c)`", false, nil},
		{"fenced", "```\n[a](b c)\n```", false, nil},
		{"indented", "    [a](b c)", false, nil},
		{"html-block", "<div>\n[a](b c)\n</div>", false, nil},
		{"inline-html", `x <span title="[a](b c)">y</span>`, false, nil},
		{"link-text", "[x [a](b c) y](u)", false, nil},
		{"block-quote", "> [a](b c)", false, [][2]int{{2, 10}}},
		{"list-item", "- [a](b c)", false, [][2]int{{2, 10}}},
		{"quoted-list", "> - [a](b c)", false, [][2]int{{4, 12}}},
		{"heading", "# [a](b c)", false, [][2]int{{2, 10}}},
		{"multibyte-prefix", "é [a](b c)", false, [][2]int{{3, 11}}},
		{"multibyte-label", "[é](b c)", false, [][2]int{{0, 9}}},
		{"crlf", "x\r\n[a](b c)\r\n", false, [][2]int{{3, 11}}},
		{"table", "| h |\n| --- |\n| [a](b c) |", true, [][2]int{{16, 24}}},
	} {
		for _, gfm := range []bool{false, true} {
			if tc.gfmOnly && !gfm {
				continue
			}
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, tc.name), func(t *testing.T) {
				d, b, index, o := md011Parse(t, tc.source, gfm)
				got, e := d.DestinationWhitespace(context.Background(), b, index, o)
				if e != nil || got.Availability != Available || len(got.Spans) != len(tc.want) {
					t.Fatal(got, e)
				}
				for i, s := range got.Spans {
					if s.start != tc.want[i][0] || s.end != tc.want[i][1] || s.SourceToken() != index.source.Token() {
						t.Fatal(i, s, tc.want[i])
					}
				}
			})
		}
	}
}

func TestDestinationWhitespaceFrontmatterIsNotScanned(t *testing.T) {
	source := NewSource([]byte("---\ntitle: [a](b c)\n---\n[d](e f)"))
	b, _ := NewOperationBudget(DefaultLimits())
	f, e := NewFrontmatterOptions(true, nil)
	if e != nil {
		t.Fatal(e)
	}
	o := ParseOptions{Frontmatter: f}
	p := ParseWithOptions(context.Background(), source, b, o)
	if _, recognized := p.Document.Frontmatter(); p.Status != Complete || !recognized {
		t.Fatal(p)
	}
	got, e := p.Document.DestinationWhitespace(context.Background(), b, NewLineIndex(source), o)
	if e != nil || got.Availability != Available || len(got.Spans) != 1 || got.Spans[0].start != 24 || got.Spans[0].end != 32 {
		t.Fatal(got, e)
	}
}

func TestDestinationWhitespaceUnknownRawCutoff(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, tc := range []struct {
			name, source string
			known        bool
			count        int
		}{
			{"unknown-after-last", md062Example + "\n\n<div>\n<x title=\"", true, 1},
			{"unknown-before-later", md062Example + "\n\n<div>\n<x title=\"\n\n" + md062Example, false, 0},
			{"unknown-before-quiet-shape", md062Example + "\n\n<div>\n<x title=\"\n\n[a](<b)", false, 0},
			{"unknown-before-parsed-link", md062Example + "\n\n<div>\n<x title=\"\n\n[a](b)", true, 1},
			{"unknown-before-unrelated", md062Example + "\n\n<div>\n<x title=\"\n\n[a] (b)", true, 1},
			{"opaque-image-raw", "![<pre>](image.png) " + md062Example, true, 1},
		} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, tc.name), func(t *testing.T) {
				md062Positive(t, gfm)
				d, b, i, o := md011Parse(t, tc.source, gfm)
				got, e := d.DestinationWhitespace(context.Background(), b, i, o)
				want := Unavailable
				if tc.known {
					want = Available
				}
				if e != nil || got.Availability != want || len(got.Spans) != tc.count {
					t.Fatal(got, e)
				}
			})
		}
	}
}

func TestDestinationWhitespaceLateTextProvenance(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, fault := range []string{"unchanged", "literal", "unavailable", "foreign", "exact-mismatch", "normalized"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, fault), func(t *testing.T) {
				d, b, index, o := md011Parse(t, md062Example+"\n\n"+md062Example, gfm)
				nodes := md070Nodes(d, Text)
				if len(nodes) != 2 || len(nodes[1].spec.Content) == 0 {
					t.Fatal("original Text ownership", nodes)
				}
				n := nodes[1]
				switch fault {
				case "literal":
					n.spec.Properties.Literal.Present = false
				case "unavailable":
					n.spec.Content[0].Transformation = MappingUnavailable
				case "foreign":
					n.spec.Content[0].Source.source = NewSource(index.source.Bytes())
				case "exact-mismatch":
					n.spec.Content[0].Semantic.Value = "wrong"
				case "normalized":
					n.spec.Content[0].Transformation = Normalized
				}
				got, e := d.DestinationWhitespace(context.Background(), b, index, o)
				switch fault {
				case "literal", "unavailable":
					if e != nil || got.Availability != Unavailable || got.Spans != nil {
						t.Fatal(got, e)
					}
				case "foreign", "exact-mismatch":
					if !errors.Is(e, ErrModel) || got.Spans != nil || got.Availability != "" {
						t.Fatal(got, e)
					}
				default:
					want := 1
					if fault == "unchanged" {
						want = 2
					}
					if e != nil || got.Availability != Available || len(got.Spans) != want || got.Spans[0].start != 0 || got.Spans[0].end != 20 {
						t.Fatal(got, e)
					}
				}
			})
		}
	}
}

func TestDestinationWhitespaceRepeatedConcurrentDetachedObservation(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		d, b, i, o := md011Parse(t, md062Example, gfm)
		before, e := d.DestinationWhitespace(context.Background(), b, i, o)
		if e != nil || len(before.Spans) != 1 {
			t.Fatal(before, e)
		}
		root, token := d.root, d.Source().Token()
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				got, e := d.DestinationWhitespace(context.Background(), b, i, o)
				if e != nil || !reflect.DeepEqual(got, before) {
					t.Error(got, e)
				}
				if len(got.Spans) > 0 {
					got.Spans[0] = Span{}
				}
			})
		}
		wg.Wait()
		if d.root != root || d.Source().Token() != token || d.Source().content != md062Example {
			t.Fatal("original source/tree changed")
		}
	}
}

func TestDestinationWhitespaceBudgetAndCancellation(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		t.Run(fmt.Sprintf("gfm-%v", gfm), func(t *testing.T) {
			md062Positive(t, gfm)
			d, b, i, o := md011Parse(t, md062Example, gfm)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if got, e := d.DestinationWhitespace(ctx, b, i, o); !errors.Is(e, context.Canceled) || got.Spans != nil || got.Availability != "" {
				t.Fatal(got, e)
			}
			if e := b.ChargeWork(b.Limits().WorkUnits - b.work); e != nil {
				t.Fatal(e)
			}
			var stop *LimitExceeded
			for range 2 {
				if got, e := d.DestinationWhitespace(context.Background(), b, i, o); !errors.As(e, &stop) || got.Spans != nil || got.Availability != "" {
					t.Fatal(got, e)
				}
			}
		})
	}
}

func TestDestinationWhitespaceManyCandidatesAndUnmatchedPrefixWithinSourceBound(t *testing.T) {
	const count = 6000
	for _, gfm := range []bool{false, true} {
		for _, shape := range []string{"many", "unmatched"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, shape), func(t *testing.T) {
				md062Positive(t, gfm)
				text := strings.Repeat("[a](b c) ", count)
				want := count
				if shape == "unmatched" {
					text = strings.Repeat("[", count) + "a](b c)"
					want = 0
				}
				source := NewSource([]byte(text))
				limits := DefaultLimits()
				limits.WorkUnits = 2048*int64(len(text)) + 1<<20
				if shape == "unmatched" && limits.WorkUnits >= 8*count*(count+1)/2 {
					t.Fatal("nonlinear allowance")
				}
				b, e := NewOperationBudget(limits)
				if e != nil {
					t.Fatal(e)
				}
				o := ParseOptions{GFM: gfm}
				p := ParseWithOptions(context.Background(), source, b, o)
				if p.Status != Complete || p.Document == nil {
					t.Fatal(p)
				}
				got, e := p.Document.DestinationWhitespace(context.Background(), b, NewLineIndex(source), o)
				if e != nil || got.Availability != Available || len(got.Spans) != want {
					t.Fatal(len(got.Spans), got.Availability, e)
				}
			})
		}
	}
}
