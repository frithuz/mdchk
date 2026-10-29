//go:build mdchk_conformance

package parser_test

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
)

func TestDelimiterStyleActualProductionAndPrivateScopes(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			source := parser.NewSource([]byte("A **valid**\n\nA * x *"))
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			options := parser.ParseOptions{GFM: flavor == "gfm"}
			parsed := parser.ParseWithOptions(context.Background(), source, budget, options)
			if parsed.Status != parser.Complete {
				t.Fatal(parsed)
			}
			got, err := parsed.Document.DelimiterStyle(context.Background(), budget, parser.NewLineIndex(source), options)
			if err != nil || len(got.Valid) != 1 || len(got.Whitespace) != 2 {
				t.Fatal(got, err)
			}
		})
	}
	for _, scope := range []string{"tables-only", "tasks-only", "strike-only", "bare-only", "filter-only"} {
		t.Run(scope, func(t *testing.T) {
			source := parser.NewSource([]byte("A * x *"))
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			parsed, err := parser.ParseConformance(context.Background(), source, budget, scope)
			if err != nil || parsed.Status != parser.Complete {
				t.Fatal(parsed, err)
			}
			got, err := parsed.Document.DelimiterStyle(context.Background(), budget, parser.NewLineIndex(source), parser.ParseOptions{})
			if !errors.Is(err, parser.ErrModel) || got != nil {
				t.Fatal(got, err)
			}
		})
	}
}
