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

const md052Example = "[Guide][manual]"

func md052Positive(t *testing.T, gfm bool) {
	t.Helper()
	d, b, index, options := md011Parse(t, md052Example, gfm)
	got, err := d.UndefinedReferences(context.Background(), b, index, options)
	if err != nil || got.Availability != Available || len(got.Spans) != 1 || got.Spans[0].start != 0 || got.Spans[0].end != 15 || got.Spans[0].SourceToken() != index.source.Token() {
		t.Fatal("actual original positive prerequisite", got, err)
	}
}

func TestUndefinedReferencesCandidates(t *testing.T) {
	long := strings.Repeat("x", 1000)
	for _, tc := range []struct {
		name, source string
		gfmOnly      bool
		want         [][2]int
	}{
		{"full", "[Guide][manual]", false, [][2]int{{0, 15}}},
		{"defined", "[Guide][manual]\n\n[manual]: guide.md", false, nil},
		{"defined-before", "[manual]: guide.md\n\n[Guide][manual]", false, nil},
		{"collapsed", "[Guide][]", false, [][2]int{{0, 9}}},
		{"collapsed-defined", "[Guide][]\n\n[guide]: u", false, nil},
		{"image", "![Logo][logo]", false, [][2]int{{0, 13}}},
		{"collapsed-image", "![Logo][]", false, [][2]int{{0, 9}}},
		{"escaped-bang", `\![Logo][logo]`, false, [][2]int{{2, 14}}},
		{"folding-whitespace", "[Guide][MANUAL  x]\n\n[manual x]: u", false, nil},
		{"unicode-folding", "[a][ẞ]\n\n[ss]: u", false, nil},
		{"multibyte-prefix", "é [a][b]", false, [][2]int{{3, 9}}},
		{"two", "[a][b] and [c][]", false, [][2]int{{0, 6}, {11, 16}}},
		{"parenthesized-text", "[see (docs)][b]", false, [][2]int{{0, 15}}},
		{"escaped-label-bytes", `[a][b\*c]`, false, [][2]int{{0, 9}}},
		{"entity-label", "[a][b&amp;c]", false, [][2]int{{0, 12}}},
		{"bare-shortcut", "[text]", false, nil},
		{"escaped-open", `\[a][b]`, false, nil},
		{"escaped-first-close", `[a\][b]`, false, nil},
		{"escaped-label-close", `[a][b\]`, false, nil},
		{"entity-bracket", "[a&#93;[b]", false, nil},
		{"nested", "[a [b]][c]", false, nil},
		{"unmatched", "[a][b", false, nil},
		{"blank-collapsed", "[ ][]", false, nil},
		{"blank-full", "[a][ ]", false, nil},
		{"long-label", "[a][" + long + "]", false, nil},
		{"long-escapes", "[a][" + strings.Repeat(`\*`, 500) + "]", false, nil},
		{"chain", "[a][b][c]", false, nil},
		{"chain-inline", "[a][b](c)", false, nil},
		{"after-close", "x][a][b]", false, nil},
		{"after-paren", "x)[a][b]", false, nil},
		{"after-link", "[a](u)[b][c]", false, nil},
		{"after-image", "![a](u)[b][c]", false, nil},
		{"after-autolink", "<https://x.org>[b][c]", false, nil},
		{"after-link-space", "[a](u) [b][c]", false, [][2]int{{7, 13}}},
		{"line-break", "[a][b\nc]", false, nil},
		{"emphasis", "[*a*][b]", false, nil},
		{"code-span", "`[a][b]`", false, nil},
		{"fenced", "```\n[a][b]\n```", false, nil},
		{"indented", "    [a][b]", false, nil},
		{"html-block", "<div>\n[a][b]\n</div>", false, nil},
		{"inline-html", `x <span title="[a][b]">y</span>`, false, nil},
		{"link-text", "[x [a][b] y](u)", false, nil},
		{"image-alt", "![x [a][b] y](u)", false, nil},
		{"block-quote", "> [a][b]", false, [][2]int{{2, 8}}},
		{"list-item", "- [a][b]", false, [][2]int{{2, 8}}},
		{"quoted-list", "> - [a][b]", false, [][2]int{{4, 10}}},
		{"heading", "# [a][b]", false, [][2]int{{2, 8}}},
		{"quoted-defined-elsewhere", "> [a][b]\n\n[b]: u", false, nil},
		{"crlf", "x\r\n[a][b]\r\n", false, [][2]int{{3, 9}}},
		{"table", "| h |\n| --- |\n| [a][b] |", true, [][2]int{{16, 22}}},
		{"table-defined", "| h |\n| --- |\n| [a][b] |\n\n[b]: u", true, nil},
	} {
		for _, gfm := range []bool{false, true} {
			if tc.gfmOnly && !gfm {
				continue
			}
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, tc.name), func(t *testing.T) {
				d, b, index, o := md011Parse(t, tc.source, gfm)
				got, e := d.UndefinedReferences(context.Background(), b, index, o)
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

func TestUndefinedReferencesFrontmatterIsNotScanned(t *testing.T) {
	source := NewSource([]byte("---\ntitle: [a][b]\n---\n[c][d]"))
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
	got, e := p.Document.UndefinedReferences(context.Background(), b, NewLineIndex(source), o)
	if e != nil || got.Availability != Available || len(got.Spans) != 1 || got.Spans[0].start != 22 || got.Spans[0].end != 28 {
		t.Fatal(got, e)
	}
}

func TestUndefinedReferencesUnavailableDefinitions(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, fault := range []string{"collection", "lookup", "quick-path"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, fault), func(t *testing.T) {
				md052Positive(t, gfm)
				source := md052Example
				if fault == "quick-path" {
					source = "clean"
				}
				d, b, index, o := md011Parse(t, source, gfm)
				switch fault {
				case "collection", "quick-path":
					d.references = ReferenceCollection{Availability: Unavailable, LookupAvailability: Unavailable}
				case "lookup":
					d.references.LookupAvailability = Unavailable
					d.references.Lookup = nil
				}
				got, e := d.UndefinedReferences(context.Background(), b, index, o)
				if e != nil || got.Availability != Unavailable || got.Spans != nil {
					t.Fatal(got, e)
				}
			})
		}
	}
}

