package parser

import (
	"context"
	"fmt"
	"testing"
)

func TestLocalLinksObservations(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, tc := range []struct {
			name, source string
			want         []string // [!]destination@start-end
		}{
			{"inline", "[x](a.md) [y](#a) [z]()", []string{"a.md@4-8", "#a@14-16", "@22-22"}},
			{"image", "![x](a.png)", []string{"!a.png@5-10"}},
			{"image in link", "[![x](a.png)](b.md)", []string{"!a.png@6-11", "b.md@14-18"}},
			{"link in image description", "![[x](a.md)](b.png)", []string{"!b.png@13-18"}},
			{"reference", "[x][r] ![y][] [z]\n\n[r]: a.md\n[y]: b.png\n[z]: c.md\n", []string{"a.md@0-6", "!b.png@7-13", "c.md@14-17"}},
			{"reference image in link", "[![i][r]](b.md)\n\n[r]: a.png\n", []string{"!a.png@1-8", "b.md@10-14"}},
			{"autolink", "<https://e.test/a.md> https://e.test/b.md", nil},
			{"code", "`[x](a.md)`\n\n```\n![x](a.png)\n```\n\n    [x](a.md)\n", nil},
			{"container", "> - ![x](a.png)\n", []string{"!a.png@9-14"}},
			{"raw html", "<div>\n<x title=\"\n\n[x](a.md)", nil},
		} {
			t.Run(fmt.Sprintf("gfm-%v/%s", gfm, tc.name), func(t *testing.T) {
				d, b, i, o := md011Parse(t, tc.source, gfm)
				got, e := d.LocalLinks(context.Background(), b, i, o)
				if e != nil {
					t.Fatal(e)
				}
				var have []string
				for _, l := range got.Links {
					image := ""
					if l.Image {
						image = "!"
					}
					have = append(have, fmt.Sprintf("%s%s@%d-%d", image, l.Destination, l.Span.Start(), l.Span.End()))
				}
				if fmt.Sprint(have) != fmt.Sprint(tc.want) {
					t.Fatal(have, tc.want, got.Availability)
				}
			})
		}
	}
}
