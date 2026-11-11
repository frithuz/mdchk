//go:build mdchk_conformance

package parser_test

import (
	"context"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
)

func TestEmptySectionsRejectsPrivateScopes(t *testing.T) {
	for _, scope := range []string{"tables-only", "tasks-only", "strike-only", "bare-only", "filter-only"} {
		source := parser.NewSource([]byte("# A"))
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		p, e := parser.ParseConformance(context.Background(), source, budget, scope)
		if e != nil || p.Status != parser.Complete {
			t.Fatal(scope, p, e)
		}
		if facts, e := p.Document.EmptySections(context.Background(), budget, parser.NewLineIndex(source), parser.ParseOptions{}); e == nil || facts != nil {
			t.Fatal("private scope accepted", scope)
		}
	}
}
