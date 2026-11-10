package md050_test

import (
	"context"
	"errors"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/md050"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

func TestMD050FactValidationAndUnorderedMinimum(t *testing.T) {
	for _, invalid := range []string{"kind", "width", "marker", "foreign", "absent", "opening", "closing", "envelope", "style", "missing-style", "reversed"} {
		t.Run(invalid, func(t *testing.T) {
			var observed engine.RuleOutcome
			r := ruletest.Registry(t, []engine.Registration{{ID: "MD050", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				items, err := c.DelimiterStyle(ctx)
				if err != nil || len(items.Valid) != 2 {
					t.Fatal(items, err)
				}
				style := "consistent"
				switch invalid {
				case "kind":
					items.Valid[0].Kind = parser.Text
				case "width":
					items.Valid[0].Width = 1
				case "marker":
					items.Valid[0].Marker = '-'
				case "foreign":
					items.Valid[0].Opening, _ = parser.NewSource([]byte("**a**")).Span(0, 2)
				case "absent":
					items.Valid[0].Closing = parser.Span{}
				case "opening":
					items.Valid[0].Opening, _ = c.Source().Span(1, 3)
				case "closing":
					items.Valid[0].Closing, _ = c.Source().Span(4, 6)
				case "envelope":
					items.Valid[0].Envelope, _ = c.Source().Span(1, 5)
				case "style":
					style = "dash"
				case "missing-style":
					style = ""
				case "reversed":
					items.Valid[0], items.Valid[1] = items.Valid[1], items.Valid[0]
				}
				observed = md050.CompareBoldDelimiterStyle(ctx, c, style, items.Valid)
				if invalid == "reversed" {
					if observed.Err != nil || len(observed.Findings) != 1 || observed.Findings[0].Span.Start() != 6 || observed.Findings[0].FixEligible {
						t.Fatal(observed)
					}
				} else if !errors.Is(observed.Err, parser.ErrModel) || observed.Findings != nil {
					t.Fatal(observed)
				}
				again, err := c.DelimiterStyle(ctx)
				if err != nil || again.Valid[0].Opening.Start() != 0 || again.Valid[0].Width != 2 {
					t.Fatal("detached mutation leaked", again, err)
				}
				return observed
			}}})
			out := ruletest.AnalyzeDocument(t, r, "**a** __b__", md050Settings("consistent"), engine.DocumentContext{}, engine.RunOptions{})
			if invalid == "reversed" {
				if !out.Complete || len(out.Diagnostics) != 1 {
					t.Fatal(out)
				}
			} else if len(out.Diagnostics) != 0 || out.Complete {
				t.Fatal(out)
			}
		})
	}
}

func TestMD050UnavailableSyntaxAndEncoding(t *testing.T) {
	r := ruletest.Registry(t, rules.Registrations())
	for _, flavor := range []string{"missing", "pandoc", "quarto"} {
		out := ruletest.AnalyzeDocument(t, r, "**a** __b__", md050Settings("consistent"), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
		if out.Complete || len(out.Diagnostics) != 0 || len(out.Coverage) == 0 {
			t.Fatal(out)
		}
	}
	out := ruletest.AnalyzeDocument(t, r, string([]byte{'*', 'a', '*', ' ', 255}), md050Settings("consistent"), engine.DocumentContext{}, engine.RunOptions{})
	if out.Complete || len(out.Diagnostics) != 0 || len(out.Errors) != 1 {
		t.Fatal(out)
	}
}

func TestMD050UnreliableHeaderCoverage(t *testing.T) {
	fm, err := parser.NewFrontmatterOptions(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), "---\ntitle: __a__ **b**", md050Settings("consistent"), engine.DocumentContext{Frontmatter: fm}, engine.RunOptions{})
	if out.Complete || len(out.Diagnostics) != 0 || len(out.Coverage) == 0 {
		t.Fatal(out)
	}
}
