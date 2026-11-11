//go:build mdchk_conformance

package parser_test

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
)

func TestThematicStyleRejectsPrivateScopes(t *testing.T) {
	for _, scope := range []string{"tables-only", "tasks-only", "strike-only", "bare-only", "filter-only"} {
		t.Run(scope, func(t *testing.T) {
			source := parser.NewSource([]byte("---\n\n***"))
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			parsed, err := parser.ParseConformance(context.Background(), source, budget, scope)
			if err != nil || parsed.Status != parser.Complete {
				t.Fatal(parsed, err)
			}
			got, err := parsed.Document.ThematicStyle(context.Background(), budget, parser.NewLineIndex(source), parser.ParseOptions{}, "consistent")
			if !errors.Is(err, parser.ErrModel) || got != nil {
				t.Fatal(got, err)
			}
		})
	}
}
