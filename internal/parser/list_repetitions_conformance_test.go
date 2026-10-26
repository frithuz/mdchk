//go:build mdchk_conformance

package parser_test

import (
	"context"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
)

func TestListRepetitionsProjectionRejectsPrivateScopes(t *testing.T) {
	for _, scope := range []string{"tables-only", "tasks-only", "strike-only", "bare-only", "filter-only"} {
		s := parser.NewSource([]byte("- - H"))
		b, _ := parser.NewOperationBudget(parser.DefaultLimits())
		p, e := parser.ParseConformance(context.Background(), s, b, scope)
		if e != nil || p.Status != parser.Complete {
			t.Fatal(p, e)
		}
		if got, e := p.Document.ListRepetitions(context.Background(), b, parser.NewLineIndex(s), parser.ParseOptions{}); e == nil || got != nil {
			t.Fatal("private scope accepted", scope, got, e)
		}
	}
}

func TestListRepetitionsProjectionFullGFMProduction(t *testing.T) {
	s := parser.NewSource([]byte("- - Text"))
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	options := parser.ParseOptions{GFM: true}
	p := parser.ParseWithOptions(context.Background(), s, b, options)
	if p.Status != parser.Complete {
		t.Fatal(p)
	}
	got, e := p.Document.ListRepetitions(context.Background(), b, parser.NewLineIndex(s), options)
	if e != nil || len(got) != 1 || got[0].Start() != 0 || got[0].End() != 3 {
		t.Fatal(got, e)
	}
}

func TestExistingListProjectionsFullGFMProduction(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		s := parser.NewSource([]byte("Text\n- - Text"))
		b, _ := parser.NewOperationBudget(parser.DefaultLimits())
		options := parser.ParseOptions{GFM: gfm}
		p := parser.ParseWithOptions(context.Background(), s, b, options)
		if p.Status != parser.Complete {
			t.Fatal(p)
		}
		index := parser.NewLineIndex(s)
		items, e := p.Document.ListItems(context.Background(), b, index, options)
		if e != nil || len(items) != 2 {
			t.Fatal("ListItems production", items, e)
		}
		separators, e := p.Document.ListSeparators(context.Background(), b, index, options)
		if e != nil || len(separators) != 2 {
			t.Fatal("ListSeparators production", separators, e)
		}
		spacing, e := p.Document.ListSpacing(context.Background(), b, index, options)
		if e != nil || len(spacing) != 1 {
			t.Fatal("ListSpacing production", spacing, e)
		}
	}
}
