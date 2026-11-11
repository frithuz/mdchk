//go:build mdchk_conformance

package parser_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestTableOuterPipesPrivateConformanceCannotGrantAuthority(t *testing.T) {
	const source = "A | B\n--- | ---\n| 1 | 2 |\n"
	s := parser.NewSource([]byte(source))
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	o := parser.ParseOptions{GFM: true}
	p := parser.ParseWithOptions(context.Background(), s, b, o)
	got, e := p.Document.TableOuterPipes(context.Background(), b, parser.NewLineIndex(s), o)
	if p.Status != parser.Complete || e != nil || len(got.Rows) != 3 || !got.Rows[2].LeadingPipe.Present() || got.Rows[2].LeadingPipe.Start() != 16 {
		t.Fatal("actual original prerequisite", p, got, e)
	}
	for _, scope := range []string{"tables-only", "tasks-only", "strike-only", "bare-only", "filter-only"} {
		s := parser.NewSource([]byte(source))
		b, _ := parser.NewOperationBudget(parser.DefaultLimits())
		p, e := parser.ParseConformance(context.Background(), s, b, scope)
		if e != nil || p.Status != parser.Complete {
			t.Fatal(scope, p, e)
		}
		for _, o := range []parser.ParseOptions{{}, {GFM: true}} {
			got, e := p.Document.TableOuterPipes(context.Background(), b, parser.NewLineIndex(s), o)
			if e == nil || got.Rows != nil {
				t.Fatal(scope, got, e)
			}
		}
	}
}
