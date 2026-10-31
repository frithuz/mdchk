package engine

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

func md014ShapeContext(t *testing.T, source string, origin context.Context) RuleContext {
	t.Helper()
	in, err := NewInput("literal.md", []byte(source), nil, DocumentContext{Flavor: "commonmark"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	p := parser.ParseWithOptions(origin, in.source, b, in.documentContext.ParseOptions())
	if p.Status != parser.Complete {
		t.Fatal(p)
	}
	return RuleContext{options: Options{"languages": []string{"sh"}}, input: in, index: parser.NewLineIndex(in.source), document: p.Document, parsed: p, budget: b, operation: origin, syntaxReliable: true}
}

func TestRuleContextShellPromptsOriginalAuthority(t *testing.T) {
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("```sh\n$ pwd\n```\n")) }, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("```sh\n$ pwd\n```\n"))) }, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte("```sh\n$ pwd\n```\n"), nil, DocumentContext{})
		}, func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		}, func(c *RuleContext) { c.input.documentContext.Flavor = "gfm" }, func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) }, func(c *RuleContext) { c.options = Options{"unknown": false} }, func(c *RuleContext) { c.options = Options{"allowed_tags": []string{}} },
		func(c *RuleContext) { c.options = Options{} }, func(c *RuleContext) { c.options = Options{"languages": nil} }, func(c *RuleContext) { c.options = Options{"languages": "sh"} },
	} {
		c := md014ShapeContext(t, "```sh\n$ pwd\n```\n", context.Background())
		invalidate(&c)
		got, err := c.ShellPrompts(context.Background())
		if !errors.Is(err, parser.ErrModel) || got.Dollars != nil {
			t.Fatal(got, err)
		}
	}
	c := md014ShapeContext(t, "```sh\n$ pwd\n```\n", context.Background())
	//nolint:staticcheck // Deliberately missing caller authority.
	if got, err := c.ShellPrompts(nil); !errors.Is(err, parser.ErrModel) || got.Dollars != nil {
		t.Fatal(got, err)
	}
}

// Original empty parse25W2N, facade546W2N: held-source finite arithmetic.
func TestRuleContextShellPromptsStaticEmptyReservations(t *testing.T) {
	for _, nodes := range []bool{false, true} {
		for _, delta := range []int64{-1, 0} {
			t.Run(fmt.Sprintf("nodes-%v/delta-%d", nodes, delta), func(t *testing.T) {
				c := md014ShapeContext(t, "", context.Background())
				charge, limit, setup, phase := c.budget.ChargeWork, c.budget.Limits().WorkUnits, int64(25), int64(546)
				if nodes {
					charge, limit, setup, phase = c.budget.ChargeNodes, c.budget.Limits().TotalNodes, 2, 2
				}
				if err := charge(limit - setup - phase - delta); err != nil {
					t.Fatal(err)
				}
				got, err := c.ShellPrompts(context.Background())
				if delta == 0 {
					if err != nil || got.Availability != parser.Available || len(got.Dollars) != 0 {
						t.Fatal(got, err)
					}
				} else {
					for range 2 {
						var stop *parser.LimitExceeded
						if !errors.As(err, &stop) || got.Dollars != nil {
							t.Fatal(got, err)
						}
						got, err = c.ShellPrompts(context.Background())
					}
				}
			})
		}
	}
}

