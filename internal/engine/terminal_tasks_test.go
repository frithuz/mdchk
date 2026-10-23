package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestGFMTerminalTaskProofAuthorityAndInterruptions(t *testing.T) {
	ctx := context.Background()
	for _, text := range []string{"- [x]", "- [\r\n  ]", "ordinary", "- [x] "} {
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		before := parser.ParseGFMWithBudget(ctx, parser.NewSource([]byte(text)), budget)
		after := parser.ParseGFMWithBudget(ctx, parser.NewSource([]byte(text+"\r\n")), budget)
		preserved, err := TerminalTaskSourcesPreserved(ctx, before, after, budget)
		want := text == "ordinary" || text == "- [x] "
		if err != nil || preserved != want {
			t.Fatal(text, preserved, err)
		}
		prospective, err := terminalGFMEndingEligible(ctx, before, budget)
		if err != nil || prospective != want {
			t.Fatal("actual prospective classification", text, prospective, err)
		}
		foreign, _ := parser.NewOperationBudget(parser.DefaultLimits())
		if ok, err := TerminalTaskSourcesPreserved(ctx, before, after, foreign); ok || err == nil {
			t.Fatal("foreign operation authority", ok, err)
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if ok, err := terminalGFMEndingEligible(canceled, before, budget); ok || !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation", ok, err)
		}
		if err := budget.ChargeWork(1 << 60); err == nil {
			t.Fatal("expected exhaustion")
		}
		for _, proof := range []func() (bool, error){func() (bool, error) { return TerminalTaskSourcesPreserved(ctx, before, after, budget) }, func() (bool, error) { return terminalGFMEndingEligible(ctx, before, budget) }} {
			ok, err := proof()
			var limit *parser.LimitExceeded
			if ok || !errors.As(err, &limit) || limit.Name != "work-units" {
				t.Fatal("typed residual failure", ok, err)
			}
		}
	}
}
