package parser

import "testing"

func TestBlockquoteSpacesIndependentFinalOwners(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   []Kind
	}{
		{"> a\n>  <!--x--> tail", []Kind{Paragraph, HTMLBlock}},
		{"---\n>  text\n---\n\n>  body", []Kind{Paragraph}},
	} {
		d, _, _ := sectionParse(t, tc.source)
		var got []Kind
		for _, n := range d.root.spec.Children {
			if n.Kind() == BlockQuote {
				for _, c := range n.spec.Children {
					got = append(got, c.Kind())
				}
				break
			}
		}
		if len(got) != len(tc.want) {
			t.Fatal(got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatal(got, tc.want)
			}
		}
	}
}
