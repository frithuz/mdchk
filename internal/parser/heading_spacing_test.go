package parser_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestHeadingSpacingOwnershipAndCounts(t *testing.T) {
	for _, tc := range []struct {
		source        string
		before, after int
		first, last   int
	}{
		{"Text\n>\n> - # H\n>\nTail", 1, 1, 7, 14},
		{"Text\n\n-\n# H\n-\n\nTail", 2, 2, 8, 11},
		{"Text\n2.\n# H\nTail", 0, 0, 8, 11},
		{"Text\n\nAlpha\nBeta\n===\nTail", 1, 0, 6, 20},
	} {
		source := parser.NewSource([]byte(tc.source))
		index := parser.NewLineIndex(source)
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		parsed := parser.ParseWithBudget(context.Background(), source, budget)
		facts, err := parsed.Document.HeadingSpacing(context.Background(), budget, index)
		if err != nil || len(facts) != 1 {
			t.Fatal(tc.source, facts, err)
		}
		f := facts[0]
		if f.BeforeBlank != tc.before || f.AfterBlank != tc.after || f.Before.Start() != tc.first || f.After.Start() != tc.last || f.BeforeEdge || f.AfterEdge || f.Before.SourceToken() != source.Token() || f.After.SourceToken() != source.Token() {
			t.Fatalf("%q: %+v", tc.source, f)
		}
	}
}

func TestHeadingSpacingRejectsForeignAndConstructedAuthority(t *testing.T) {
	source := parser.NewSource([]byte("Text\n# H\nText"))
	index := parser.NewLineIndex(source)
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
		i parser.LineIndex
	}{
		{parsed.Document, foreign, index}, {constructed, budget, index}, {unfinished.Document, budget, index},
		{parsed.Document, budget, parser.NewLineIndex(parser.NewSource([]byte("Text\n# H\nText")))},
	} {
		if facts, err := tc.d.HeadingSpacing(context.Background(), tc.b, tc.i); err == nil || facts != nil {
			t.Fatal("foreign/unfinished authority", facts, err)
		}
	}
}

func TestHeadingSpacingProjectionWithholdsInterruptedInventory(t *testing.T) {
	source := parser.NewSource([]byte("Text\n>\n> # H\n>\nText\n# J\nText"))
	index := parser.NewLineIndex(source)
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	counter := &headingStopContext{Context: context.Background()}
	facts, err := parsed.Document.HeadingSpacing(counter, budget, index)
	if err != nil || len(facts) != 2 {
		t.Fatal(facts, err)
	}
	for _, stop := range []int{counter.calls / 2, counter.calls} {
		facts, err := parsed.Document.HeadingSpacing(&headingStopContext{Context: context.Background(), stop: stop}, budget, index)
		if !errors.Is(err, context.Canceled) || facts != nil {
			t.Fatal("partial inventory published", facts, err)
		}
	}
}

func TestHeadingSpacingLongScanCheckpoints(t *testing.T) {
	source := parser.NewSource([]byte("Text\n" + strings.Repeat(" \t", 1024) + "\n# H\nText"))
	index := parser.NewLineIndex(source)
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	counter := &headingStopContext{Context: context.Background()}
	facts, err := parsed.Document.HeadingSpacing(counter, budget, index)
	if err != nil || len(facts) != 1 || facts[0].BeforeBlank != 1 {
		t.Fatal(facts, err)
	}
	for stop := 1; stop <= counter.calls; stop++ {
		facts, err := parsed.Document.HeadingSpacing(&headingStopContext{Context: context.Background(), stop: stop}, budget, index)
		if !errors.Is(err, context.Canceled) || facts != nil {
			t.Fatal("long scan/final check published partial facts", stop, facts, err)
		}
	}
}
