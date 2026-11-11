//go:build mdchk_conformance

package parser_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestLinkLabelsPrivateConformanceCannotGrantAuthority(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		s := parser.NewSource([]byte("[Guide](guide.md)"))
		b, _ := parser.NewOperationBudget(parser.DefaultLimits())
		o := parser.ParseOptions{GFM: gfm}
		p := parser.ParseWithOptions(context.Background(), s, b, o)
		got, e := p.Document.LinkLabels(context.Background(), b, parser.NewLineIndex(s), o)
		if p.Status != parser.Complete || e != nil || len(got.Items) != 1 || got.Items[0].Span.Start() != 1 || got.Items[0].Span.End() != 6 || got.Items[0].Text != "Guide" {
			t.Fatal("actual original prerequisite", p, got, e)
		}
	}
	for _, scope := range []string{"tables-only", "tasks-only", "strike-only", "bare-only", "filter-only"} {
		s := parser.NewSource([]byte("[Guide](guide.md)"))
		b, _ := parser.NewOperationBudget(parser.DefaultLimits())
		p, e := parser.ParseConformance(context.Background(), s, b, scope)
		if e != nil || p.Status != parser.Complete {
			t.Fatal(scope, p, e)
		}
		got, e := p.Document.LinkLabels(context.Background(), b, parser.NewLineIndex(s), parser.ParseOptions{})
		if e == nil || got.Items != nil {
			t.Fatal(scope, got, e)
		}
	}
}
