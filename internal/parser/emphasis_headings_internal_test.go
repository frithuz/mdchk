package parser

import (
	"context"
	"testing"
)

func TestEmphasisHeadingsMissingSemanticIsError(t *testing.T) {
	source := NewSource([]byte("**A**\n\nBody"))
	budget, _ := NewOperationBudget(DefaultLimits())
	parsed := ParseWithBudget(context.Background(), source, budget)
	parsed.Document.root.spec.Children[0].spec.Children[0].spec.Children[0].spec.Properties.Literal = Optional[string]{}
	if got, err := parsed.Document.EmphasisHeadings(context.Background(), budget, NewLineIndex(source)); err == nil || got != nil {
		t.Fatal(got, err)
	}
}
