//go:build mdchk_conformance

package parser_test

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
)

func TestConfiguredNamesActualProductionAndPrivateScopes(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			source := parser.NewSource([]byte("github"))
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			options := parser.ParseOptions{GFM: flavor == "gfm"}
			p := parser.ParseWithOptions(context.Background(), source, budget, options)
			if p.Status != parser.Complete {
				t.Fatal(p)
			}
			got, err := p.Document.ConfiguredNames(context.Background(), budget, parser.NewLineIndex(source), options, []string{"GitHub"}, false)
			if err != nil || len(got) != 1 || got[0].Start() != 0 || got[0].End() != 6 {
				t.Fatal(got, err)
			}
		})
	}
	for _, scope := range []string{"tables-only", "tasks-only", "strike-only", "bare-only", "filter-only"} {
		t.Run(scope, func(t *testing.T) {
			source := parser.NewSource([]byte("github"))
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			p, err := parser.ParseConformance(context.Background(), source, budget, scope)
			if err != nil || p.Status != parser.Complete {
				t.Fatal(p, err)
			}
			got, err := p.Document.ConfiguredNames(context.Background(), budget, parser.NewLineIndex(source), parser.ParseOptions{}, []string{"GitHub"}, false)
			if !errors.Is(err, parser.ErrModel) || got != nil {
				t.Fatal(got, err)
			}
		})
	}
}
