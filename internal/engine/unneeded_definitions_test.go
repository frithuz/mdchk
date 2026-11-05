package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

func md053ShapeContext(t *testing.T, flavor, source string, origin context.Context, ignored ...string) RuleContext {
	t.Helper()
	c := md011ShapeContext(t, flavor, source, origin)
	c.options = Options{"ignored_labels": append([]string{}, ignored...)}
	return c
}

func TestRuleContextUnneededDefinitionsOptions(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			const source = "[foo bar]: x\n[FOO BAR]: y\n[b]: z\n"
			c := md053ShapeContext(t, flavor, source, context.Background())
			got, err := c.UnneededDefinitions(context.Background())
			if err != nil || got.Shadowed != parser.Available || got.Unused != parser.Available || len(got.Definitions) != 3 {
				t.Fatal(got, err)
			}
			list := []string{"Foo  Bar", "B"}
			c.options = Options{"ignored_labels": list}
			got, err = c.UnneededDefinitions(context.Background())
			if err != nil || len(got.Definitions) != 1 || !got.Definitions[0].Shadowed || got.Definitions[0].Label != "foo bar" || got.Definitions[0].Span.Start() != 13 || got.Definitions[0].Span.End() != 23 {
				t.Fatal(got, err)
			}
			if !reflect.DeepEqual(list, []string{"Foo  Bar", "B"}) {
				t.Fatal("options changed", list)
			}
			for _, options := range []Options{nil, {}, {"ignored_labels": nil}, {"ignored_labels": "foo"}, {"ignored_labels": []any{"foo"}}, {"ignored_labels": []string{}, "other": true}, {"allowed_tags": []string{}}} {
				c.options = options
				if got, err := c.UnneededDefinitions(context.Background()); !errors.Is(err, parser.ErrModel) || !reflect.DeepEqual(got, parser.UnneededDefinitionInventory{}) {
					t.Fatal(options, got, err)
				}
			}
		})
	}
}

func TestRuleContextUnneededDefinitionsOriginalAuthority(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, invalidate := range []func(*RuleContext){
				func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) {
					c.parsed.Source = parser.NewSource([]byte("[unused]: guide.md"))
				}, func(c *RuleContext) { c.parsed.Status = parser.Incomplete }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil }, func(c *RuleContext) {
					c.index = parser.NewLineIndex(parser.NewSource([]byte("[unused]: guide.md")))
				}, func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) }, func(c *RuleContext) {
					c.input, _ = NewInput("foreign.md", []byte("[unused]: guide.md"), nil, DocumentContext{})
				}, func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) }, func(c *RuleContext) {
					if flavor == "commonmark" {
						c.input.documentContext.Flavor = "gfm"
					} else {
						c.input.documentContext.Flavor = "commonmark"
					}
				}, func(c *RuleContext) { c.input.documentContext.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil) },
			} {
				c := md053ShapeContext(t, flavor, "[unused]: guide.md", context.Background())
				pre, err := c.UnneededDefinitions(context.Background())
				if err != nil || len(pre.Definitions) != 1 || pre.Definitions[0].Span.Start() != 0 || pre.Definitions[0].Span.End() != 9 {
					t.Fatal("actual positive before authority mutation", pre, err)
				}
				invalidate(&c)
				got, err := c.UnneededDefinitions(context.Background())
				if err == nil || got.Definitions != nil {
					t.Fatal(got, err)
				}
			}
			c := md053ShapeContext(t, flavor, "[unused]: guide.md", context.Background())
			//nolint:staticcheck // Deliberately missing caller authority.
			if got, err := c.UnneededDefinitions(nil); !errors.Is(err, parser.ErrModel) || got.Definitions != nil {
				t.Fatal(got, err)
			}
		})
	}
}

func TestRuleContextUnneededDefinitionsCallerAndOriginControls(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, source := range []string{"", "clean", "[unused]: guide.md", "[a]\n\n[a]: x\n[A]: y\n"} {
				for _, originating := range []bool{false, true} {
					t.Run(fmt.Sprintf("%q/origin-%v", source, originating), func(t *testing.T) {
						origin, cancelOrigin := context.WithCancel(context.Background())
						defer cancelOrigin()
						c := md053ShapeContext(t, flavor, source, origin)
						caller, cancelCaller := context.WithCancel(context.Background())
						defer cancelCaller()
						if originating {
							cancelOrigin()
						} else {
							cancelCaller()
						}
						got, err := c.UnneededDefinitions(caller)
						if !errors.Is(err, context.Canceled) || got.Definitions != nil {
							t.Fatal(got, err)
						}
					})
				}
			}
		})
	}
}

func TestRuleContextUnneededDefinitionsActualCallerAndOriginDeadlines(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("origin-%v", originating), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						origin := context.Background()
						caller := context.Background()
						if originating {
							c, cancel := context.WithDeadline(origin, time.Now().Add(time.Second))
							defer cancel()
							origin = c
						}
						c := md053ShapeContext(t, flavor, "[unused]: guide.md", origin)
						if !originating {
							d, cancel := context.WithDeadline(caller, time.Now().Add(time.Second))
							defer cancel()
							caller = d
						}
						time.Sleep(2 * time.Second)
						out, err := c.UnneededDefinitions(caller)
						if !errors.Is(err, context.DeadlineExceeded) || out.Definitions != nil {
							t.Fatal(out, err)
						}
					})
				})
			}
		})
	}
}

func TestRuleContextUnneededDefinitionsBudgetExhaustion(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			c := md053ShapeContext(t, flavor, "[unused]: guide.md", context.Background())
			var stop *parser.LimitExceeded
			if err := c.budget.ChargeWork(c.budget.Limits().WorkUnits); !errors.As(err, &stop) {
				t.Fatal("exhaustion precondition", err)
			}
			for range 2 {
				got, err := c.UnneededDefinitions(context.Background())
				if !errors.As(err, &stop) || !reflect.DeepEqual(got, parser.UnneededDefinitionInventory{}) {
					t.Fatal(got, err)
				}
			}
		})
	}
}
