package parser

import (
	"context"
	"errors"
	"testing"
)

func TestListSeparatorProjectionOriginalGeometry(t *testing.T) {
	for _, tc := range []struct {
		source              string
		start, end, padding int
		nonempty            bool
	}{
		{"-", 1, 1, 1, false}, {"-   ", 1, 4, 1, false}, {"-\t", 1, 2, 1, false},
		{"-\n  paragraph", 1, 1, 1, false}, {"-\n      code", 1, 1, 1, false},
		{"-  [r]: /url", 1, 3, 2, true}, {"-  [ ]", 1, 3, 2, true},
		{"-     code", 1, 2, 1, true}, {"-\t  code", 1, 2, 1, true},
		{"-\tOne", 1, 2, 3, true}, {"> -\tOne", 3, 4, 1, true},
		{">\t-  雪", 3, 5, 2, true},
	} {
		t.Run(tc.source, func(t *testing.T) {
			d, b, index := sectionParse(t, tc.source)
			facts, err := d.ListSeparators(context.Background(), b, index, ParseOptions{})
			if err != nil || len(facts) != 1 {
				t.Fatal(facts, err)
			}
			f := facts[0]
			if f.Separator.Start() != tc.start || f.Separator.End() != tc.end || f.Padding != tc.padding || f.FirstLineNonempty != tc.nonempty || f.Separator.SourceToken() != d.Source().Token() {
				t.Fatal(f, tc)
			}
			facts[0] = ListSeparatorFact{}
			again, err := d.ListSeparators(context.Background(), b, index, ParseOptions{})
			if err != nil || again[0] != f {
				t.Fatal(again, err)
			}
		})
	}
}

func TestListSeparatorProjectionOwnReservations(t *testing.T) {
	for _, kind := range []string{"capacity", "record", "lookup"} {
		t.Run(kind, func(t *testing.T) {
			d, b, index := sectionParse(t, "-  a\n-  b\n-  c")
			beforeWork, beforeNodes := b.work, b.nodes
			inventory, err := d.ListItems(context.Background(), b, index, ParseOptions{})
			if err != nil || len(inventory) != 3 {
				t.Fatal(inventory, err)
			}
			work, nodes := b.work-beforeWork, b.nodes-beforeNodes
			d, b, index = sectionParse(t, "-  a\n-  b\n-  c")
			switch kind {
			case "capacity":
				b.limits.TotalNodes = b.nodes + nodes + 3
			case "record":
				b.limits.TotalNodes = b.nodes + nodes + 3
			case "lookup":
				b.limits.WorkUnits = b.work + work + 7
			}
			b.limits.DocumentNodes = b.limits.TotalNodes
			got, err := d.ListSeparators(context.Background(), b, index, ParseOptions{})
			var limit *LimitExceeded
			if !errors.As(err, &limit) || got != nil {
				t.Fatal(got, err)
			}
		})
	}
}
