package engine

import (
	"context"
	"errors"
	"fmt"
	"github.com/frithuz/mdchk/internal/parser"
	"reflect"
	"sync"
	"testing"
)

func md033ShapeContext(t *testing.T, source string, origin context.Context) RuleContext {
	t.Helper()
	in, e := NewInput("html.md", []byte(source), nil, DocumentContext{Flavor: "commonmark"})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	p := parser.ParseWithOptions(origin, in.source, b, in.documentContext.ParseOptions())
	if p.Status != parser.Complete {
		t.Fatal(p)
	}
	return RuleContext{input: in, index: parser.NewLineIndex(in.source), document: p.Document, parsed: p, budget: b, operation: origin, syntaxReliable: true, options: Options{"allowed_tags": []string{}}}
}

func TestRuleContextEmbeddedHTMLAuthority(t *testing.T) {
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("<b>")) }, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("<b>"))) }, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) { c.input, _ = NewInput("other.md", []byte("<b>"), nil, DocumentContext{}) }, func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		}, func(c *RuleContext) { c.input.documentContext.Flavor = "gfm" }, func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) }, func(c *RuleContext) { c.options = Options{} }, func(c *RuleContext) { c.options = Options{"allowed_tags": nil} }, func(c *RuleContext) { c.options = Options{"allowed_tags": []string{}, "unknown": false} },
	} {
		c := md033ShapeContext(t, "<b>", context.Background())
		invalidate(&c)
		got, e := c.EmbeddedHTMLTags(context.Background())
		if !errors.Is(e, parser.ErrModel) || got.Tags != nil {
			t.Fatal(got, e)
		}
		list, e := c.EmbeddedHTMLAllowedTags(context.Background())
		if !errors.Is(e, parser.ErrModel) || list != nil {
			t.Fatal(list, e)
		}
	}
	c := md033ShapeContext(t, "<b>", context.Background())
	//nolint:staticcheck // Deliberately missing caller authority.
	got, e := c.EmbeddedHTMLTags(nil)
	if !errors.Is(e, parser.ErrModel) || got.Tags != nil {
		t.Fatal(got, e)
	}
}

func TestRuleContextEmbeddedHTMLDetachedOptionsAndControls(t *testing.T) {
	c := md033ShapeContext(t, "<b>", context.Background())
	c.options = Options{"allowed_tags": []string{"b", "B", "", string([]byte{0xff})}}
	list, e := c.EmbeddedHTMLAllowedTags(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	list[0] = "x"
	public := c.Options()
	public["allowed_tags"].([]string)[0] = "x"
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			got, e := c.EmbeddedHTMLAllowedTags(context.Background())
			if e != nil || !reflect.DeepEqual(got, []string{"b", "B", "", string([]byte{0xff})}) {
				t.Error(got, e)
			}
		})
	}
	wg.Wait()
	for _, origin := range []bool{false, true} {
		c := md033ShapeContext(t, "<b>", context.Background())
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		ctx := context.Background()
		if origin {
			c.operation = cancelled
		} else {
			ctx = cancelled
		}
		got, e := c.EmbeddedHTMLTags(ctx)
		if !errors.Is(e, context.Canceled) || got.Tags != nil {
			t.Fatal(got, e)
		}
		list, e := c.EmbeddedHTMLAllowedTags(ctx)
		if !errors.Is(e, context.Canceled) || list != nil {
			t.Fatal(list, e)
		}
	}
	for _, nodes := range []bool{false, true} {
		c := md033ShapeContext(t, "", context.Background())
		charge, limit := c.budget.ChargeWork, c.budget.Limits().WorkUnits
		if nodes {
			charge, limit = c.budget.ChargeNodes, c.budget.Limits().TotalNodes
		}
		_ = charge(limit)
		for range 2 {
			got, e := c.EmbeddedHTMLTags(context.Background())
			var stop *parser.LimitExceeded
			if !errors.As(e, &stop) || got.Tags != nil {
				t.Fatal(got, e)
			}
		}
	}
}

