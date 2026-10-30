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

func md092ShapeContext(t *testing.T, source string, origin context.Context) RuleContext {
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
	return RuleContext{input: in, index: parser.NewLineIndex(in.source), document: p.Document, parsed: p, budget: b, operation: origin, syntaxReliable: true}
}

func TestRuleContextConflictMarkersOriginalAuthority(t *testing.T) {
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("<<<<<<<")) }, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("<<<<<<<"))) }, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte("<<<<<<<"), nil, DocumentContext{})
		}, func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		}, func(c *RuleContext) { c.input.documentContext.Flavor = "gfm" }, func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) }, func(c *RuleContext) { c.options = Options{"unknown": false} }, func(c *RuleContext) { c.options = Options{"allowed_tags": []string{}} },
	} {
		c := md092ShapeContext(t, "<<<<<<<", context.Background())
		invalidate(&c)
		got, err := c.ConflictMarkers(context.Background())
		if !errors.Is(err, parser.ErrModel) || got.Markers != nil {
			t.Fatal(got, err)
		}
	}
	c := md092ShapeContext(t, "<<<<<<<", context.Background())
	//nolint:staticcheck // Deliberately missing caller authority.
	if got, err := c.ConflictMarkers(nil); !errors.Is(err, parser.ErrModel) || got.Markers != nil {
		t.Fatal(got, err)
	}
}

// Original empty parse25W2N, facade36W2N: held-source finite arithmetic.
func TestRuleContextConflictMarkersStaticEmptyReservations(t *testing.T) {
	for _, nodes := range []bool{false, true} {
		for _, delta := range []int64{-1, 0} {
			t.Run(fmt.Sprintf("nodes-%v/delta-%d", nodes, delta), func(t *testing.T) {
				c := md092ShapeContext(t, "", context.Background())
				charge, limit, setup, phase := c.budget.ChargeWork, c.budget.Limits().WorkUnits, int64(25), int64(36)
				if nodes {
					charge, limit, setup, phase = c.budget.ChargeNodes, c.budget.Limits().TotalNodes, 2, 2
				}
				if err := charge(limit - setup - phase - delta); err != nil {
					t.Fatal(err)
				}
				got, err := c.ConflictMarkers(context.Background())
				if delta == 0 {
					if err != nil || got.Availability != parser.Available || len(got.Markers) != 0 {
						t.Fatal(got, err)
					}
				} else {
					for range 2 {
						var stop *parser.LimitExceeded
						if !errors.As(err, &stop) || got.Markers != nil {
							t.Fatal(got, err)
						}
						got, err = c.ConflictMarkers(context.Background())
					}
				}
			})
		}
	}
}

func TestRuleContextConflictMarkersCallerAndOriginControls(t *testing.T) {
	for _, source := range []string{"", "clean", "`code`", "<pre>**x**", "<<<<<<<", "<div>**x** <x a=\""} {
		for _, originating := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/origin-%v", source, originating), func(t *testing.T) {
				origin, cancelOrigin := context.WithCancel(context.Background())
				defer cancelOrigin()
				c := md092ShapeContext(t, source, origin)
				caller, cancelCaller := context.WithCancel(context.Background())
				defer cancelCaller()
				if originating {
					cancelOrigin()
				} else {
					cancelCaller()
				}
				got, err := c.ConflictMarkers(caller)
				if !errors.Is(err, context.Canceled) || got.Markers != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
}

type md092ProducerFinalStop struct {
	context.Context
	armed, reached bool
	reason         error
	gate           string
}

func (c *md092ProducerFinalStop) Err() error {
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

func TestRuleContextConflictMarkersProducerFinalAfterPositive(t *testing.T) {
	for _, gate := range []string{"/parser.(*Document).ConflictMarkers.func1", ".RuleContext.ConflictMarkers.func1"} {
		for _, originating := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				t.Run(fmt.Sprintf("origin-%v/%v", originating, reason), func(t *testing.T) {
					origin := &md092ProducerFinalStop{Context: context.Background(), reason: reason, gate: gate}
					c := md092ShapeContext(t, "<<<<<<<", origin)
					before, err := c.ConflictMarkers(context.Background())
					if err != nil || len(before.Markers) != 1 || before.Markers[0].Start() != 0 || before.Markers[0].End() != 7 {
						t.Fatal("actual earlier positive precondition", before, err)
					}
					caller := &md092ProducerFinalStop{Context: context.Background(), armed: true, reason: reason, gate: gate}
					var ctx context.Context = caller
					if originating {
						origin.armed = true
						ctx = context.Background()
					}
					got, err := c.ConflictMarkers(ctx)
					reached := caller.reached
					if originating {
						reached = origin.reached
					}
					if !reached || !errors.Is(err, reason) || got.Markers != nil {
						t.Fatal(got, err, reached)
					}
				})
			}
		}
	}

}

func TestRuleContextConflictMarkersActualCallerAndOriginDeadlines(t *testing.T) {
	for _, source := range []string{"", "clean", "```\n<<<<<<<\n```", "<<<<<<<", "<<<<<<< before\n\n<div>\n<x title=\""} {
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
					c := md092ShapeContext(t, source, origin)
					if !originating {
						d, cancel := context.WithDeadline(caller, time.Now().Add(time.Second))
						defer cancel()
						caller = d
					}
					time.Sleep(2 * time.Second)
					out, err := c.ConflictMarkers(caller)
					if !errors.Is(err, context.DeadlineExceeded) || out.Markers != nil {
						t.Fatal(out, err)
					}
				})
			})
		}
	}
}
