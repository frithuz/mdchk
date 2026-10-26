package parser_test

import (
	"context"
	"errors"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestOpeningBlockActualSourceOperation(t *testing.T) {
	source := parser.NewSource([]byte("<!-- intro -->\n# A"))
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	p := parser.ParseWithBudget(context.Background(), source, budget)
	fact, err := p.Document.OpeningBlock(context.Background(), budget, parser.NewLineIndex(source))
	if err != nil || fact.Level != 1 || fact.Point.Start() != 15 || fact.Point.End() != 15 || fact.Point.SourceToken() != source.Token() {
		t.Fatal(fact, err)
	}
	foreign, _ := parser.NewOperationBudget(parser.DefaultLimits())
	constructed, _ := parser.NewDocument(p.Document.Root(), p.Document.References())
	for _, tc := range []struct {
		d *parser.Document
		b *parser.OperationBudget
		i parser.LineIndex
	}{
		{p.Document, foreign, parser.NewLineIndex(source)},
		{constructed, budget, parser.NewLineIndex(source)},
		{p.Document, budget, parser.NewLineIndex(parser.NewSource([]byte("<!-- intro -->\n# A")))},
	} {
		if got, err := tc.d.OpeningBlock(context.Background(), tc.b, tc.i); err == nil || got.Point.Present() {
			t.Fatal(got, err)
		}
	}
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	unfinished := parser.ParseWithOptions(context.Background(), parser.NewSource([]byte("---\n# A")), budget, parser.ParseOptions{Frontmatter: fm})
	if got, err := unfinished.Document.OpeningBlock(context.Background(), budget, parser.NewLineIndex(source)); err == nil || got.Point.Present() {
		t.Fatal(got, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := p.Document.OpeningBlock(cancelled, budget, parser.NewLineIndex(source)); !errors.Is(err, context.Canceled) || got.Point.Present() {
		t.Fatal(got, err)
	}
	if err := budget.ChargeNodes(budget.Limits().TotalNodes); err == nil {
		t.Fatal("quota not exhausted")
	}
	var limit *parser.LimitExceeded
	if got, err := p.Document.OpeningBlock(context.Background(), budget, parser.NewLineIndex(source)); !errors.As(err, &limit) || got.Point.Present() {
		t.Fatal(got, err)
	}
}
