package rules

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func TestMD040ComparisonAndDistinctOuterFinalControls(t *testing.T) {
	for _, source := range []string{"", "clean", "`code`", "```sh\n$ pwd\nout\n```\n", "x\n```\n```", "x\n```\n```\n<div>\n<x a=\"\n\nx\n```\n```"} {
		for _, whole := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%q/whole-%v/origin-%v/%v", source, whole, originating, reason), func(t *testing.T) {
						origin := &gateStopContext{Context: context.Background(), reason: reason}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD040", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							items, e := c.FencedLanguages(ctx)
							if e != nil {
								t.Fatal(e)
							}
							if source == "x\n```\n```" {
								pre := compareFencedBlockLanguages(ctx, c, items.Facts)
								if pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 5 || pre.Findings[0].Span.End() != 5 {
									t.Fatal("actual positive precondition", pre)
								}
							}
							gate := ".compareFencedBlockLanguages.func1"
							if whole {
								gate = ".fencedBlockLanguages.func1"
							}
							caller := &gateStopContext{Context: context.Background(), gate: gate, reason: reason}
							var target context.Context = caller
							if originating {
								origin.gate = gate
								target = context.Background()
							}
							var out engine.RuleOutcome
							if whole {
								out = fencedBlockLanguages(target, c)
							} else {
								out = compareFencedBlockLanguages(target, c, items.Facts)
							}
							reached := caller.reached
							if originating {
								reached = origin.reached
							}
							if !reached || !errors.Is(out.Err, reason) || out.Findings != nil || out.IncompleteReason != "" {
								t.Fatal(out, reached)
							}
							called = true
							return out
						}}})
						in, e := engine.NewInput("literal.md", []byte(source), onlyMD040Settings(), engine.DocumentContext{})
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
						origin := &gateQuotaContext{Context: context.Background(), budget: budget, kind: kind, gate: gate}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD040", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							items, e := c.FencedLanguages(ctx)
							pre := compareFencedBlockLanguages(ctx, c, items.Facts)
							if e != nil || pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 5 {
								t.Fatal("positive precondition", pre, e)
							}
							caller := &gateQuotaContext{Context: context.Background(), budget: budget, kind: kind, gate: gate, armed: true}
							var target context.Context = caller
							if originating {
								origin.armed = true
								target = context.Background()
							}
							var out engine.RuleOutcome
							if outer {
								out = fencedBlockLanguages(target, c)
							} else {
								out = compareFencedBlockLanguages(target, c, items.Facts)
							}
							var stop *parser.LimitExceeded
							want := "work-units"
							if kind == "nodes" {
								want = "total-nodes"
							}
							if kind == "elapsed" {
								want = "operation-time"
							}
							if !errors.As(out.Err, &stop) || stop.Name != want || out.Findings != nil || !caller.reached && !origin.reached {
								t.Fatal(out, caller.reached, origin.reached)
							}
							called = true
							return out
						}}})
						in, e := engine.NewInput("literal.md", []byte("x\n```\n```"), onlyMD040Settings(), engine.DocumentContext{})
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
			r := registry(t, []engine.Registration{{ID: "MD040", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				items, e := c.FencedLanguages(ctx)
				if e != nil || len(items.Facts) != 2 {
					t.Fatal(items, e)
				}
				pre := compareFencedBlockLanguages(ctx, c, items.Facts)
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
				out := compareFencedBlockLanguages(ctx, c, items.Facts)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				called = true
				return out
			}}})
			analyzeHeadings(t, r, "```\r\nx\r\n```\r\né\r\n~~~\r\ny\r\n~~~", onlyMD040Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if !called {
				t.Fatal("callback missing")
			}
		})
	}
}
