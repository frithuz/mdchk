package parser_test

import (
	"context"
	"errors"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestIndentedHeadingsRejectsForeignAndConstructedAuthority(t *testing.T) {
	source := parser.NewSource([]byte("Text\n# H\nText"))
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	foreign, _ := parser.NewOperationBudget(parser.DefaultLimits())
	constructed, err := parser.NewDocument(parsed.Document.Root(), parsed.Document.References())
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	unfinished := parser.ParseWithBudget(cancelled, source, budget)
	for _, tc := range []struct {
		d *parser.Document
		b *parser.OperationBudget
	}{
		{parsed.Document, foreign}, {constructed, budget}, {unfinished.Document, budget},
	} {
		if facts, err := tc.d.IndentedHeadings(context.Background(), tc.b); err == nil || facts != nil {
			t.Fatal("foreign/unfinished authority", facts, err)
		}
	}
}

func TestIndentedHeadingsProjectionWithholdsInterruptedInventory(t *testing.T) {
	source := parser.NewSource([]byte(" >  # H\n\n # J"))
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	counter := &headingStopContext{Context: context.Background()}
	facts, err := parsed.Document.IndentedHeadings(counter, budget)
	if err != nil || len(facts) != 2 {
		t.Fatal(facts, err)
	}
	for _, stop := range []int{counter.calls / 2, counter.calls} {
		facts, err := parsed.Document.IndentedHeadings(&headingStopContext{Context: context.Background(), stop: stop}, budget)
		if !errors.Is(err, context.Canceled) || facts != nil {
			t.Fatal("partial inventory published", facts, err)
		}
	}
}

func TestIndentedHeadingsActualCursorAndOwnership(t *testing.T) {
	for _, tc := range []struct {
		source     string
		start, end int
	}{
		{">\t# H", 2, 3}, {"-  # H", -1, -1}, {"Alpha\n    Beta\n===", 15, 18},
		{" [x]: /url\nAlpha\n===", -1, -1}, {"> Alpha\n  Beta\n> ===", 17, 20},
	} {
		source := parser.NewSource([]byte(tc.source))
		token := source.Token()
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		parsed := parser.ParseWithBudget(context.Background(), source, budget)
		facts, err := parsed.Document.IndentedHeadings(context.Background(), budget)
		if err != nil {
			t.Fatal(err)
		}
		if tc.start < 0 {
			if len(facts) != 0 {
				t.Fatal(tc.source, facts)
			}
			continue
		}
		if len(facts) != 1 || facts[0].Start() != tc.start || facts[0].End() != tc.end || facts[0].SourceToken() != token {
			t.Fatal(tc.source, facts)
		}
		facts[0] = parser.Span{}
		again, err := parsed.Document.IndentedHeadings(context.Background(), budget)
		if err != nil || len(again) != 1 || again[0].Start() != tc.start || source.Token() != token || string(source.Bytes()) != tc.source {
			t.Fatal("mutable inventory or source", again, err)
		}
	}
}
