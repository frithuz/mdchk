package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func md086ShapeContext(t *testing.T, source string, origin context.Context) RuleContext {
	t.Helper()
	in, err := NewInput("comments.md", []byte(source), nil, DocumentContext{Flavor: "commonmark"})
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

func TestRuleContextUnclosedHTMLCommentsAuthority(t *testing.T) {
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("<!--")) }, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("<!--"))) }, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) { c.input, _ = NewInput("other.md", []byte("<!--"), nil, DocumentContext{}) }, func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		}, func(c *RuleContext) { c.input.documentContext.Flavor = "gfm" }, func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) }, func(c *RuleContext) { c.options = Options{"unknown": false} }, func(c *RuleContext) { c.options = Options{"allowed_tags": []string{}} },
	} {
		c := md086ShapeContext(t, "<!--", context.Background())
		invalidate(&c)
		got, err := c.UnclosedHTMLComments(context.Background())
		if !errors.Is(err, parser.ErrModel) || got.Comments != nil {
			t.Fatal(got, err)
		}
	}
	c := md086ShapeContext(t, "<!--", context.Background())
	//nolint:staticcheck // Deliberately missing caller authority.
	got, err := c.UnclosedHTMLComments(nil)
	if !errors.Is(err, parser.ErrModel) || got.Comments != nil {
		t.Fatal(got, err)
	}
}

// The genuine empty production parse costs 25 work and 2 records, derived in the
// static ledger. The facade adds three gates to the producer's empty traversal.
// No measured setup, counter inspection or replacement allowance is used.
func TestRuleContextUnclosedHTMLCommentsStaticEmptyReservations(t *testing.T) {
	for _, nodes := range []bool{false, true} {
		for _, delta := range []int64{-1, 0} {
			c := md086ShapeContext(t, "", context.Background())
			charge, limit, setup, phase := c.budget.ChargeWork, c.budget.Limits().WorkUnits, int64(25), int64(35)
			if nodes {
				charge, limit, setup, phase = c.budget.ChargeNodes, c.budget.Limits().TotalNodes, 2, 2
			}
			if err := charge(limit - setup - phase - delta); err != nil {
				t.Fatal(err)
			}
			got, err := c.UnclosedHTMLComments(context.Background())
			if delta == 0 {
				if err != nil || got.Availability != parser.Available || len(got.Comments) != 0 {
					t.Fatal(got, err)
				}
			} else {
				for range 2 {
					var stop *parser.LimitExceeded
					if !errors.As(err, &stop) || got.Comments != nil {
						t.Fatal(got, err)
					}
					got, err = c.UnclosedHTMLComments(context.Background())
				}
			}
		}
	}
}

func TestRuleContextUnclosedHTMLCommentsControls(t *testing.T) {
	for _, source := range []string{"", "clean", "`ab`", "<!--x-->", "<!--", "<div\n"} {
		for _, origin := range []bool{false, true} {
			c := md086ShapeContext(t, source, context.Background())
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			ctx := context.Background()
			if origin {
				c.operation = cancelled
			} else {
				ctx = cancelled
			}
			got, err := c.UnclosedHTMLComments(ctx)
			if !errors.Is(err, context.Canceled) || got.Comments != nil {
				t.Fatal(got, err)
			}
		}
	}
	for _, nodes := range []bool{false, true} {
		c := md086ShapeContext(t, "", context.Background())
		charge, limit := c.budget.ChargeWork, c.budget.Limits().WorkUnits
		if nodes {
			charge, limit = c.budget.ChargeNodes, c.budget.Limits().TotalNodes
		}
		_ = charge(limit)
		for range 2 {
			got, err := c.UnclosedHTMLComments(context.Background())
			var stop *parser.LimitExceeded
			if !errors.As(err, &stop) || got.Comments != nil {
				t.Fatal(got, err)
			}
		}
	}
}

func TestRuleContextUnclosedHTMLCommentsFinalCallerAndOrigin(t *testing.T) {
	for _, tc := range []struct {
		source string
		final  int
	}{{"", 8}, {"clean", 10}, {"`ab`", 10}, {"<!--x-->", 10}, {"<!--", 9}, {"<div\n", 11}} {
		for _, originating := range []bool{false, true} {
			for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
				origin := &mdTerminalStopAt{Context: context.Background(), stop: 1 << 30, reason: reason}
				c := md086ShapeContext(t, tc.source, origin)
				caller := &mdTerminalStopAt{Context: context.Background(), stop: tc.final, reason: reason}
				var ctx context.Context = caller
				if originating {
					origin.stop = origin.calls + tc.final
					ctx = context.Background()
				}
				got, err := c.UnclosedHTMLComments(ctx)
				if !errors.Is(err, reason) || got.Comments != nil {
					t.Fatal(tc.source, got, err)
				}
			}
		}
	}
}
