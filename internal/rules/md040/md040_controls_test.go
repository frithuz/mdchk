package md040_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules/md040"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

func TestMD040ComparisonAndDistinctOuterFinalControls(t *testing.T) {
	for _, source := range []string{"", "clean", "`code`", "```sh\n$ pwd\nout\n```\n", "x\n```\n```", "x\n```\n```\n<div>\n<x a=\"\n\nx\n```\n```"} {
		for _, whole := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%q/whole-%v/origin-%v/%v", source, whole, originating, reason), func(t *testing.T) {
						origin := &ruletest.GateStopContext{Context: context.Background(), Reason: reason}
						called := false
						r := ruletest.Registry(t, []engine.Registration{{ID: "MD040", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							items, e := c.FencedLanguages(ctx)
							if e != nil {
								t.Fatal(e)
							}
							if source == "x\n```\n```" {
								pre := md040.CompareFencedBlockLanguages(ctx, c, items.Facts)
								if pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 5 || pre.Findings[0].Span.End() != 5 {
									t.Fatal("actual positive precondition", pre)
								}
							}
							gate := ".compareFencedBlockLanguages.func1"
							if whole {
								gate = ".fencedBlockLanguages.func1"
							}
							caller := &ruletest.GateStopContext{Context: context.Background(), Gate: gate, Reason: reason}
							var target context.Context = caller
							if originating {
								origin.Gate = gate
								target = context.Background()
							}
							var out engine.RuleOutcome
							if whole {
								out = md040.FencedBlockLanguages(target, c)
							} else {
								out = md040.CompareFencedBlockLanguages(target, c, items.Facts)
							}
							reached := caller.Reached
							if originating {
								reached = origin.Reached
							}
							if !reached || !errors.Is(out.Err, reason) || out.Findings != nil || out.IncompleteReason != "" {
								t.Fatal(out, reached)
							}
							called = true
							return out
						}}})
						in, e := engine.NewInput("literal.md", []byte(source), ruletest.Settings("MD040"), engine.DocumentContext{})
						if e != nil {
							t.Fatal(e)
						}
						_, e = r.Lint(origin, []engine.Input{in}, engine.RunOptions{})
						if e != nil || !called {
							t.Fatal(e, called)
						}
					})
				}
			}
		}
	}
}

func TestMD040ComparisonAndOuterFinalStickyDiscard(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		for _, outer := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/outer-%v/origin-%v", kind, outer, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
						gate := ".compareFencedBlockLanguages.func1"
						if outer {
							gate = ".fencedBlockLanguages.func1"
						}
						origin := &ruletest.GateQuotaContext{Context: context.Background(), Budget: budget, Kind: kind, Gate: gate}
						called := false
						r := ruletest.Registry(t, []engine.Registration{{ID: "MD040", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							items, e := c.FencedLanguages(ctx)
							pre := md040.CompareFencedBlockLanguages(ctx, c, items.Facts)
							if e != nil || pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 5 {
								t.Fatal("positive precondition", pre, e)
							}
							caller := &ruletest.GateQuotaContext{Context: context.Background(), Budget: budget, Kind: kind, Gate: gate, Armed: true}
							var target context.Context = caller
							if originating {
								origin.Armed = true
								target = context.Background()
							}
							var out engine.RuleOutcome
							if outer {
								out = md040.FencedBlockLanguages(target, c)
							} else {
								out = md040.CompareFencedBlockLanguages(target, c, items.Facts)
							}
							var stop *parser.LimitExceeded
							want := "work-units"
							if kind == "nodes" {
								want = "total-nodes"
							}
							if kind == "elapsed" {
								want = "operation-time"
							}
							if !errors.As(out.Err, &stop) || stop.Name != want || out.Findings != nil || !caller.Reached && !origin.Reached {
								t.Fatal(out, caller.Reached, origin.Reached)
							}
							called = true
							return out
						}}})
						in, e := engine.NewInput("literal.md", []byte("x\n```\n```"), ruletest.Settings("MD040"), engine.DocumentContext{})
						if e != nil {
							t.Fatal(e)
						}
						out, e := r.Lint(origin, []engine.Input{in}, engine.RunOptions{Budget: budget})
						if e != nil || !called || out.Complete || len(out.Diagnostics) != 0 {
							t.Fatal(out, e, called)
						}
					})
				})
			}
		}
	}
}

func TestMD040ComparisonRejectsLateInvalidPoints(t *testing.T) {
	for _, kind := range []string{"absent", "foreign", "wide", "utf8", "crlf", "reversed", "wrong-kind"} {
		t.Run(kind, func(t *testing.T) {
			called := false
			r := ruletest.Registry(t, []engine.Registration{{ID: "MD040", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				items, e := c.FencedLanguages(ctx)
				if e != nil || len(items.Facts) != 2 {
					t.Fatal(items, e)
				}
				pre := md040.CompareFencedBlockLanguages(ctx, c, items.Facts)
				if pre.Err != nil || len(pre.Findings) != 2 || pre.Findings[0].Span.Start() != 3 {
					t.Fatal(pre)
				}
				var bad parser.Span
				switch kind {
				case "absent":
				case "foreign":
					s := parser.NewSource([]byte("x"))
					bad, _ = s.Span(0, 0)
				case "wide":
					bad, _ = c.Source().Span(0, 1)
				case "utf8":
					bad, _ = c.Source().Span(14, 14)
				case "crlf":
					bad, _ = c.Source().Span(16, 16)
				case "reversed":
					bad, _ = c.Source().Span(0, 0)
				case "wrong-kind":
					bad = items.Facts[1].Span
					items.Facts[1].Missing = false
				}
				items.Facts[1].Span = bad
				out := md040.CompareFencedBlockLanguages(ctx, c, items.Facts)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				called = true
				return out
			}}})
			ruletest.AnalyzeDocument(t, r, "```\r\nx\r\n```\r\né\r\n~~~\r\ny\r\n~~~", ruletest.Settings("MD040"), engine.DocumentContext{}, engine.RunOptions{})
			if !called {
				t.Fatal("callback missing")
			}
		})
	}
}
