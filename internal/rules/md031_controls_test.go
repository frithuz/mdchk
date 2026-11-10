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

func TestMD031ComparisonAndDistinctOuterFinalControls(t *testing.T) {
	for _, source := range []string{"", "clean", "`code`", "```sh\n$ pwd\nout\n```\n", "x\n```\n```", "x\n```\n```\n<div>\n<x a=\"\n\nx\n```\n```"} {
		for _, whole := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%q/whole-%v/origin-%v/%v", source, whole, originating, reason), func(t *testing.T) {
						origin := &gateStopContext{Context: context.Background(), Reason: reason}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD031", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							items, e := c.FencedSpacing(ctx)
							if e != nil {
								t.Fatal(e)
							}
							if source == "x\n```\n```" {
								pre := compareFencedBlockBlankLines(ctx, c, items.Facts)
								if pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 2 || pre.Findings[0].Span.End() != 2 {
									t.Fatal("actual positive precondition", pre)
								}
							}
							gate := ".compareFencedBlockBlankLines.func1"
							if whole {
								gate = ".fencedBlockBlankLines.func1"
							}
							caller := &gateStopContext{Context: context.Background(), Gate: gate, Reason: reason}
							var target context.Context = caller
							if originating {
								origin.Gate = gate
								target = context.Background()
							}
							var out engine.RuleOutcome
							if whole {
								out = fencedBlockBlankLines(target, c)
							} else {
								out = compareFencedBlockBlankLines(target, c, items.Facts)
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
						in, e := engine.NewInput("literal.md", []byte(source), onlyMD031Settings(), engine.DocumentContext{})
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

func TestMD031ComparisonAndOuterFinalStickyDiscard(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		for _, outer := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/outer-%v/origin-%v", kind, outer, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
						gate := ".compareFencedBlockBlankLines.func1"
						if outer {
							gate = ".fencedBlockBlankLines.func1"
						}
						origin := &gateQuotaContext{Context: context.Background(), Budget: budget, Kind: kind, Gate: gate}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD031", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							items, e := c.FencedSpacing(ctx)
							pre := compareFencedBlockBlankLines(ctx, c, items.Facts)
							if e != nil || pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 2 {
								t.Fatal("positive precondition", pre, e)
							}
							caller := &gateQuotaContext{Context: context.Background(), Budget: budget, Kind: kind, Gate: gate, Armed: true}
							var target context.Context = caller
							if originating {
								origin.Armed = true
								target = context.Background()
							}
							var out engine.RuleOutcome
							if outer {
								out = fencedBlockBlankLines(target, c)
							} else {
								out = compareFencedBlockBlankLines(target, c, items.Facts)
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
						in, e := engine.NewInput("literal.md", []byte("x\n```\n```"), onlyMD031Settings(), engine.DocumentContext{})
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

func TestMD031ComparisonRejectsLateInvalidPoints(t *testing.T) {
	for _, kind := range []string{"absent", "foreign", "wide", "utf8", "crlf"} {
		t.Run(kind, func(t *testing.T) {
			called := false
			r := registry(t, []engine.Registration{{ID: "MD031", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				items, e := c.FencedSpacing(ctx)
				if e != nil || len(items.Facts) != 2 {
					t.Fatal(items, e)
				}
				pre := compareFencedBlockBlankLines(ctx, c, items.Facts)
				if pre.Err != nil || len(pre.Findings) != 2 {
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
					bad, _ = c.Source().Span(1, 1)
				case "crlf":
					bad, _ = c.Source().Span(3, 3)
				}
				items.Facts[1].Point = bad
				out := compareFencedBlockBlankLines(ctx, c, items.Facts)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				called = true
				return out
			}}})
			analyzeHeadings(t, r, "é\r\n```\r\nx\r\n```\r\ny", onlyMD031Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if !called {
				t.Fatal("callback missing")
			}
		})
	}
}