var md033ProducerRows = []struct {
	name, source string
	work, nodes  int64
	facts, final int
	available    parser.Availability
}{
	{"empty", "", 32, 2, 0, 5, parser.Available}, {"clean", "clean", 82, 4, 0, 7, parser.Available},
	{"code", "`ab`", 82, 4, 0, 7, parser.Available}, {"comment", "<!--x-->", 202, 4, 0, 7, parser.Available},
	{"one", "<b>", 406, 7, 1, 10, parser.Available}, {"two", "<b></b>", 685, 15, 2, 15, parser.Available},
	{"raw", "<pre><b>x</b></pre>", 1071, 10, 2, 19, parser.Available}, {"unavailable", "<div\n", 199, 5, 0, 8, parser.Unavailable},
}

func TestRuleContextEmbeddedHTMLStaticBoundaries(t *testing.T) {
	for _, tc := range md033ProducerRows {
		for _, getter := range []bool{false, true} {
			for _, nodes := range []bool{false, true} {
				for _, delta := range []int64{-1, 0} {
					t.Run(fmt.Sprintf("%s/getter-%v/nodes-%v/%d", tc.name, getter, nodes, delta), func(t *testing.T) {
						// Nonempty list gives a meaningful construction boundary even for the getter.
						makeContext := func() RuleContext {
							c := md033ShapeContext(t, tc.source, context.Background())
							c.options = Options{"allowed_tags": []string{"B"}}
							return c
						}
						prefix := makeContext()
						charge, limit := prefix.budget.ChargeWork, prefix.budget.Limits().WorkUnits
						exact := tc.work + 22
						if getter {
							exact = 20
						}
						if nodes {
							charge, limit = prefix.budget.ChargeNodes, prefix.budget.Limits().TotalNodes
							exact = tc.nodes + 1
							if getter {
								exact = 1
							}
						}
						var stop *parser.LimitExceeded
						if !errors.As(charge(limit), &stop) {
							t.Fatal("prefix must exhaust")
						}
						used := stop.Observed - limit
						c := makeContext()
						charge = c.budget.ChargeWork
						if nodes {
							charge = c.budget.ChargeNodes
						}
						if e := charge(limit - used - exact - delta); e != nil {
							t.Fatal(e)
						}
						call := func() ([]string, parser.EmbeddedHTML, error) {
							if getter {
								list, e := c.EmbeddedHTMLAllowedTags(context.Background())
								return list, parser.EmbeddedHTML{}, e
							}
							got, e := c.EmbeddedHTMLTags(context.Background())
							return nil, got, e
						}
						list, got, e := call()
						if delta == 0 {
							if e != nil || getter && !reflect.DeepEqual(list, []string{"B"}) || !getter && (len(got.Tags) != tc.facts || got.Availability != tc.available) {
								t.Fatal(list, got, e)
							}
						} else {
							for range 2 {
								if !errors.As(e, &stop) || list != nil || got.Tags != nil {
									t.Fatal(list, got, e)
								}
								list, got, e = call()
							}
						}
					})
				}
			}
		}
	}
}

func TestRuleContextEmbeddedHTMLFinalCallerAndOrigin(t *testing.T) {
	for _, tc := range md033ProducerRows {
		for _, getter := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%s/getter-%v/origin-%v/%v", tc.name, getter, originating, reason), func(t *testing.T) {
						origin := &mdTerminalStopAt{Context: context.Background(), stop: 1 << 30, reason: reason}
						c := md033ShapeContext(t, tc.source, origin)
						final := tc.final + 6
						if getter {
							final = 4
						}
						caller := &mdTerminalStopAt{Context: context.Background(), stop: final, reason: reason}
						var ctx context.Context = caller
						if originating {
							origin.stop = origin.calls + final
							ctx = context.Background()
						}
						if getter {
							got, e := c.EmbeddedHTMLAllowedTags(ctx)
							if !errors.Is(e, reason) || got != nil {
								t.Fatal(got, e)
							}
						} else {
							got, e := c.EmbeddedHTMLTags(ctx)
							if !errors.Is(e, reason) || got.Tags != nil {
								t.Fatal(got, e)
							}
						}
					})
				}
			}
		}
	}
}