func TestRuleContextShellPromptsCallerAndOriginControls(t *testing.T) {
	for _, source := range []string{"", "clean", "`code`", "<pre>**x**", "```sh\n$ pwd\n```\n", "<div>**x** <x a=\""} {
		for _, originating := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/origin-%v", source, originating), func(t *testing.T) {
				origin, cancelOrigin := context.WithCancel(context.Background())
				defer cancelOrigin()
				c := md014ShapeContext(t, source, origin)
				caller, cancelCaller := context.WithCancel(context.Background())
				defer cancelCaller()
				if originating {
					cancelOrigin()
				} else {
					cancelCaller()
				}
				got, err := c.ShellPrompts(caller)
				if !errors.Is(err, context.Canceled) || got.Dollars != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
}

type md014ProducerFinalStop struct {
	context.Context
	armed, reached bool
	reason         error
	gate           string
}

func (c *md014ProducerFinalStop) Err() error {
	if c.armed {
		var pcs [32]uintptr
		n := runtime.Callers(1, pcs[:])
		frames := runtime.CallersFrames(pcs[:n])
		for {
			f, more := frames.Next()
			if strings.HasSuffix(f.Function, c.gate) {
				c.reached = true
				return c.reason
			}
			if !more {
				break
			}
		}
	}
	return c.Context.Err()
}

func TestRuleContextShellPromptsProducerFinalAfterPositive(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).ShellPrompts.func1", ".RuleContext.ShellPrompts.func1"} {
		for _, originating := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(fmt.Sprintf("origin-%v/%v", originating, reason), func(t *testing.T) {
					origin := &md014ProducerFinalStop{Context: context.Background(), reason: reason, gate: gate}
					c := md014ShapeContext(t, "```sh\n$ pwd\n```\n", origin)
					before, err := c.ShellPrompts(context.Background())
					if err != nil || len(before.Dollars) != 1 || before.Dollars[0].Start() != 6 || before.Dollars[0].End() != 7 {
						t.Fatal("actual earlier positive precondition", before, err)
					}
					caller := &md014ProducerFinalStop{Context: context.Background(), armed: true, reason: reason, gate: gate}
					var ctx context.Context = caller
					if originating {
						origin.armed = true
						ctx = context.Background()
					}
					got, err := c.ShellPrompts(ctx)
					reached := caller.reached
					if originating {
						reached = origin.reached
					}
					if !reached || !errors.Is(err, reason) || got.Dollars != nil {
						t.Fatal(got, err, reached)
					}
				})
			}
		}
	}

}

func TestRuleContextShellPromptsActualCallerAndOriginDeadlines(t *testing.T) {
	for _, source := range []string{"", "clean", "```sh\n$ pwd\nout\n```\n", "```sh\n$ pwd\n```\n", "```sh\n$ pwd\n```\n\n<div>\n<x title=\"\n\n```sh\n$ pwd\n```\n"} {
		for _, originating := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/origin-%v", source, originating), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					origin := context.Background()
					caller := context.Background()
					if originating {
						c, cancel := context.WithDeadline(origin, time.Now().Add(time.Second))
						defer cancel()
						origin = c
					}
					c := md014ShapeContext(t, source, origin)
					if !originating {
						d, cancel := context.WithDeadline(caller, time.Now().Add(time.Second))
						defer cancel()
						caller = d
					}
					time.Sleep(2 * time.Second)
					out, err := c.ShellPrompts(caller)
					if !errors.Is(err, context.DeadlineExceeded) || out.Dollars != nil {
						t.Fatal(out, err)
					}
				})
			})
		}
	}
}

// Inject at a named source publication gate, after an independently asserted
// positive original projection. This consumes the original allowance and never
// reads counters, computes measured setup or replaces the production authority.
type md014GateLimit struct {
	context.Context
	budget         *parser.OperationBudget
	gate, kind     string
	armed, reached bool
}

func (c *md014GateLimit) Err() error {
	if c.armed {
		var pcs [32]uintptr
		n := runtime.Callers(1, pcs[:])
		frames := runtime.CallersFrames(pcs[:n])
		for {
			frame, more := frames.Next()
			if strings.HasSuffix(frame.Function, c.gate) {
				c.reached, c.armed = true, false
				switch c.kind {
				case "work":
					_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
				case "nodes":
					_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes)
				case "elapsed":
					time.Sleep(61 * time.Second)
				default:
					panic("unknown test stop")
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

func TestRuleContextShellPromptsProducerAndFacadeFinalStickyDiscard(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).ShellPrompts.func1", ".RuleContext.ShellPrompts.func1"} {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/origin-%v", gate, kind, originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						origin := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind}
						c := md014ShapeContext(t, "```sh\n$ pwd\n```\n", origin)
						origin.budget = c.budget
						pre, e := c.ShellPrompts(context.Background())
						if e != nil || len(pre.Dollars) != 1 || pre.Dollars[0].Start() != 6 {
							t.Fatal("positive precondition", pre, e)
						}
						caller := &md014GateLimit{Context: context.Background(), gate: gate, kind: kind, budget: c.budget, armed: true}
						var target context.Context = caller
						if originating {
							origin.armed = true
							target = context.Background()
						}
						for range 2 {
							got, e := c.ShellPrompts(target)
							var stop *parser.LimitExceeded
							if !errors.As(e, &stop) || got.Dollars != nil || got.Availability != "" {
								t.Fatal(got, e)
							}
						}
						if !caller.reached && !origin.reached {
							t.Fatal("final gate not reached")
						}
					})
				})
			}
		}
	}
}
