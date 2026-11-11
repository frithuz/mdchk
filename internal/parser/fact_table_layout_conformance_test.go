//go:build mdchk_conformance

package parser_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestTableLayoutPrivateConformanceCannotGrantAuthority(t *testing.T) {
	const source = "| A| B |\n|---|---|\n|1|2|\n"
	s := parser.NewSource([]byte(source))
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	o := parser.ParseOptions{GFM: true}
	p := parser.ParseWithOptions(context.Background(), s, b, o)
	got, e := p.Document.TableLayout(context.Background(), b, parser.NewLineIndex(s), o)
	if p.Status != parser.Complete || e != nil || len(got.Tables) != 1 || len(got.Tables[0].Rows) != 3 || got.Tables[0].Rows[0].Cells[0].FieldStart != 1 {
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
			got, e := p.Document.TableLayout(context.Background(), b, parser.NewLineIndex(s), o)
			if e == nil || got.Tables != nil {
				t.Fatal(scope, got, e)
			}
		}
	}
}
