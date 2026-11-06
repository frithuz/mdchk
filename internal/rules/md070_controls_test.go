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

func TestMD070ComparisonRejectsLateInvalidPoints(t *testing.T) {
	for _, fault := range []string{"absent", "foreign", "zero-width-length", "reversed", "utf8", "crlf", "wrong-kind"} {
		t.Run(fault, func(t *testing.T) {
			called := false
			r := registry(t, []engine.Registration{{ID: "MD070", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				items, err := c.NestedExamples(ctx)
				if err != nil || len(items.Facts) != 2 {
					t.Fatal(items, err)
				}
				pre := compareNestedMarkdownExamples(ctx, c, items.Facts)
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
				out := compareNestedMarkdownExamples(ctx, c, items.Facts)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				called = true
				return out
			}}})
			analyzeHeadings(t, r, md070Example+"\n\né\r\n"+md070Example, md070Settings(), engine.DocumentContext{}, engine.RunOptions{})
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
						origin := &gateStopContext{Context: context.Background(), reason: reason}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD070", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							items, e := c.NestedExamples(ctx)
							if e != nil {
								t.Fatal(e)
							}
							if source == "<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->" {
								pre := compareNestedMarkdownExamples(ctx, c, items.Facts)
								if pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 23 || pre.Findings[0].Span.End() != 26 {
									t.Fatal("actual positive precondition", pre)
								}
							}
							gate := ".compareNestedMarkdownExamples.func1"
							if whole {
								gate = ".nestedMarkdownExamples.func1"
							}
							caller := &gateStopContext{Context: context.Background(), gate: gate, reason: reason}
							var target context.Context = caller
							if originating {
								origin.gate = gate
								target = context.Background()
							}
							var out engine.RuleOutcome
							if whole {
								out = nestedMarkdownExamples(target, c)
							} else {
								out = compareNestedMarkdownExamples(target, c, items.Facts)
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
						origin := &gateQuotaContext{Context: context.Background(), budget: budget, kind: kind, gate: gate}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD070", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							items, e := c.NestedExamples(ctx)
							pre := compareNestedMarkdownExamples(ctx, c, items.Facts)
							if e != nil || pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 23 {
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
								out = nestedMarkdownExamples(target, c)
							} else {
								out = compareNestedMarkdownExamples(target, c, items.Facts)
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
