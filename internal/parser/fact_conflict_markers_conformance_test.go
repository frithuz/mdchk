//go:build mdchk_conformance

package parser_test

import (
	"context"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
)

func TestConflictMarkersPrivateConformanceCannotGrantAuthority(t *testing.T) {
	for _, scope := range []string{"tables-only", "tasks-only", "strike-only", "bare-only", "filter-only"} {
		s := parser.NewSource([]byte("<<<<<<<"))
		b, _ := parser.NewOperationBudget(parser.DefaultLimits())
		p, e := parser.ParseConformance(context.Background(), s, b, scope)
		if e != nil || p.Status != parser.Complete {
			t.Fatal(scope, p, e)
		}
		got, e := p.Document.ConflictMarkers(context.Background(), b, parser.NewLineIndex(s), parser.ParseOptions{})
		if e == nil || got.Markers != nil {
			t.Fatal(scope, got, e)
		}
	}
}
