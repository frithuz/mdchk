package parser_test

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
)

func TestEmphasisHeadingsRequireActualSourceOperation(t *testing.T) {
	source := parser.NewSource([]byte("**A**\n\nBody\n\n**B**\n\nBody"))
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	if parsed.Status != parser.Complete {
		t.Fatal(parsed)
	}
	facts, err := parsed.Document.EmphasisHeadings(context.Background(), budget, parser.NewLineIndex(source))
	if err != nil || len(facts) != 2 {
		t.Fatal(facts, err)
	}
	foreign, _ := parser.NewOperationBudget(parser.DefaultLimits())
	if _, err := parsed.Document.EmphasisHeadings(context.Background(), foreign, parser.NewLineIndex(source)); err == nil {
		t.Fatal("foreign budget manufactured verified headings")
	}
	constructed, err := parser.NewDocument(parsed.Document.Root(), parsed.Document.References())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := constructed.EmphasisHeadings(context.Background(), budget, parser.NewLineIndex(source)); err == nil {
		t.Fatal("constructed syntax manufactured recognition")
	}
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	unfinished := parser.ParseWithOptions(context.Background(), parser.NewSource([]byte("---\n# A!")), budget, parser.ParseOptions{Frontmatter: fm})
	if got, err := unfinished.Document.EmphasisHeadings(context.Background(), budget, parser.NewLineIndex(source)); err == nil || got != nil {
		t.Fatal("unfinished parse supplied inventory", got, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if partial, err := parsed.Document.EmphasisHeadings(cancelled, budget, parser.NewLineIndex(source)); !errors.Is(err, context.Canceled) || partial != nil {
		t.Fatal("cancelled collection published facts", partial, err)
	}
	if err := budget.ChargeNodes(budget.Limits().TotalNodes); err == nil {
		t.Fatal("actual node quota not exhausted")
	}
	var limit *parser.LimitExceeded
	if partial, err := parsed.Document.EmphasisHeadings(context.Background(), budget, parser.NewLineIndex(source)); !errors.As(err, &limit) || partial != nil {
		t.Fatal("quota became empty successful inventory", partial, err)
	}
}
