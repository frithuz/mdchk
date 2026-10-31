package rules

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func TestMD014ComparisonRejectsLateOriginalFacts(t *testing.T) {
	for _, kind := range []string{"absent", "foreign", "wrong-byte", "wide", "overlap"} {
		t.Run(kind, func(t *testing.T) {
			called := false
			r := registry(t, []engine.Registration{{ID: "MD014", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				items, e := c.ShellPrompts(ctx)
				if e != nil || len(items.Dollars) != 2 {
					t.Fatal(items, e)
				}
				pre := compareShellPrompts(ctx, c, items.Dollars)
				if pre.Err != nil || len(pre.Findings) != 2 {
					t.Fatal("actual positive precondition", pre)
				}
				var bad parser.Span
				switch kind {
				case "absent":
				case "foreign":
					s := parser.NewSource([]byte("$"))
					bad, _ = s.Span(0, 1)
				case "wrong-byte":
					bad, _ = c.Source().Span(13, 14)
				case "wide":
					bad, _ = c.Source().Span(12, 14)
				case "overlap":
					bad = items.Dollars[0]
				default:
					t.Fatal(kind)
				}
				items.Dollars[1] = bad
				out := compareShellPrompts(ctx, c, items.Dollars)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				called = true
				return out
			}}})
			analyzeHeadings(t, r, "```sh\n$ pwd\n$ whoami\n```\n", md014Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if !called {
				t.Fatal("callback absent")
			}
		})
	}
}
func TestMD014OriginalStickyCallbackControls(t *testing.T) {
	for _, source := range []string{"", "clean", "`code`", "```sh\n$ pwd\nout\n```\n", "```sh\n$ pwd\n```\n", "```sh\n$ pwd\n```\n\n<div>\n<x a=\"\n\n```sh\n$ pwd\n```\n"} {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			t.Run(source+"/"+kind, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					b, _ := parser.NewOperationBudget(parser.DefaultLimits())
					called := false
					r := registry(t, []engine.Registration{{ID: "MD014", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						switch kind {
						case "work":
							_ = b.ChargeWork(b.Limits().WorkUnits)
						case "nodes":
							_ = b.ChargeNodes(b.Limits().TotalNodes)
						case "elapsed":
							time.Sleep(61 * time.Second)
						}
						var out engine.RuleOutcome
						for range 2 {
							out = shellPrompts(ctx, c)
							var limit *parser.LimitExceeded
							if !errors.As(out.Err, &limit) || out.Findings != nil || out.IncompleteReason != "" {
								t.Fatal(out)
							}
						}
						called = true
						return out
					}}})
					analyzeHeadings(t, r, source, md014Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: b})
					if !called {
						t.Fatal("callback absent")
					}
				})
			})
		}
	}
}

// Function identities come from held source, not sampled checkpoint counters.
// The context interrupts only a named deferred publication gate; it also proves
// that gate was reached after the actual positive comparison precondition.
type md014PublicationStop struct {
	context.Context
	gate    string
	reason  error
	reached bool
}

