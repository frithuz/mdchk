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

const md054Example = "[Guide](guide.md)"

type md054Item struct {
	start, end    int
	image         bool
	style, origin string
}

func md054Items(got LinkRepresentationsInventory) []md054Item {
	var out []md054Item
	for _, item := range got.Items {
		out = append(out, md054Item{item.Span.start, item.Span.end, item.Image, item.Style, item.Origin})
	}
	return out
}

func md054Positive(t *testing.T, gfm bool) {
	t.Helper()
	d, b, index, options := md011Parse(t, md054Example, gfm)
	got, err := d.LinkRepresentations(context.Background(), b, index, options)
	if err != nil || got.Availability != Available || !reflect.DeepEqual(md054Items(got), []md054Item{{0, 17, false, "inline", "inline"}}) || got.Items[0].Span.SourceToken() != index.source.Token() {
		t.Fatal("actual original positive prerequisite", got, err)
	}
}

func TestLinkRepresentationsItems(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		gfm          []bool
		want         []md054Item
	}{
		{"inline", "[a](u)", nil, []md054Item{{0, 6, false, "inline", "inline"}}},
		{"inline-image", "![a](u)", nil, []md054Item{{0, 7, true, "inline", "inline"}}},
		{"full", "[a][b]\n\n[b]: u", nil, []md054Item{{0, 6, false, "full_reference", "full"}}},
		{"full-image", "![a][b]\n\n[b]: u", nil, []md054Item{{0, 7, true, "full_reference", "full"}}},
		{"collapsed", "[a][]\n\n[a]: u", nil, []md054Item{{0, 5, false, "collapsed_reference", "collapsed"}}},
		{"collapsed-image", "![a][]\n\n[a]: u", nil, []md054Item{{0, 6, true, "collapsed_reference", "collapsed"}}},
		{"shortcut", "[a]\n\n[a]: u", nil, []md054Item{{0, 3, false, "shortcut_reference", "shortcut"}}},
		{"shortcut-image", "![a]\n\n[a]: u", nil, []md054Item{{0, 4, true, "shortcut_reference", "shortcut"}}},
		{"angle-autolink", "<https://example.com>", nil, []md054Item{{0, 21, false, "autolink", "autolink"}}},
		{"bare-gfm", "x https://example.com", []bool{true}, []md054Item{{2, 21, false, "autolink", "bare-gfm"}}},
		{"bare-commonmark", "x https://example.com", []bool{false}, nil},
		{"undefined-full", "[a][b]", nil, nil},
		{"bracket-text", "[not a link]", nil, nil},
		{"definition", "[a]: <https://example.com>", nil, nil},
		{"code-span", "`[a](u)`", nil, nil},
		{"fenced", "```\n[a](u)\n```", nil, nil},
		{"indented", "    [a](u)", nil, nil},
		{"html-block", "<div>\n[a](u)\n</div>", nil, nil},
		{"inline-html", `x <span title="[a](u)">y</span>`, nil, nil},
		{"inline-raw", "x <pre>[a](u)</pre> [b](v)", nil, []md054Item{{20, 26, false, "inline", "inline"}}},
		{"opaque-image-raw", "![<pre>](i) [b](v)", nil, []md054Item{{0, 11, true, "inline", "inline"}, {12, 18, false, "inline", "inline"}}},
		{"block-quote", "> [a](u)", nil, []md054Item{{2, 8, false, "inline", "inline"}}},
		{"list-item", "- [a](u)", nil, []md054Item{{2, 8, false, "inline", "inline"}}},
		{"heading", "# [a](u)", nil, []md054Item{{2, 8, false, "inline", "inline"}}},
		{"table", "| h |\n| --- |\n| [a](u) |", []bool{true}, []md054Item{{16, 22, false, "inline", "inline"}}},
		{"image-in-link", "[![alt](a.png)](b)", nil, []md054Item{{0, 18, false, "inline", "inline"}, {1, 14, true, "inline", "inline"}}},
		{"link-in-image", "![x [a](u) y](i.png)", nil, []md054Item{{0, 20, true, "inline", "inline"}, {4, 10, false, "inline", "inline"}}},
		{"multiline", "> [a\n> b](u)", nil, []md054Item{{2, 12, false, "inline", "inline"}}},
		{"crlf", "x\r\n[a](u)\r\n", nil, []md054Item{{3, 9, false, "inline", "inline"}}},
	} {
		flavors := tc.gfm
		if flavors == nil {
			flavors = []bool{false, true}
		}
		for _, gfm := range flavors {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, tc.name), func(t *testing.T) {
				d, b, index, o := md011Parse(t, tc.source, gfm)
				got, e := d.LinkRepresentations(context.Background(), b, index, o)
				if e != nil || got.Availability != Available || !reflect.DeepEqual(md054Items(got), tc.want) {
					t.Fatal(md054Items(got), got.Availability, e)
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

func TestLinkRepresentationsOriginalAuthority(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, fault := range []string{"original", "detached", "wrong-source", "wrong-budget", "wrong-options", "incomplete", "nil-document", "nil-context"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, fault), func(t *testing.T) {
				md054Positive(t, gfm)
				d, b, i, o := md011Parse(t, md054Example, gfm)
				ctx := context.Background()
				switch fault {
				case "detached":
					d, _ = NewDocument(d.Root(), d.References())
				case "wrong-source":
					i = NewLineIndex(NewSource([]byte(md054Example)))
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
				got, e := d.LinkRepresentations(ctx, b, i, o)
				if fault == "original" {
					if e != nil || len(got.Items) != 1 {
						t.Fatal(got, e)
					}
				} else if !errors.Is(e, ErrModel) || got.Items != nil {
					t.Fatal(got, e)
				}
			})
		}
	}
}

func TestLinkRepresentationsUnknownAndLateProvenance(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, tc := range []struct {
			name, source string
			available    Availability
			count        int
		}{
			{"unknown-after-last", md054Example + "\n\n<div>\n<x title=\"", Available, 1},
			{"unknown-before-later", md054Example + "\n\n<div>\n<x title=\"\n\n" + md054Example, Unavailable, 0},
			{"unknown-before-unrelated", md054Example + "\n\n<div>\n<x title=\"\n\nplain", Available, 1},
		} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, tc.name), func(t *testing.T) {
				md054Positive(t, gfm)
				d, b, i, o := md011Parse(t, tc.source, gfm)
				got, e := d.LinkRepresentations(context.Background(), b, i, o)
				if e != nil || got.Availability != tc.available || len(got.Items) != tc.count {
					t.Fatal(got, e)
				}
			})
		}
		for _, fault := range []string{"link-properties", "image-properties", "cell-properties", "origin", "envelope", "foreign"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, fault), func(t *testing.T) {
				md054Positive(t, gfm)
				source := "[x](u)\n\n![y](v)\n\n[later](w)"
				if fault == "cell-properties" {
					if !gfm {
						t.Skip("tables are GFM only")
					}
					source = "| h |\n| --- |\n| [a](u) |"
				}
				d, b, i, o := md011Parse(t, source, gfm)
				if pre, e := d.LinkRepresentations(context.Background(), b, i, o); e != nil || pre.Availability != Available || len(pre.Items) == 0 {
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
				}
				got, e := d.LinkRepresentations(context.Background(), b, i, o)
				switch fault {
				case "origin", "envelope", "foreign":
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

func TestLinkRepresentationsRepeatedConcurrentDetachedObservation(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		d, b, i, o := md011Parse(t, md054Example, gfm)
		before, e := d.LinkRepresentations(context.Background(), b, i, o)
		if e != nil || len(before.Items) != 1 {
			t.Fatal(before, e)
		}
		root, token := d.root, d.Source().Token()
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				got, e := d.LinkRepresentations(context.Background(), b, i, o)
				if e != nil || !reflect.DeepEqual(got, before) {
					t.Error(got, e)
				}
				if len(got.Items) > 0 {
					got.Items[0] = LinkRepresentation{}
				}
			})
		}
		wg.Wait()
		if d.root != root || d.Source().Token() != token || d.Source().content != md054Example {
			t.Fatal("original source/tree changed")
		}
	}
}

