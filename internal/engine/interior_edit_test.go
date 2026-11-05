package engine

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func interiorQuery(t *testing.T, text string) (*InteriorEditQuery, parser.ParseResult, DirectiveResult, *parser.OperationBudget) {
	t.Helper()
	r, err := NewRegistry([]Metadata{{ID: "MD019", Name: "spacing", DefaultEnabled: true, Fix: ImplementedFix}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := r.ResolveSettings(nil, ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	options, err := parser.NewFrontmatterOptions(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithOptions(context.Background(), parser.NewSource([]byte(text)), budget, parser.ParseOptions{Frontmatter: options})
	state := ProcessDirectives(context.Background(), parsed, ruleDirectivePolicy{rules}, budget)
	return NewInteriorEditQuery(parsed, state, budget), parsed, state, budget
}

func TestInteriorEditEligibility(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, text string
		start, end int
		want       bool
	}{
		{"valid gap beside code span", "#  `x`\n", 2, 3, true},
		{"valid gap beside comment", "#  <!-- c -->\n", 2, 3, true},
		{"code span payload", "# `a  b`\n", 4, 5, false},
		{"into code span", "#  `x`\n", 2, 5, false},
		{"fenced payload", "```\n#  x\n```\n", 6, 7, false},
		{"comment", "<!-- a  b -->\n", 6, 7, false},
		{"directive comment", "# <!-- mdchk:enable MD019  --> x\n", 25, 26, false},
		{"front matter", "---\n#  x: y\n---\n\ntext\n", 6, 7, false},
		{"disabled anchor", "<!-- mdchk:disable MD019 -->\n#  x\n", 31, 32, false},
		{"insertion", "#  x\n", 2, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, parsed, _, _ := interiorQuery(t, tc.text)
			span, err := parsed.Source.Span(tc.start, tc.end)
			if err != nil {
				t.Fatal(err)
			}
			got, err := q.Eligible(ctx, "MD019", span, span)
			if err != nil || got != tc.want {
				t.Fatal(got, err)
			}
		})
	}
}

func TestInteriorEditRejectsUncertainAndIncompleteParses(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		alter func(*parser.ParseResult)
	}{
		{"uncertainty", func(p *parser.ParseResult) { p.Uncertainty = append(p.Uncertainty, parser.Uncertainty{}) }},
		{"incomplete", func(p *parser.ParseResult) { p.Status = parser.Incomplete }},
		{"inlines unfinished", func(p *parser.ParseResult) { p.Phases.Inlines = parser.Pending }},
		{"no document", func(p *parser.ParseResult) { p.Document = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, parsed, state, budget := interiorQuery(t, "#  x\n")
			span, _ := parsed.Source.Span(2, 3)
			tc.alter(&parsed)
			got, err := NewInteriorEditQuery(parsed, state, budget).Eligible(ctx, "MD019", span, span)
			if err != nil || got {
				t.Fatal(got, err)
			}
		})
	}
}

func TestRuleInteriorEditGuards(t *testing.T) {
	ctx := context.Background()
	_, parsed, state, budget := interiorQuery(t, "#  x\n")
	span, _ := parsed.Source.Span(2, 3)
	base := RuleContext{input: Input{source: parsed.Source}, parsed: parsed, directives: state, budget: budget, operation: ctx, syntaxReliable: true}
	if got, err := base.InteriorEdits().Eligible(ctx, "MD019", span, span); err != nil || !got {
		t.Fatal("valid gap", got, err)
	}
	hidden := base
	hidden.observational = true
	if got, err := hidden.InteriorEdits().Eligible(ctx, "MD019", span, span); err != nil || got {
		t.Fatal("hidden observation", got, err)
	}
	unreliable := base
	unreliable.syntaxReliable = false
	if got, err := unreliable.InteriorEdits().Eligible(ctx, "MD019", span, span); err != nil || got {
		t.Fatal("unreliable syntax", got, err)
	}
	_, parsed, state, budget = interiorQuery(t, "<!-- mdchk:disable MD019 -->\n#  x\n")
	span, _ = parsed.Source.Span(31, 32)
	disabled := RuleContext{input: Input{source: parsed.Source}, parsed: parsed, directives: state, budget: budget, operation: ctx, syntaxReliable: true}
	if got, err := disabled.InteriorEdits().Eligible(ctx, "MD019", span, span); err != nil || got {
		t.Fatal("disabled anchor", got, err)
	}
}

func TestInteriorEditIndexBuiltOnce(t *testing.T) {
	ctx := context.Background()
	q, parsed, _, _ := interiorQuery(t, "#  `a`\n\n#  `b`\n")
	first, _ := parsed.Source.Span(2, 3)
	second, _ := parsed.Source.Span(10, 11)
	if got, err := q.Eligible(ctx, "MD019", first, first); err != nil || !got || !q.indexed {
		t.Fatal(got, err)
	}
	built := q.protected
	if got, err := q.Eligible(ctx, "MD019", second, second); err != nil || !got || &built[0] != &q.protected[0] {
		t.Fatal(got, err)
	}
}
