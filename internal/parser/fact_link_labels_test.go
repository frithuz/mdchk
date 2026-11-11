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

const md059Example = "[Click here](guide.md)"

type md059Item struct {
	start, end int
	text       string
}

func md059Items(got LinkLabelsInventory) []md059Item {
	var out []md059Item
	for _, item := range got.Items {
		out = append(out, md059Item{item.Span.start, item.Span.end, item.Text})
	}
	return out
}

func md059Positive(t *testing.T, gfm bool) {
	t.Helper()
	d, b, index, options := md011Parse(t, md059Example, gfm)
	got, err := d.LinkLabels(context.Background(), b, index, options)
	if err != nil || got.Availability != Available || !reflect.DeepEqual(md059Items(got), []md059Item{{1, 11, "Click here"}}) || got.Items[0].Span.SourceToken() != index.source.Token() {
		t.Fatal("actual original positive prerequisite", got, err)
	}
}

func TestLinkLabelsItems(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		gfm          []bool
		want         []md059Item
	}{
		{"inline", "[a](u)", nil, []md059Item{{1, 2, "a"}}},
		{"full", "[a][b]\n\n[b]: u", nil, []md059Item{{1, 2, "a"}}},
		{"collapsed", "[a][]\n\n[a]: u", nil, []md059Item{{1, 2, "a"}}},
		{"shortcut", "[a]\n\n[a]: u", nil, []md059Item{{1, 2, "a"}}},
		{"empty", "[](u)", nil, []md059Item{{1, 1, ""}}},
		{"whitespace", "[ \t a \u00a0\u3000 b  ](u)", nil, []md059Item{{1, 15, "a b"}}},
		{"soft-break", "[a\nb](u)", nil, []md059Item{{1, 4, "a b"}}},
		{"hard-break", "[a\\\nb](u)", nil, []md059Item{{1, 5, "a b"}}},
		{"formatting", "[*a* **b** _c_](u)", nil, []md059Item{{1, 14, "a b c"}}},
		{"strike", "[~~a~~](u)", []bool{true}, []md059Item{{1, 6, "a"}}},
		{"code-span", "[` a `](u)", nil, []md059Item{{1, 6, "a"}}},
		{"entity", "[&#104;&amp;](u)", nil, []md059Item{{1, 12, "h&"}}},
		{"escape", "[\\*a\\b](u)", nil, []md059Item{{1, 6, "*a\\b"}}},
		{"inline-html", "[<b>a</b>](u)", nil, []md059Item{{1, 9, "a"}}},
		{"image-only", "[![alt *x*](i.png)](u)", nil, []md059Item{{1, 18, "alt x"}}},
		{"image-and-text", "[![icon](i.png) a](u)", nil, []md059Item{{1, 17, "icon a"}}},
		{"reference-image", "[![a][i]](u)\n\n[i]: i.png", nil, []md059Item{{1, 8, "a"}}},
		{"image", "![a](u)", nil, nil},
		{"link-in-image", "![x [a](u) y](i.png)", nil, nil},
		{"angle-autolink", "<https://example.com>", nil, nil},
		{"bare-gfm", "x https://example.com", []bool{true}, nil},
		{"undefined-full", "[a][b]", nil, nil},
		{"bracket-text", "[not a link]", nil, nil},
		{"definition", "[a]: <https://example.com>", nil, nil},
		{"code-span-link", "`[a](u)`", nil, nil},
		{"fenced", "```\n[a](u)\n```", nil, nil},
		{"indented", "    [a](u)", nil, nil},
		{"html-block", "<div>\n[a](u)\n</div>", nil, nil},
		{"inline-html-attribute", `x <span title="[a](u)">y</span>`, nil, nil},
		{"raw-html-anchor", `<a href="u">a</a>`, nil, nil},
		{"inline-raw", "x <pre>[a](u)</pre> [b](v)", nil, []md059Item{{21, 22, "b"}}},
		{"block-quote", "> [a](u)", nil, []md059Item{{3, 4, "a"}}},
		{"list-item", "- [a](u)", nil, []md059Item{{3, 4, "a"}}},
		{"heading", "# [a](u)", nil, []md059Item{{3, 4, "a"}}},
		{"table", "| h |\n| --- |\n| [a](u) |", []bool{true}, []md059Item{{17, 18, "a"}}},
		{"multiline", "> [a\n> b](u)", nil, []md059Item{{3, 8, "a b"}}},
		{"crlf", "x\r\n[a\r\nb](u)\r\n", nil, []md059Item{{4, 8, "a b"}}},
		{"sorted", "[a](u) [b](v)", nil, []md059Item{{1, 2, "a"}, {8, 9, "b"}}},
	} {
		flavors := tc.gfm
		if flavors == nil {
			flavors = []bool{false, true}
		}
		for _, gfm := range flavors {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, tc.name), func(t *testing.T) {
				d, b, index, o := md011Parse(t, tc.source, gfm)
				got, e := d.LinkLabels(context.Background(), b, index, o)
				if e != nil || got.Availability != Available || !reflect.DeepEqual(md059Items(got), tc.want) {
					t.Fatal(md059Items(got), got.Availability, e)
				}
				for _, item := range got.Items {
					if item.Span.SourceToken() != index.source.Token() {
						t.Fatal("foreign span", item)
					}
				}
			})
		}
	}
}

