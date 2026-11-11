package md070_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules/md070"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

func TestMD070ComparisonRejectsLateInvalidPoints(t *testing.T) {
	for _, fault := range []string{"absent", "foreign", "zero-width-length", "reversed", "utf8", "crlf", "wrong-kind"} {
		t.Run(fault, func(t *testing.T) {
			called := false
			r := ruletest.Registry(t, []engine.Registration{{ID: "MD070", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				items, err := c.NestedExamples(ctx)
				if err != nil || len(items.Facts) != 2 {
					t.Fatal(items, err)
				}
				pre := md070.CompareNestedMarkdownExamples(ctx, c, items.Facts)
				if pre.Err != nil || len(pre.Findings) != 2 || pre.Findings[0].Span.Start() != 23 {
					t.Fatal("positive", pre)
				}
				var bad parser.Span
				switch fault {
				case "foreign":
					s := parser.NewSource(c.Source().Bytes())
					bad, _ = s.Span(93, 96)
				case "zero-width-length":
					bad, _ = c.Source().Span(93, 93)
				case "reversed":
					bad, _ = c.Source().Span(23, 26)
				case "utf8":
					bad, _ = c.Source().Span(67, 68)
				case "crlf":
					bad, _ = c.Source().Span(69, 70)
				case "wrong-kind":
					bad = items.Facts[1].Span
					items.Facts[1].Kind = "other"
				}
				items.Facts[1].Span = bad
				out := md070.CompareNestedMarkdownExamples(ctx, c, items.Facts)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				called = true
				return out
			}}})
			ruletest.AnalyzeDocument(t, r, md070Example+"\n\né\r\n"+md070Example, md070Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if !called {
				t.Fatal("callback missing")
			}
		})
	}
}

func TestMD070ComparisonAndDistinctOuterFinalControls(t *testing.T) {
	for _, source := range []string{"", "clean", "`code`", "```sh\n$ pwd\nout\n```\n", "<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->", "<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->\n<div>\n<x a=\"\n\n<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->"} {
		for _, whole := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%q/whole-%v/origin-%v/%v", source, whole, originating, reason), func(t *testing.T) {
						origin := &ruletest.GateStopContext{Context: context.Background(), Reason: reason}
						called := false
						r := ruletest.Registry(t, []engine.Registration{{ID: "MD070", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							items, e := c.NestedExamples(ctx)
							if e != nil {
								t.Fatal(e)
							}
							if source == "<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->" {
								pre := md070.CompareNestedMarkdownExamples(ctx, c, items.Facts)
								if pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 23 || pre.Findings[0].Span.End() != 26 {
									t.Fatal("actual positive precondition", pre)
								}
							}
							gate := ".compareNestedMarkdownExamples.func1"
							if whole {
								gate = ".nestedMarkdownExamples.func1"
							}
							caller := &ruletest.GateStopContext{Context: context.Background(), Gate: gate, Reason: reason}
							var target context.Context = caller
							if originating {
								origin.Gate = gate
								target = context.Background()
							}
							var out engine.RuleOutcome
							if whole {
								out = md070.NestedMarkdownExamples(target, c)
							} else {
								out = md070.CompareNestedMarkdownExamples(target, c, items.Facts)
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
						in, e := engine.NewInput("literal.md", []byte(source), md070Settings(), engine.DocumentContext{})
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

func TestMD070ComparisonAndOuterFinalStickyDiscard(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		for _, outer := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/outer-%v/origin-%v", kind, outer, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
						gate := ".compareNestedMarkdownExamples.func1"
						if outer {
							gate = ".nestedMarkdownExamples.func1"
						}
						origin := &ruletest.GateQuotaContext{Context: context.Background(), Budget: budget, Kind: kind, Gate: gate}
						called := false
						r := ruletest.Registry(t, []engine.Registration{{ID: "MD070", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							items, e := c.NestedExamples(ctx)
							pre := md070.CompareNestedMarkdownExamples(ctx, c, items.Facts)
							if e != nil || pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 23 {
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
								out = md070.NestedMarkdownExamples(target, c)
							} else {
								out = md070.CompareNestedMarkdownExamples(target, c, items.Facts)
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
						in, e := engine.NewInput("literal.md", []byte("<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->"), md070Settings(), engine.DocumentContext{})
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
