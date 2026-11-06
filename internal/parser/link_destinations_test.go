package parser

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestLinkDestinationsObservations(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, tc := range []struct {
			name, source string
			want         []string // destination@start-end/segments
			bare         []string // replaces want under GFM when set
		}{
			{"inline", "[x](https://a.test/p) [y](#a) [z]()", []string{"https://a.test/p@4-20/1", "#a@26-28/1", "@34-34/1"}, nil},
			{"image", "![x](https://a.test/i.png)", []string{"https://a.test/i.png@5-25/1"}, nil},
			{"image in link", "[![x](a.png)](b.md)", []string{"a.png@6-11/1", "b.md@14-18/1"}, nil},
			{"link in image description", "![[x](a.md)](b.png)", []string{"b.png@13-18/1"}, nil},
			{"angle autolink", "<https://a.test/x>", []string{"https://a.test/x@1-17/1"}, nil},
			{"email autolink", "<u@a.test>", []string{"mailto:u@a.test@1-9/2"}, nil},
			{"bare url", "see https://a.test/x and www.b.test", nil, []string{"https://a.test/x@4-20/1", "http://www.b.test@25-35/2"}},
			{"bare email", "u@a.test", nil, []string{"mailto:u@a.test@0-8/2"}},
			{"escape and entity", "[x](https://a\\.test/&amp;)", []string{"https://a.test/&@4-25/4"}, nil},
			{"definition owns references", "[a] [b][a] [c][] ![a]\n\n[a]: https://a.test\n[c]: <https://c.test> \"t\"\n", []string{"https://a.test@28-42/1", "https://c.test@49-63/1"}, nil},
			{"unused definition", "[r]: https://r.test\n", []string{"https://r.test@5-19/1"}, nil},
			{"duplicate definitions", "[r]: https://a.test\n[r]: https://b.test\n\n[r]\n", []string{"https://a.test@5-19/1", "https://b.test@25-39/1"}, nil},
			{"definition in containers", "> - [r]: https://r.test\n", []string{"https://r.test@9-23/1"}, nil},
			{"code", "`[x](a.md)`\n\n```\n![x](a.png)\n<https://a.test>\n```\n\n    [x](a.md)\n", nil, nil},
			{"html block", "<div>\n[x](a.md) <https://a.test>\n</div>\n", nil, nil},
			{"raw inline html", "x <pre>[x](a.md)</pre> [y](b.md)\n", []string{"b.md@27-31/1"}, nil},
			{"definition in raw html", "> <pre>\n\n[r]: https://r.test\n\n</pre> [x](a.md)\n", []string{"a.md@41-45/1"}, nil},
			{"definition after closed html", "> <div>\n\n[r]: https://r.test\n", []string{"https://r.test@14-28/1"}, nil},
			{"containers", "> - [x](a.md)\n\n- > <https://a.test>\n", []string{"a.md@8-12/1", "https://a.test@20-34/1"}, nil},
			{"heading", "# [x](a.md)\n", []string{"a.md@6-10/1"}, nil},
			{"table cell", "| a |\n| - |\n| [x](a.md) |\n", []string{"a.md@18-22/1"}, nil},
		} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, tc.name), func(t *testing.T) {
				d, b, i, o := md011Parse(t, tc.source, gfm)
				got, e := d.LinkDestinations(context.Background(), b, i, o)
				if e != nil || got.Availability != Available {
					t.Fatal(got, e)
				}
				var have []string
				for _, l := range got.Destinations {
					have = append(have, fmt.Sprintf("%s@%d-%d/%d", l.Destination, l.Span.Start(), l.Span.End(), len(l.Segments)))
				}
				want := tc.want
				if gfm && tc.bare != nil {
					want = tc.bare
				}
				if fmt.Sprint(have) != fmt.Sprint(want) {
					t.Fatal(have, want)
				}
			})
		}
	}
}

func TestLinkDestinationsSegments(t *testing.T) {
	d, b, i, o := md011Parse(t, "www.a.test\n", true)
	got, e := d.LinkDestinations(context.Background(), b, i, o)
	if e != nil || len(got.Destinations) != 1 {
		t.Fatal(got, e)
	}
	s := got.Destinations[0].Segments
	if len(s) != 2 || s[0].Transformation != Synthetic || s[0].Source.Present() || s[0].Semantic.Value != "http://" || s[1].Transformation != Exact || s[1].Source.Start() != 0 || s[1].Source.End() != 10 {
		t.Fatal(s)
	}
	// Results are detached from the document and from each other.
	got.Destinations[0].Segments[0] = Segment{}
	got, e = d.LinkDestinations(context.Background(), b, i, o)
	if e != nil || got.Destinations[0].Segments[0].Semantic.Value != "http://" {
		t.Fatal(got, e)
	}
}

func TestLinkDestinationsUnavailable(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		t.Run(fmt.Sprintf("gfm-%v/raw", gfm), func(t *testing.T) {
			d, b, i, o := md011Parse(t, "<div>\n<x title=\"\n\n[x](a.md)", gfm)
			got, e := d.LinkDestinations(context.Background(), b, i, o)
			if e != nil || got.Availability != Unavailable || got.Destinations != nil {
				t.Fatal(got, e)
			}
		})
		t.Run(fmt.Sprintf("gfm-%v/references", gfm), func(t *testing.T) {
			d, b, i, o := md011Parse(t, "[x](a.md)\n", gfm)
			d.references = ReferenceCollection{Availability: Unavailable, LookupAvailability: Unavailable}
			got, e := d.LinkDestinations(context.Background(), b, i, o)
			if e != nil || got.Availability != Unavailable || got.Destinations != nil {
				t.Fatal(got, e)
			}
		})
	}
}

func TestLinkDestinationsOriginalAuthority(t *testing.T) {
	for _, fault := range []string{"detached", "wrong-source", "wrong-budget", "incomplete", "nil-document"} {
		t.Run(fault, func(t *testing.T) {
			d, b, i, o := md011Parse(t, "[x](https://a.test)", false)
			switch fault {
			case "detached":
				d, _ = NewDocument(d.Root(), d.References())
			case "wrong-source":
				i = NewLineIndex(NewSource([]byte("[x](https://a.test)")))
			case "wrong-budget":
				b, _ = NewOperationBudget(DefaultLimits())
			case "incomplete":
				d.parsedComplete = false
			case "nil-document":
				d = nil
			}
			if got, e := d.LinkDestinations(context.Background(), b, i, o); !errors.Is(e, ErrModel) || got.Destinations != nil {
				t.Fatal(got, e)
			}
		})
	}
}