func (c *md014PublicationStop) Err() error {
	if c.gate == "" {
		return c.Context.Err()
	}
	var pcs [32]uintptr
	n := runtime.Callers(1, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if strings.HasSuffix(frame.Function, c.gate) {
			c.reached = true
			return c.reason
		}
		if !more {
			break
		}
	}
	return c.Context.Err()
}
func TestMD014ComparisonAndDistinctOuterFinalControls(t *testing.T) {
	for _, source := range []string{"", "clean", "`code`", "```sh\n$ pwd\nout\n```\n", "```sh\n$ pwd\n```\n", "```sh\n$ pwd\n```\n\n<div>\n<x a=\"\n\n```sh\n$ pwd\n```\n"} {
		for _, whole := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%q/whole-%v/origin-%v/%v", source, whole, originating, reason), func(t *testing.T) {
						origin := &md014PublicationStop{Context: context.Background(), reason: reason}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD014", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							items, e := c.ShellPrompts(ctx)
							if e != nil {
								t.Fatal(e)
							}
							if source == "```sh\n$ pwd\n```\n" {
								pre := compareShellPrompts(ctx, c, items.Dollars)
								if pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 6 || pre.Findings[0].Span.End() != 7 {
									t.Fatal("actual positive precondition", pre)
								}
							}
							gate := ".compareShellPrompts.func1"
							if whole {
								gate = ".shellPrompts.func1"
							}
							caller := &md014PublicationStop{Context: context.Background(), gate: gate, reason: reason}
							var target context.Context = caller
							if originating {
								origin.gate = gate
								target = context.Background()
							}
							var out engine.RuleOutcome
							if whole {
								out = shellPrompts(target, c)
							} else {
								out = compareShellPrompts(target, c, items.Dollars)
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
						in, e := engine.NewInput("literal.md", []byte(source), md014Settings(), engine.DocumentContext{})
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

// Exhaustion is injected at the actual deferred outer gate after production
// evaluation. It uses the original allowance and no measured setup subtraction.
type md014FinalQuota struct {
	context.Context
	budget         *parser.OperationBudget
	kind           string
	gate           string
	armed, reached bool
}

func (c *md014FinalQuota) Err() error {
	if c.armed {
		var pcs [32]uintptr
		n := runtime.Callers(1, pcs[:])
		frames := runtime.CallersFrames(pcs[:n])
		for {
			f, more := frames.Next()
			gate := c.gate
			if gate == "" {
				gate = ".shellPrompts.func1"
			}
			if strings.HasSuffix(f.Function, gate) {
				c.reached = true
				if c.kind == "elapsed" {
					c.armed = false
					time.Sleep(61 * time.Second)
				} else if c.kind == "nodes" {
					_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes)
				} else {
					_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
				}
				break
			}
			if !more {
				break
			}
		}
	}
	return c.Context.Err()
}

func TestMD014ComparisonAndOuterFinalStickyDiscard(t *testing.T) {
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		for _, outer := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/outer-%v/origin-%v", kind, outer, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
						gate := ".compareShellPrompts.func1"
						if outer {
							gate = ".shellPrompts.func1"
						}
						origin := &md014FinalQuota{Context: context.Background(), budget: budget, kind: kind, gate: gate}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD014", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							items, e := c.ShellPrompts(ctx)
							pre := compareShellPrompts(ctx, c, items.Dollars)
							if e != nil || pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 6 {
								t.Fatal("positive precondition", pre, e)
							}
							caller := &md014FinalQuota{Context: context.Background(), budget: budget, kind: kind, gate: gate, armed: true}
							var target context.Context = caller
							if originating {
								origin.armed = true
								target = context.Background()
							}
							var out engine.RuleOutcome
							if outer {
								out = shellPrompts(target, c)
							} else {
								out = compareShellPrompts(target, c, items.Dollars)
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
						in, e := engine.NewInput("literal.md", []byte("```sh\n$ pwd\n```\n"), md014Settings(), engine.DocumentContext{})
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

func TestMD014DistinctOuterFinalStickyQuotaDiscard(t *testing.T) {
	for _, source := range []string{"", "clean", "```sh\n$ pwd\nout\n```\n", "```sh\n$ pwd\n```\n", "```sh\n$ pwd\n```\n\n<div>\n<x title=\"\n\n```sh\n$ pwd\n```\n"} {
		for _, kind := range []string{"work", "nodes"} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%q/%s/origin-%v", source, kind, originating), func(t *testing.T) {
					b, _ := parser.NewOperationBudget(parser.DefaultLimits())
					origin := &md014FinalQuota{Context: context.Background(), budget: b, kind: kind}
					called := false
					r := registry(t, []engine.Registration{{ID: "MD014", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						pre := shellPrompts(ctx, c)
						if pre.Err != nil {
							t.Fatal(pre)
						}
						if source == "```sh\n$ pwd\n```\n" && (len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 6 || pre.Findings[0].Span.End() != 7) {
							t.Fatal("positive precondition", pre)
						}
						caller := &md014FinalQuota{Context: context.Background(), budget: b, kind: kind, armed: true}
						var target context.Context = caller
						if originating {
							origin.armed = true
							target = context.Background()
						}
						out := shellPrompts(target, c)
						var stop *parser.LimitExceeded
						reached := caller.reached
						if originating {
							reached = origin.reached
						}
						if !reached || !errors.As(out.Err, &stop) || out.Findings != nil || out.IncompleteReason != "" {
							t.Fatal(out, reached)
						}
						called = true
						return out
					}}})
					in, err := engine.NewInput("literal.md", []byte(source), md014Settings(), engine.DocumentContext{})
					if err != nil {
						t.Fatal(err)
					}
					out, err := r.Lint(origin, []engine.Input{in}, engine.RunOptions{Budget: b})
					if err != nil || !called || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) == 0 {
						t.Fatal(out, err, called)
					}
				})
			}
		}
	}
}

func TestMD014MD087InterruptedSingleObservationRemainsUncertain(t *testing.T) {
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	calls := 0
	regs := Registrations()
	for i := range regs {
		if regs[i].ID == "MD014" {
			actual := regs[i].Check
			regs[i].Check = func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				calls++
				child, cancel := context.WithCancel(ctx)
				cancel()
				return actual(child, c)
			}
		}
	}
	s := md014Settings()
	on := true
	s["MD087"] = engine.RuleSetting{Enabled: &on}
	in, err := engine.NewInput("literal.md", []byte("<!-- mdchk:disable MD014 -->\n```sh\n$ pwd\n```\n"), s, engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := registry(t, regs).Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Budget: b})
	if err != nil || calls != 1 || out.Complete || len(out.Diagnostics)+len(out.SuppressedDiagnostics) != 0 || len(out.Interruptions) == 0 {
		t.Fatal(out, err, calls)
	}
	var decisions []engine.SuppressionDecision
	for _, record := range out.AnalysisRecords() {
		if a, ok := record.(engine.DirectiveAnalysis); ok {
			rows, e := a.SuppressionDecisions(context.Background(), b)
			if e != nil {
				t.Fatal(e)
			}
			decisions = append(decisions, rows...)
		}
	}
	if len(decisions) != 1 || decisions[0].Rule != "MD014" || decisions[0].Reason != "suppression-uncertain" {
		t.Fatal(decisions)
	}
}

func TestMD014InvalidUTF8Admission(t *testing.T) {
	s := md014Settings()
	off := false
	s["MD094"] = engine.RuleSetting{Enabled: &off}
	calls := 0
	regs := Registrations()
	for i := range regs {
		if regs[i].ID == "MD014" {
			regs[i].Check = func(context.Context, engine.RuleContext) engine.RuleOutcome {
				calls++
				t.Fatal("UTF8 gate bypassed")
				return engine.RuleOutcome{}
			}
		}
	}
	r := registry(t, regs)
	source := []byte{'<', 'd', 'i', 'v', '>', '\n', 0xff}
	before := append([]byte(nil), source...)
	in, err := engine.NewInput("invalid.md", source, s, engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil || len(out.Errors) != 1 || out.Errors[0].Category != result.IO || out.Errors[0].Code != "invalid-utf8" || out.Errors[0].Rule != "" || out.Errors[0].ByteOffset == nil || *out.Errors[0].ByteOffset != 6 || len(out.Diagnostics) != 0 || calls != 0 || !reflect.DeepEqual(source, before) {
		t.Fatal(out, err)
	}
}
