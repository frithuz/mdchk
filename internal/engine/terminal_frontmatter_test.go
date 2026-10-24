package engine

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestFrontmatterTerminalProtection(t *testing.T) {
	ctx := context.Background()
	options, err := parser.NewFrontmatterOptions(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry([]Metadata{{ID: "MD047", Name: "terminal", DefaultEnabled: true, Fix: PlannedFix}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := r.ResolveSettings(nil, ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"---\nx: y\n---", false}, {"+++\nx = 1\n+++", false},
		{"---\nx: y\n---\nbody", true},
	} {
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		parsed := parser.ParseWithOptions(ctx, parser.NewSource([]byte(tc.text)), budget, parser.ParseOptions{Frontmatter: options})
		state := ProcessDirectives(ctx, parsed, ruleDirectivePolicy{rules}, budget)
		at, err := parsed.Source.Span(len(tc.text), len(tc.text))
		if err != nil {
			t.Fatal(err)
		}
		got, err := TerminalEditEligible(ctx, parsed, state, "MD047", at, budget)
		if err != nil || got != tc.want {
			t.Fatal(tc.text, got, err)
		}
	}
}

func TestFrontmatterGFMTerminalReparseKeepsSelection(t *testing.T) {
	ctx := context.Background()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	options, err := parser.NewFrontmatterOptions(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := "---\n- [x] metadata\n---\nordinary"
	before := parser.ParseWithOptions(ctx, parser.NewSource([]byte(text)), budget, parser.ParseOptions{GFM: true, Frontmatter: options})
	if got, err := terminalGFMEndingEligible(ctx, before, budget, options); err != nil || !got {
		t.Fatal("configured reparse lost header opacity", got, err)
	}
	legacy := parser.ParseGFMWithBudget(ctx, parser.NewSource([]byte(text+"\n")), budget)
	if got, err := TerminalTaskSourcesPreserved(ctx, before, legacy, budget); err != nil || got {
		t.Fatal("fixture does not witness the disabled-reparse regression", got, err)
	}
}