func TestUndefinedReferencesUnknownRawCutoff(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, tc := range []struct {
			name, source string
			known        bool
			count        int
		}{
			{"unknown-after-last", md052Example + "\n\n<div>\n<x title=\"", true, 1},
			{"unknown-before-later", md052Example + "\n\n<div>\n<x title=\"\n\n" + md052Example, false, 0},
			{"unknown-before-unrelated", md052Example + "\n\n<div>\n<x title=\"\n\n[a] b", true, 1},
			{"opaque-image-raw", "![<pre>](image.png) " + md052Example, true, 1},
		} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, tc.name), func(t *testing.T) {
				md052Positive(t, gfm)
				d, b, i, o := md011Parse(t, tc.source, gfm)
				got, e := d.UndefinedReferences(context.Background(), b, i, o)
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

func TestUndefinedReferencesLateTextProvenance(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, fault := range []string{"unchanged", "literal", "unavailable", "foreign", "exact-mismatch", "normalized"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, fault), func(t *testing.T) {
				d, b, index, o := md011Parse(t, md052Example+"\n\n"+md052Example, gfm)
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
				got, e := d.UndefinedReferences(context.Background(), b, index, o)
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
					if e != nil || got.Availability != Available || len(got.Spans) != want || got.Spans[0].start != 0 || got.Spans[0].end != 15 {
						t.Fatal(got, e)
					}
				}
			})
		}
	}
}

func TestUndefinedReferencesRepeatedConcurrentDetachedObservation(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		d, b, i, o := md011Parse(t, md052Example, gfm)
		before, e := d.UndefinedReferences(context.Background(), b, i, o)
		if e != nil || len(before.Spans) != 1 {
			t.Fatal(before, e)
		}
		root, token := d.root, d.Source().Token()
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				got, e := d.UndefinedReferences(context.Background(), b, i, o)
				if e != nil || !reflect.DeepEqual(got, before) {
					t.Error(got, e)
				}
				if len(got.Spans) > 0 {
					got.Spans[0] = Span{}
				}
			})
		}
		wg.Wait()
		if d.root != root || d.Source().Token() != token || d.Source().content != md052Example {
			t.Fatal("original source/tree changed")
		}
	}
}

func TestUndefinedReferencesBudgetAndCancellation(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		t.Run(fmt.Sprintf("gfm-%v", gfm), func(t *testing.T) {
			md052Positive(t, gfm)
			d, b, i, o := md011Parse(t, md052Example, gfm)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if got, e := d.UndefinedReferences(ctx, b, i, o); !errors.Is(e, context.Canceled) || got.Spans != nil || got.Availability != "" {
				t.Fatal(got, e)
			}
			if e := b.ChargeWork(b.Limits().WorkUnits - b.work); e != nil {
				t.Fatal(e)
			}
			var stop *LimitExceeded
			for range 2 {
				if got, e := d.UndefinedReferences(context.Background(), b, i, o); !errors.As(e, &stop) || got.Spans != nil || got.Availability != "" {
					t.Fatal(got, e)
				}
			}
		})
	}
}

func TestUndefinedReferencesManyCandidatesAndUnmatchedPrefixWithinSourceBound(t *testing.T) {
	const count = 6000
	for _, gfm := range []bool{false, true} {
		for _, shape := range []string{"many", "unmatched"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, shape), func(t *testing.T) {
				md052Positive(t, gfm)
				text := strings.Repeat("[a][b] ", count)
				want := count
				if shape == "unmatched" {
					text = strings.Repeat("[", count) + "a][b]"
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
				got, e := p.Document.UndefinedReferences(context.Background(), b, NewLineIndex(source), o)
				if e != nil || got.Availability != Available || len(got.Spans) != want {
					t.Fatal(len(got.Spans), got.Availability, e)
				}
			})
		}
	}
}
