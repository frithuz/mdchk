//go:build mdchk_conformance

package parser_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestShellPromptsPrivateConformanceCannotGrantAuthority(t *testing.T) {
	for _, scope := range []string{"tables-only", "tasks-only", "strike-only", "bare-only", "filter-only"} {
		s := parser.NewSource([]byte("```sh\n$ pwd\n```\n"))
		b, _ := parser.NewOperationBudget(parser.DefaultLimits())
		p, e := parser.ParseConformance(context.Background(), s, b, scope)
		if e != nil || p.Status != parser.Complete {
			t.Fatal(scope, p, e)
		}
		got, e := p.Document.ShellPrompts(context.Background(), b, parser.NewLineIndex(s), parser.ParseOptions{}, []string{"sh"})
		if e == nil || got.Dollars != nil {
			t.Fatal(scope, got, e)
		}
	}
}
