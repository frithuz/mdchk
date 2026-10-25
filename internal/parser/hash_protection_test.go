package parser

import (
	"context"
	"reflect"
	"testing"
)

func TestHashProtectionUsesWholeRunAndAdjacentBoundary(t *testing.T) {
	source := NewSource([]byte("# `x`###"))
	budget, _ := NewOperationBudget(DefaultLimits())
	parsed := ParseWithBudget(context.Background(), source, budget)
	// Independent bounded queries touch the code's beginning/end. A point at
	// byte 1 is outside code, but the first range overlaps its envelope.
	overlap, _ := source.Span(1, 3)
	adjacent, _ := source.Span(5, 8)
	got, err := parsed.Document.protectedHashRuns(context.Background(), budget, []Span{overlap, adjacent})
	if err != nil || !reflect.DeepEqual(got, []bool{true, false}) {
		t.Fatal(got, err)
	}
}
