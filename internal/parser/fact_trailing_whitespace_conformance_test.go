//go:build mdchk_conformance

package parser_test

import (
	"context"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
)

func TestTrailingWhitespaceProjectionRejectsPrivateScopes(t *testing.T) {
	for _, scope := range []string{"tables-only", "tasks-only", "strike-only", "bare-only", "filter-only"} {
		s := parser.NewSource([]byte("- H"))
		b, _ := parser.NewOperationBudget(parser.DefaultLimits())
		p, e := parser.ParseConformance(context.Background(), s, b, scope)
		if e != nil || p.Status != parser.Complete {
			t.Fatal(p, e)
		}
		if got, e := p.Document.TrailingWhitespace(context.Background(), b, parser.NewLineIndex(s), parser.ParseOptions{}, 2, false, false); e == nil || got != nil {
			t.Fatal("private scope accepted", scope, got, e)
		}
	}
}

func TestTrailingWhitespaceProjectionFullGFMProduction(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		s := parser.NewSource([]byte("a \n"))
		b, _ := parser.NewOperationBudget(parser.DefaultLimits())
		options := parser.ParseOptions{GFM: gfm}
		p := parser.ParseWithOptions(context.Background(), s, b, options)
		if p.Status != parser.Complete {
			t.Fatal(p)
		}
		got, e := p.Document.TrailingWhitespace(context.Background(), b, parser.NewLineIndex(s), options, 2, false, false)
		if e != nil || len(got) != 1 || got[0].Start() != 1 || got[0].End() != 2 {
			t.Fatal(got, e)
		}
	}
}
