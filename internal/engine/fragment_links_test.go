package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

func fragmentContext(t *testing.T, flavor, source string, origin context.Context) RuleContext {
	t.Helper()
	c := md011ShapeContext(t, flavor, source, origin)
	c.options = Options{"cross_file": false}
	return c
}

func TestRuleContextFragmentLinksOriginalAuthority(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, invalidate := range []func(*RuleContext){
				func(c *RuleContext) { c.syntaxReliable = false },
				func(c *RuleContext) { c.parsed.Status = parser.Incomplete },
				func(c *RuleContext) { c.operation = nil },
				func(c *RuleContext) { c.document = nil },
				func(c *RuleContext) { c.budget = nil },
				func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("[x](#a)"))) },
				func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
				func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) },
				func(c *RuleContext) { c.options = Options{} },
				func(c *RuleContext) { c.options = Options{"cross_file": nil} },
				func(c *RuleContext) { c.options = Options{"cross_file": false, "extra": true} },
			} {
				c := fragmentContext(t, flavor, "[x](#a)", context.Background())
				pre, err := c.FragmentLinks(context.Background())
				if err != nil || len(pre.Links) != 1 || pre.Links[0].Destination != "#a" {
					t.Fatal("actual positive before authority mutation", pre, err)
				}
				invalidate(&c)
				got, err := c.FragmentLinks(context.Background())
				if !errors.Is(err, parser.ErrModel) || got.Links != nil {
					t.Fatal(got, err)
				}
			}
			c := fragmentContext(t, flavor, "[x](#a)", context.Background())
			//nolint:staticcheck // Deliberately missing caller authority.
			if got, err := c.FragmentLinks(nil); !errors.Is(err, parser.ErrModel) || got.Links != nil {
				t.Fatal(got, err)
			}
		})
	}
}

func TestRuleContextFragmentLinksCallerAndOriginControls(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, source := range []string{"", "clean", "[x](#a)", "<div>**x** <x a=\""} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%q/origin-%v", flavor, source, originating), func(t *testing.T) {
					origin, cancelOrigin := context.WithCancel(context.Background())
					defer cancelOrigin()
					c := fragmentContext(t, flavor, source, origin)
					caller, cancelCaller := context.WithCancel(context.Background())
					defer cancelCaller()
					if originating {
						cancelOrigin()
					} else {
						cancelCaller()
					}
					got, err := c.FragmentLinks(caller)
					if !errors.Is(err, context.Canceled) || got.Links != nil {
						t.Fatal(got, err)
					}
				})
			}
		}
	}
}

func TestRuleContextAnchorOccurrences(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, style := range []string{"github", "explicit"} {
			t.Run(flavor+"/"+style, func(t *testing.T) {
				c := fragmentContext(t, flavor, "# A\n\n<a id=\"b\"></a>\n", context.Background())
				c.input.documentContext.AnchorStyle = style
				got, availability, err := c.AnchorOccurrences(context.Background())
				want := 2
				if style == "explicit" {
					want = 1
				}
				if err != nil || availability != resource.Available || len(got) != want {
					t.Fatal(got, availability, err)
				}
				c.syntaxReliable = false
				if got, availability, err = c.AnchorOccurrences(context.Background()); !errors.Is(err, parser.ErrModel) || got != nil || availability == resource.Available {
					t.Fatal(got, availability, err)
				}
				//nolint:staticcheck // Deliberately missing caller authority.
				if got, _, err = fragmentContext(t, flavor, "# A\n", context.Background()).AnchorOccurrences(nil); !errors.Is(err, parser.ErrModel) || got != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
}