func TestLinkLabelsOriginalAuthority(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, fault := range []string{"original", "detached", "wrong-source", "wrong-budget", "wrong-options", "incomplete", "nil-document", "nil-context"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, fault), func(t *testing.T) {
				md059Positive(t, gfm)
				d, b, i, o := md011Parse(t, md059Example, gfm)
				ctx := context.Background()
				switch fault {
				case "detached":
					d, _ = NewDocument(d.Root(), d.References())
				case "wrong-source":
					i = NewLineIndex(NewSource([]byte(md059Example)))
				case "wrong-budget":
					b, _ = NewOperationBudget(DefaultLimits())
				case "wrong-options":
					o.GFM = !o.GFM
				case "incomplete":
					d.parsedComplete = false
				case "nil-document":
					d = nil
				case "nil-context":
					ctx = nil
				}
				got, e := d.LinkLabels(ctx, b, i, o)
				if fault == "original" {
					if e != nil || len(got.Items) != 1 || got.Items[0].Text != "Click here" {
						t.Fatal(got, e)
					}
				} else if !errors.Is(e, ErrModel) || got.Items != nil {
					t.Fatal(got, e)
				}
			})
		}
	}
}

func TestLinkLabelsUnknownAndLateProvenance(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, tc := range []struct {
			name, source string
			available    Availability
			count        int
		}{
			{"unknown-after-last", md059Example + "\n\n<div>\n<x title=\"", Available, 1},
			{"unknown-before-later", md059Example + "\n\n<div>\n<x title=\"\n\n" + md059Example, Unavailable, 0},
			{"unknown-before-unrelated", md059Example + "\n\n<div>\n<x title=\"\n\nplain", Available, 1},
		} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, tc.name), func(t *testing.T) {
				md059Positive(t, gfm)
				d, b, i, o := md011Parse(t, tc.source, gfm)
				got, e := d.LinkLabels(context.Background(), b, i, o)
				if e != nil || got.Availability != tc.available || len(got.Items) != tc.count {
					t.Fatal(got, e)
				}
			})
		}
		for _, fault := range []string{"link-properties", "image-properties", "cell-properties", "origin", "envelope", "foreign", "markers", "label-child", "content"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, fault), func(t *testing.T) {
				md059Positive(t, gfm)
				source := "[x](u)\n\n![y](v)\n\n[*later*](w)"
				if fault == "cell-properties" {
					if !gfm {
						t.Skip("tables are GFM only")
					}
					source = "| h |\n| --- |\n| [a](u) |"
				}
				d, b, i, o := md011Parse(t, source, gfm)
				if pre, e := d.LinkLabels(context.Background(), b, i, o); e != nil || pre.Availability != Available || len(pre.Items) == 0 {
					t.Fatal("positive before fault", pre, e)
				}
				links, images := md070Nodes(d, Link), md070Nodes(d, Image)
				switch fault {
				case "link-properties":
					links[len(links)-1].spec.Properties.Link = nil
				case "image-properties":
					images[0].spec.Properties.Link = nil
				case "cell-properties":
					cells := md070Nodes(d, TableCell)
					cells[len(cells)-1].spec.Properties.Cell = nil
				case "origin":
					links[len(links)-1].spec.Properties.Link.Origin = "unknown"
				case "envelope":
					links[len(links)-1].spec.Envelope.end = i.source.Len() + 1
				case "foreign":
					links[len(links)-1].spec.Envelope.source = NewSource(i.source.Bytes())
				case "markers":
					links[len(links)-1].spec.Markers = nil
				case "label-child":
					links[len(links)-1].spec.Children[0].spec.Kind = Paragraph
				case "content":
					texts := md070Nodes(d, Text)
					texts[len(texts)-1].spec.Content[0].Semantic.Present = false
				}
				got, e := d.LinkLabels(context.Background(), b, i, o)
				switch fault {
				case "origin", "envelope", "foreign", "markers", "label-child", "content":
					if !errors.Is(e, ErrModel) || got.Items != nil || got.Availability != "" {
						t.Fatal(got, e)
					}
				default:
					if e != nil || got.Availability != Unavailable || got.Items != nil {
						t.Fatal(got, e)
					}
				}
			})
		}
	}
}

