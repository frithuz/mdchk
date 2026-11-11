//go:build mdchk_conformance

package parser_test

import (
	"context"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
)

func TestCJKSpacingProjectionRejectsPrivateScopes(t *testing.T) {
	for _, scope := range []string{"tables-only", "tasks-only", "strike-only", "bare-only", "filter-only"} {
		s := parser.NewSource([]byte("中A"))
		b, _ := parser.NewOperationBudget(parser.DefaultLimits())
		p, e := parser.ParseConformance(context.Background(), s, b, scope)
		if e != nil || p.Status != parser.Complete {
			t.Fatal(p, e)
		}
		if got, e := p.Document.CJKSpacing(context.Background(), b, parser.NewLineIndex(s), parser.ParseOptions{}); e == nil || got != nil {
			t.Fatal("private scope accepted", scope, got, e)
		}
	}
}

func TestCJKSpacingProjectionFullGFMProduction(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		s := parser.NewSource([]byte("中A"))
		b, _ := parser.NewOperationBudget(parser.DefaultLimits())
		options := parser.ParseOptions{GFM: gfm}
		p := parser.ParseWithOptions(context.Background(), s, b, options)
		if p.Status != parser.Complete {
			t.Fatal(p)
		}
		got, e := p.Document.CJKSpacing(context.Background(), b, parser.NewLineIndex(s), options)
		if e != nil || len(got) != 1 || got[0].Start() != 3 || got[0].End() != 3 {
			t.Fatal(got, e)
		}
	}
}
