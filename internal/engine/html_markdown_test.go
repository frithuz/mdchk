package engine

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func md091ShapeContext(t *testing.T, source string, origin context.Context) RuleContext {
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

func TestRuleContextHTMLMarkdownOriginalAuthority(t *testing.T) {
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("<div>**x**")) }, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("<div>**x**"))) }, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) {
			c.input, _ = NewInput("foreign.md", []byte("<div>**x**"), nil, DocumentContext{})
		}, func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		}, func(c *RuleContext) { c.input.documentContext.Flavor = "gfm" }, func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) }, func(c *RuleContext) { c.options = Options{"unknown": false} }, func(c *RuleContext) { c.options = Options{"allowed_tags": []string{}} },
	} {
		c := md091ShapeContext(t, "<div>**x**", context.Background())
		invalidate(&c)
		got, err := c.HTMLMarkdownCandidates(context.Background())
		if !errors.Is(err, parser.ErrModel) || got.Candidates != nil {
			t.Fatal(got, err)
		}
	}
	c := md091ShapeContext(t, "<div>**x**", context.Background())
	//nolint:staticcheck // Deliberately missing caller authority.
	if got, err := c.HTMLMarkdownCandidates(nil); !errors.Is(err, parser.ErrModel) || got.Candidates != nil {
		t.Fatal(got, err)
	}
}

// Original empty parse25W2N, facade51W2N: held-source finite arithmetic.
func TestRuleContextHTMLMarkdownStaticEmptyReservations(t *testing.T) {
	for _, nodes := range []bool{false, true} {
		for _, delta := range []int64{-1, 0} {
			t.Run(fmt.Sprintf("nodes-%v/delta-%d", nodes, delta), func(t *testing.T) {
				c := md091ShapeContext(t, "", context.Background())
				charge, limit, setup, phase := c.budget.ChargeWork, c.budget.Limits().WorkUnits, int64(25), int64(51)
				if nodes {
					charge, limit, setup, phase = c.budget.ChargeNodes, c.budget.Limits().TotalNodes, 2, 2
				}
				if err := charge(limit - setup - phase - delta); err != nil {
					t.Fatal(err)
				}
				got, err := c.HTMLMarkdownCandidates(context.Background())
				if delta == 0 {
					if err != nil || got.Availability != parser.Available || len(got.Candidates) != 0 {
						t.Fatal(got, err)
					}
				} else {
					for range 2 {
						var stop *parser.LimitExceeded
						if !errors.As(err, &stop) || got.Candidates != nil {
							t.Fatal(got, err)
						}
						got, err = c.HTMLMarkdownCandidates(context.Background())
					}
				}
			})
		}
	}
}

func TestRuleContextHTMLMarkdownCallerAndOriginControls(t *testing.T) {
	for _, source := range []string{"", "clean", "`code`", "<pre>**x**", "<div>**x**", "<div>**x** <x a=\""} {
		for _, originating := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/origin-%v", source, originating), func(t *testing.T) {
				origin, cancelOrigin := context.WithCancel(context.Background())
				defer cancelOrigin()
				c := md091ShapeContext(t, source, origin)
				caller, cancelCaller := context.WithCancel(context.Background())
				defer cancelCaller()
				if originating {
					cancelOrigin()
				} else {
					cancelCaller()
				}
				got, err := c.HTMLMarkdownCandidates(caller)
				if !errors.Is(err, context.Canceled) || got.Candidates != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
	// Source-derived7 reaches producer deferred final after facade's2 entry
	// gates;8 reaches facade final after the completed producer publication.
	for _, final := range []int{7, 8} {
		for _, originating := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				origin := &mdTerminalStopAt{Context: context.Background(), stop: 1 << 30, reason: reason}
				c := md091ShapeContext(t, "", origin)
				caller := &mdTerminalStopAt{Context: context.Background(), stop: final, reason: reason}
				var ctx context.Context = caller
				if originating {
					origin.stop = origin.calls + final
					ctx = context.Background()
				}
				got, err := c.HTMLMarkdownCandidates(ctx)
				if !errors.Is(err, reason) || got.Candidates != nil {
					t.Fatal(got, err)
				}
			}
		}
	}
}

type md091ProducerFinalStop struct {
	context.Context
	armed, reached bool
	reason         error
}

func (c *md091ProducerFinalStop) Err() error {
	if c.armed {
		var pcs [32]uintptr
		n := runtime.Callers(1, pcs[:])
		frames := runtime.CallersFrames(pcs[:n])
		for {
			f, more := frames.Next()
			if strings.HasSuffix(f.Function, "/parser.(*Document).HTMLMarkdownCandidates.func1") {
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

func TestRuleContextHTMLMarkdownProducerFinalAfterPositive(t *testing.T) {
	for _, originating := range []bool{false, true} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(fmt.Sprintf("origin-%v/%v", originating, reason), func(t *testing.T) {
				origin := &md091ProducerFinalStop{Context: context.Background(), reason: reason}
				c := md091ShapeContext(t, "<div>**x**", origin)
				before, err := c.HTMLMarkdownCandidates(context.Background())
				if err != nil || len(before.Candidates) != 1 || before.Candidates[0].Marker.Start() != 5 || before.Candidates[0].Marker.End() != 7 {
					t.Fatal("actual earlier positive precondition", before, err)
				}
				caller := &md091ProducerFinalStop{Context: context.Background(), armed: true, reason: reason}
				var ctx context.Context = caller
				if originating {
					origin.armed = true
					ctx = context.Background()
				}
				got, err := c.HTMLMarkdownCandidates(ctx)
				reached := caller.reached
				if originating {
					reached = origin.reached
				}
				if !reached || !errors.Is(err, reason) || got.Candidates != nil {
					t.Fatal(got, err, reached)
				}
			})
		}
	}
}
