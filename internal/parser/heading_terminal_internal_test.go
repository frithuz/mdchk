package parser

import (
	"context"
	"testing"
)

func TestHeadingTerminalsUnavailableMappingIsError(t *testing.T) {
	budget, _ := NewOperationBudget(DefaultLimits())
	p := ParseWithBudget(context.Background(), NewSource([]byte("# A!")), budget)
	text := p.Document.root.spec.Children[0].spec.Children[0]
	text.spec.Content[0].Transformation = MappingUnavailable
	if got, err := p.Document.HeadingTerminals(context.Background(), budget); err == nil || got != nil {
		t.Fatal(got, err)
	}
}
