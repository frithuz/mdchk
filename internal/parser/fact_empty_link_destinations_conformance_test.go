//go:build mdchk_conformance

package parser_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestEmptyLinkDestinationsPrivateConformanceCannotGrantAuthority(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		s := parser.NewSource([]byte("[x](#)"))
		b, _ := parser.NewOperationBudget(parser.DefaultLimits())
		o := parser.ParseOptions{GFM: gfm}
		p := parser.ParseWithOptions(context.Background(), s, b, o)
		got, e := p.Document.EmptyLinkDestinations(context.Background(), b, parser.NewLineIndex(s), o)
		if p.Status != parser.Complete || e != nil || len(got.Spans) != 1 || got.Spans[0].Start() != 4 || got.Spans[0].End() != 5 {
			t.Fatal("actual original prerequisite", p, got, e)
		}
	}
	for _, scope := range []string{"tables-only", "tasks-only", "strike-only", "bare-only", "filter-only"} {
		s := parser.NewSource([]byte("[x](#)"))
		b, _ := parser.NewOperationBudget(parser.DefaultLimits())
		p, e := parser.ParseConformance(context.Background(), s, b, scope)
		if e != nil || p.Status != parser.Complete {
			t.Fatal(scope, p, e)
		}
		got, e := p.Document.EmptyLinkDestinations(context.Background(), b, parser.NewLineIndex(s), parser.ParseOptions{})
		if e == nil || got.Spans != nil {
			t.Fatal(scope, got, e)
		}
	}
}
