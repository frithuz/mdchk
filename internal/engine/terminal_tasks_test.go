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
		prospective, err := terminalGFMEndingEligible(ctx, before, budget, parser.FrontmatterOptions{})
		if err != nil || prospective != want {
			t.Fatal("actual prospective classification", text, prospective, err)
		}
		foreign, _ := parser.NewOperationBudget(parser.DefaultLimits())
		if ok, err := TerminalTaskSourcesPreserved(ctx, before, after, foreign); ok || err == nil {
			t.Fatal("foreign operation authority", ok, err)
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if ok, err := terminalGFMEndingEligible(canceled, before, budget, parser.FrontmatterOptions{}); ok || !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation", ok, err)
		}
		if err := budget.ChargeWork(1 << 60); err == nil {
			t.Fatal("expected exhaustion")
		}
		for _, proof := range []func() (bool, error){func() (bool, error) { return TerminalTaskSourcesPreserved(ctx, before, after, budget) }, func() (bool, error) {
			return terminalGFMEndingEligible(ctx, before, budget, parser.FrontmatterOptions{})
		}} {
			ok, err := proof()
			var limit *parser.LimitExceeded
			if ok || !errors.As(err, &limit) || limit.Name != "work-units" {
				t.Fatal("typed residual failure", ok, err)
			}
		}
	}
}

func TestTerminalTaskSourcesPreservedThroughEditMapping(t *testing.T) {
	ctx := context.Background()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	before := parser.ParseGFMWithBudget(ctx, parser.NewSource([]byte("##  T\n\n- [x] t\n")), budget)
	after := parser.ParseGFMWithBudget(ctx, parser.NewSource([]byte("## T\n\n- [x] t\n")), budget)
	if got, err := TerminalTaskSourcesPreserved(ctx, before, after, budget); err != nil || got {
		t.Fatal("absolute offsets accepted a shifted task", got, err)
	}
	shift := func(delta int) func(parser.Span) (parser.Span, bool, error) {
		return func(s parser.Span) (parser.Span, bool, error) {
			mapped, err := after.Source.Span(s.Start()+delta, s.End()+delta)
			return mapped, err == nil, nil
		}
	}
	if got, err := TerminalTaskSourcesPreservedMapped(ctx, before, after, budget, shift(-1)); err != nil || !got {
		t.Fatal("mapped task sources rejected", got, err)
	}
	if got, err := TerminalTaskSourcesPreservedMapped(ctx, before, after, budget, shift(-2)); err != nil || got {
		t.Fatal("wrong mapping accepted", got, err)
	}
	unmappable := func(parser.Span) (parser.Span, bool, error) { return parser.Span{}, false, nil }
	if got, err := TerminalTaskSourcesPreservedMapped(ctx, before, after, budget, unmappable); err != nil || got {
		t.Fatal("unmappable task fact accepted", got, err)
	}
}
