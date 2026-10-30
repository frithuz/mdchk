//go:build mdchk_conformance

package parser_test

import (
	"context"
	"errors"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestASCIIPunctuationRejectsPrivateScopes(t *testing.T) {
	for _, scope := range []string{"tables-only", "tasks-only", "strike-only", "bare-only", "filter-only"} {
		t.Run(scope, func(t *testing.T) {
			s := parser.NewSource([]byte("‘’“”–—"))
			b, e := parser.NewOperationBudget(parser.DefaultLimits())
			if e != nil {
				t.Fatal(e)
			}
			p, e := parser.ParseConformance(context.Background(), s, b, scope)
			if e != nil || p.Status != parser.Complete {
				t.Fatal(p, e)
			}
			got, e := p.Document.ASCIIPunctuation(context.Background(), b, parser.NewLineIndex(s), parser.ParseOptions{})
			if !errors.Is(e, parser.ErrModel) || got != nil {
				t.Fatal(got, e)
			}
		})
	}
}