func TestLinkLabelsRepeatedConcurrentDetachedObservation(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		d, b, i, o := md011Parse(t, md059Example, gfm)
		before, e := d.LinkLabels(context.Background(), b, i, o)
		if e != nil || len(before.Items) != 1 {
			t.Fatal(before, e)
		}
		root, token := d.root, d.Source().Token()
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				got, e := d.LinkLabels(context.Background(), b, i, o)
				if e != nil || !reflect.DeepEqual(got, before) {
					t.Error(got, e)
				}
				if len(got.Items) > 0 {
					got.Items[0] = LinkLabel{}
				}
			})
		}
		wg.Wait()
		if d.root != root || d.Source().Token() != token || d.Source().content != md059Example {
			t.Fatal("original source/tree changed")
		}
	}
}

func TestLinkLabelsBudgetAndCancellation(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		t.Run(fmt.Sprintf("gfm-%v", gfm), func(t *testing.T) {
			md059Positive(t, gfm)
			d, b, i, o := md011Parse(t, md059Example, gfm)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if got, e := d.LinkLabels(ctx, b, i, o); !errors.Is(e, context.Canceled) || got.Items != nil || got.Availability != "" {
				t.Fatal(got, e)
			}
			if e := b.ChargeWork(b.Limits().WorkUnits - b.work); e != nil {
				t.Fatal(e)
			}
			var stop *LimitExceeded
			for range 2 {
				if got, e := d.LinkLabels(context.Background(), b, i, o); !errors.As(e, &stop) || got.Items != nil || got.Availability != "" {
					t.Fatal(got, e)
				}
			}
		})
	}
}

func TestLinkLabelsManyItemsWithinSourceBound(t *testing.T) {
	const count = 1000
	for _, gfm := range []bool{false, true} {
		t.Run(fmt.Sprintf("gfm-%v", gfm), func(t *testing.T) {
			md059Positive(t, gfm)
			text := strings.Repeat("[a](u) ![b](v) [![c](w)](x) ", count)
			source := NewSource([]byte(text))
			limits := DefaultLimits()
			limits.WorkUnits = 2048*int64(len(text)) + 1<<20
			b, e := NewOperationBudget(limits)
			if e != nil {
				t.Fatal(e)
			}
			o := ParseOptions{GFM: gfm}
			p := ParseWithOptions(context.Background(), source, b, o)
			if p.Status != Complete || p.Document == nil {
				t.Fatal(p.Status, p.Interruption)
			}
			got, e := p.Document.LinkLabels(context.Background(), b, NewLineIndex(source), o)
			if e != nil || got.Availability != Available || len(got.Items) != 2*count {
				t.Fatal(len(got.Items), got.Availability, e)
			}
			for j := 1; j < len(got.Items); j++ {
				if got.Items[j].Span.start <= got.Items[j-1].Span.start {
					t.Fatal("unsorted", j, got.Items[j-1], got.Items[j])
				}
			}
		})
	}
}
