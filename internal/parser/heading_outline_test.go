package parser_test

import (
	"context"
	"errors"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestHeadingOutlineCheckpointActualSourceOperation(t *testing.T) {
	source := parser.NewSource([]byte("# A"))
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	index := parser.NewLineIndex(source)
	if err := parsed.Document.HeadingOutlineCheckpoint(context.Background(), budget, index, 1, 1); err != nil {
		t.Fatal(err)
	}
	foreign, _ := parser.NewOperationBudget(parser.DefaultLimits())
	if err := parsed.Document.HeadingOutlineCheckpoint(context.Background(), foreign, index, 0, 0); err == nil {
		t.Fatal("foreign budget")
	}
	other := parser.NewLineIndex(parser.NewSource([]byte("# A")))
	if err := parsed.Document.HeadingOutlineCheckpoint(context.Background(), budget, other, 0, 0); err == nil {
		t.Fatal("foreign equal-byte index")
	}
	constructed, err := parser.NewDocument(parsed.Document.Root(), parsed.Document.References())
	if err != nil {
		t.Fatal(err)
	}
	if err := constructed.HeadingOutlineCheckpoint(context.Background(), budget, index, 0, 0); err == nil {
		t.Fatal("constructed document")
	}
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	_ = budget.ChargeNodes(budget.Limits().TotalNodes)
	err = parsed.Document.HeadingOutlineCheckpoint(stopped, budget, index, 0, 0)
	var limit *parser.LimitExceeded
	if !errors.Is(err, context.Canceled) || !errors.As(err, &limit) {
		t.Fatal(err)
	}
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	unfinishedSource := parser.NewSource([]byte("---\n# A"))
	newBudget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	unfinished := parser.ParseWithOptions(context.Background(), unfinishedSource, newBudget, parser.ParseOptions{Frontmatter: fm})
	if err := unfinished.Document.HeadingOutlineCheckpoint(context.Background(), newBudget, parser.NewLineIndex(unfinishedSource), 0, 0); err == nil {
		t.Fatal("unfinished document")
	}
}
