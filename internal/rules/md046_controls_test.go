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

func TestMD046ComparisonAndDistinctOuterFinalControls(t *testing.T) {
	for _, source := range []string{"", "clean", "`code`", "```sh\n$ pwd\nout\n```\n", "x\n```\n```", "x\n```\n```\n<div>\n<x a=\"\n\nx\n```\n```"} {
		for _, whole := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%q/whole-%v/origin-%v/%v", source, whole, originating, reason), func(t *testing.T) {
						origin := &gateStopContext{Context: context.Background(), Reason: reason}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD046", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							items, e := c.CodeStyles(ctx)
							if e != nil {
								t.Fatal(e)
							}
							if source == "x\n```\n```" {
								pre := compareCodeBlockStyles(ctx, c, items.Facts)
								if pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 2 || pre.Findings[0].Span.End() != 5 {
									t.Fatal("actual positive precondition", pre)
								}
							}
							gate := ".compareCodeBlockStyles.func1"
							if whole {
								gate = ".codeBlockStyles.func1"
							}
							caller := &gateStopContext{Context: context.Background(), Gate: gate, Reason: reason}
							var target context.Context = caller
							if originating {
								origin.Gate = gate
								target = context.Background()
							}
							var out engine.RuleOutcome
							if whole {
								out = codeBlockStyles(target, c)
							} else {
								out = compareCodeBlockStyles(target, c, items.Facts)
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
						in, e := engine.NewInput("literal.md", []byte(source), md046FixedSettings(), engine.DocumentContext{})
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

func TestMD046ComparisonAndOuterFinalStickyDiscard(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		for _, outer := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/outer-%v/origin-%v", kind, outer, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
						gate := ".compareCodeBlockStyles.func1"
						if outer {
							gate = ".codeBlockStyles.func1"
						}
						origin := &gateQuotaContext{Context: context.Background(), Budget: budget, Kind: kind, Gate: gate}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD046", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							items, e := c.CodeStyles(ctx)
							pre := compareCodeBlockStyles(ctx, c, items.Facts)
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
								out = codeBlockStyles(target, c)
							} else {
								out = compareCodeBlockStyles(target, c, items.Facts)
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
						in, e := engine.NewInput("literal.md", []byte("x\n```\n```"), md046FixedSettings(), engine.DocumentContext{})
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

func TestMD046ComparisonRejectsLateInvalidPoints(t *testing.T) {
	for _, kind := range []string{"absent", "foreign", "zero-width", "utf8", "crlf", "reversed", "wrong-kind"} {
		t.Run(kind, func(t *testing.T) {
			called := false
			r := registry(t, []engine.Registration{{ID: "MD046", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				items, e := c.CodeStyles(ctx)
				if e != nil || len(items.Facts) != 2 {
					t.Fatal(items, e)
				}
				pre := compareCodeBlockStyles(ctx, c, items.Facts)
				if pre.Err != nil || len(pre.Findings) != 2 || pre.Findings[0].Span.Start() != 0 {
					t.Fatal(pre)
				}
				var bad parser.Span
				switch kind {
				case "absent":
				case "foreign":
					s := parser.NewSource([]byte("x"))
					bad, _ = s.Span(0, 0)
				case "zero-width":
					bad, _ = c.Source().Span(17, 17)
				case "utf8":
					bad, _ = c.Source().Span(14, 15)
				case "crlf":
					bad, _ = c.Source().Span(16, 17)
				case "reversed":
					bad, _ = c.Source().Span(0, 0)
				case "wrong-kind":
					bad = items.Facts[1].Span
					items.Facts[1].Expected = "consistent"
				}
				items.Facts[1].Span = bad
				out := compareCodeBlockStyles(ctx, c, items.Facts)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				called = true
				return out
			}}})
			analyzeHeadings(t, r, "```\r\nx\r\n```\r\né\r\n~~~\r\ny\r\n~~~", md046FixedSettings(), engine.DocumentContext{}, engine.RunOptions{})
			if !called {
				t.Fatal("callback missing")
			}
		})
	}
}

func md046FixedSettings() engine.Settings {
	s := onlyMD046Settings()
	s["MD046"] = engine.RuleSetting{Options: engine.Options{"style": "indented"}}
	return s
}
