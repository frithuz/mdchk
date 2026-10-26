package parser_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestHeadingKeyRequiresCompletedOriginalOperation(t *testing.T) {
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	p := parser.ParseWithBudget(context.Background(), parser.NewSource([]byte("# A\n")), budget)
	hs, err := p.Document.Headings(context.Background(), budget)
	if err != nil {
		t.Fatal(err)
	}
	constructed, err := parser.NewDocument(p.Document.Root(), p.Document.References())
	if err != nil {
		t.Fatal(err)
	}
	if key, err := constructed.HeadingKey(context.Background(), budget, hs[0]); err == nil || key != "" {
		t.Fatal("constructed key authority", key, err)
	}
	foreign, _ := parser.NewOperationBudget(parser.DefaultLimits())
	if key, err := p.Document.HeadingKey(context.Background(), foreign, hs[0]); err == nil || key != "" {
		t.Fatal("foreign key allowance", key, err)
	}
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	unfinished := parser.ParseWithOptions(context.Background(), parser.NewSource([]byte("---\n# A")), budget, parser.ParseOptions{Frontmatter: fm})
	if unfinished.Status == parser.Complete {
		t.Fatal("unfinished fixture is complete")
	}
	if key, err := unfinished.Document.HeadingKey(context.Background(), budget, hs[0]); err == nil || key != "" {
		t.Fatal("unfinished key authority", key, err)
	}
}
