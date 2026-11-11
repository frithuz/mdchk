package parser

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestFragmentLinksOriginalAuthority(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, fault := range []string{"original", "detached", "wrong-source", "wrong-budget", "wrong-options", "incomplete", "nil-document", "nil-context"} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, fault), func(t *testing.T) {
				d, b, i, o := md011Parse(t, "[x](#a)", gfm)
				got, e := d.FragmentLinks(context.Background(), b, i, o)
				if e != nil || got.Availability != Available || len(got.Links) != 1 {
					t.Fatal("positive prerequisite", got, e)
				}
				ctx := context.Background()
				switch fault {
				case "detached":
					d, _ = NewDocument(d.Root(), d.References())
				case "wrong-source":
					i = NewLineIndex(NewSource([]byte("[x](#a)")))
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
				got, e = d.FragmentLinks(ctx, b, i, o)
				if fault == "original" {
					if e != nil || len(got.Links) != 1 {
						t.Fatal(got, e)
					}
				} else if !errors.Is(e, ErrModel) || got.Links != nil {
					t.Fatal(got, e)
				}
			})
		}
	}
}

func TestFragmentLinksObservations(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, tc := range []struct {
			name, source string
			want         []string // destination@start-end
		}{
			{"inline", "[x](#a)", []string{"#a@4-6"}},
			{"path", "[x](other.md#a) [y](other.md)", []string{"other.md#a@4-14"}},
			{"angle", "[x](<#a b>)", []string{"#a b@5-9"}},
			{"escape", "[x](#a\\_b)", []string{"#a_b@4-9"}},
			{"entity", "[x](#a&amp;b)", []string{"#a&b@4-12"}},
			{"reference", "[x][r] [y][] [z]\n\n[r]: #a\n[y]: #b\n[z]: #c\n", []string{"#a@0-6", "#b@7-12", "#c@13-16"}},
			{"unresolved reference", "[x][missing]\n", nil},
			{"autolink", "<https://e.test/#a> https://e.test/#b", nil},
			{"image", "![x](#a)", nil},
			{"code", "`[x](#a)`\n\n```\n[x](#a)\n```\n\n    [x](#a)\n", nil},
			{"container", "> - [x](#a)\n", []string{"#a@8-10"}},
			{"heading", "# [x](#a)\n", []string{"#a@6-8"}},
			{"no fragment", "[x](a.md) [y]()", nil},
			{"raw html", "<div>\n<x title=\"\n\n[x](#a)", nil},
		} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, tc.name), func(t *testing.T) {
				d, b, i, o := md011Parse(t, tc.source, gfm)
				got, e := d.FragmentLinks(context.Background(), b, i, o)
				if e != nil {
					t.Fatal(e)
				}
				var have []string
				for _, l := range got.Links {
					have = append(have, fmt.Sprintf("%s@%d-%d", l.Destination, l.Span.Start(), l.Span.End()))
				}
				if fmt.Sprint(have) != fmt.Sprint(tc.want) {
					t.Fatal(have, tc.want, got.Availability)
				}
			})
		}
	}
}

func TestFragmentLinksDetachedConcurrentResults(t *testing.T) {
	d, b, i, o := md011Parse(t, "[x](#a)", false)
	got, e := d.FragmentLinks(context.Background(), b, i, o)
	if e != nil || len(got.Links) != 1 {
		t.Fatal(got, e)
	}
	got.Links[0] = FragmentLink{}
	got, e = d.FragmentLinks(context.Background(), b, i, o)
	if e != nil || got.Links[0].Destination != "#a" {
		t.Fatal(got, e)
	}
}