func TestLinkRepresentationsBudgetAndCancellation(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		t.Run(fmt.Sprintf("gfm-%v", gfm), func(t *testing.T) {
			md054Positive(t, gfm)
			d, b, i, o := md011Parse(t, md054Example, gfm)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if got, e := d.LinkRepresentations(ctx, b, i, o); !errors.Is(e, context.Canceled) || got.Items != nil || got.Availability != "" {
				t.Fatal(got, e)
			}
			if e := b.ChargeWork(b.Limits().WorkUnits - b.work); e != nil {
				t.Fatal(e)
			}
			var stop *LimitExceeded
			for range 2 {
				if got, e := d.LinkRepresentations(context.Background(), b, i, o); !errors.As(e, &stop) || got.Items != nil || got.Availability != "" {
					t.Fatal(got, e)
				}
			}
		})
	}
}

func TestLinkRepresentationsManyItemsWithinSourceBound(t *testing.T) {
	const count = 2000
	for _, gfm := range []bool{false, true} {
		t.Run(fmt.Sprintf("gfm-%v", gfm), func(t *testing.T) {
			md054Positive(t, gfm)
			text := strings.Repeat("[a](u) ![b](v) ", count)
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
			got, e := p.Document.LinkRepresentations(context.Background(), b, NewLineIndex(source), o)
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
