//go:build mdchk_conformance

package parser_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestFragmentLinksPrivateConformanceCannotGrantAuthority(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		s := parser.NewSource([]byte("[x](#a)"))
		b, _ := parser.NewOperationBudget(parser.DefaultLimits())
		o := parser.ParseOptions{GFM: gfm}
		p := parser.ParseWithOptions(context.Background(), s, b, o)
		got, e := p.Document.FragmentLinks(context.Background(), b, parser.NewLineIndex(s), o)
		if p.Status != parser.Complete || e != nil || len(got.Links) != 1 || got.Links[0].Span.Start() != 4 || got.Links[0].Span.End() != 6 {
			t.Fatal("actual original prerequisite", p, got, e)
		}
	}
	for _, scope := range []string{"tables-only", "tasks-only", "strike-only", "bare-only", "filter-only"} {
		s := parser.NewSource([]byte("[x](#a)"))
		b, _ := parser.NewOperationBudget(parser.DefaultLimits())
		p, e := parser.ParseConformance(context.Background(), s, b, scope)
		if e != nil || p.Status != parser.Complete {
			t.Fatal(scope, p, e)
		}
		got, e := p.Document.FragmentLinks(context.Background(), b, parser.NewLineIndex(s), parser.ParseOptions{})
		if e == nil || got.Links != nil {
			t.Fatal(scope, got, e)
		}
	}
}
