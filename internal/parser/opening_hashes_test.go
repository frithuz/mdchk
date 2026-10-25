package parser_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestOpeningHashCandidatesOriginalSourceOperation(t *testing.T) {
	const text = "> - ##雪\r\n\r\ntext `open\r\n##`\r\n##good"
	source := parser.NewSource([]byte(text))
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	if parsed.Status != parser.Complete {
		t.Fatal(parsed)
	}
	before := parsed.Document.Root()
	points, err := parsed.Document.OpeningHashCandidates(context.Background(), budget)
	if err != nil || len(points) != 2 || points[0].Start() != 6 || points[0].End() != 6 || points[1].Start() != 32 || points[1].End() != 32 {
		t.Fatal(points, err)
	}
	for _, point := range points {
		if point.SourceToken() != source.Token() {
			t.Fatal("foreign source point")
		}
	}
	foreignSource := parser.NewSource(source.Bytes())
	if _, err := foreignSource.Text(points[0]); err == nil {
		t.Fatal("equal bytes manufactured ownership")
	}
	points[0] = parser.Span{}
	again, err := parsed.Document.OpeningHashCandidates(context.Background(), budget)
	if err != nil || again[0].Start() != 6 || string(source.Bytes()) != text || !reflect.DeepEqual(before, parsed.Document.Root()) {
		t.Fatal("mutated inventory/source/tree", again, err)
	}
	foreign, _ := parser.NewOperationBudget(parser.DefaultLimits())
	constructed, err := parser.NewDocument(parsed.Document.Root(), parsed.Document.References())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		document *parser.Document
		budget   *parser.OperationBudget
	}{{parsed.Document, foreign}, {constructed, budget}} {
		if _, err := tc.document.OpeningHashCandidates(context.Background(), tc.budget); err == nil {
			t.Fatal("manufactured recognition")
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if partial, err := parsed.Document.OpeningHashCandidates(cancelled, budget); !errors.Is(err, context.Canceled) || partial != nil {
		t.Fatal(partial, err)
	}
	_ = budget.ChargeNodes(budget.Limits().TotalNodes)
	var limit *parser.LimitExceeded
	if partial, err := parsed.Document.OpeningHashCandidates(context.Background(), budget); !errors.As(err, &limit) || partial != nil {
		t.Fatal(partial, err)
	}
	unfinishedBudget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	unfinished := parser.ParseWithBudget(cancelled, source, unfinishedBudget)
	if partial, err := unfinished.Document.OpeningHashCandidates(context.Background(), unfinishedBudget); err == nil || partial != nil {
		t.Fatal(partial, err)
	}
}

// Fail immediately before publication as well as during filtering, after work
// has already accumulated. Neither stop may return complete-looking facts.
type openingHashCancellation struct {
	context.Context
	calls, stop int
}

func (c *openingHashCancellation) Err() error {
	c.calls++
	if c.stop != 0 && c.calls >= c.stop {
		return context.Canceled
	}
	return nil
}
func TestOpeningHashCandidatesWithholdInterruptedInventory(t *testing.T) {
	source := parser.NewSource([]byte("##first\ntext `open\n##hidden\nclose`\n##last"))
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	baseline := &openingHashCancellation{Context: context.Background()}
	points, err := parsed.Document.OpeningHashCandidates(baseline, budget)
	if err != nil || len(points) != 2 {
		t.Fatal(points, err)
	}
	for _, stop := range []int{baseline.calls / 2, baseline.calls} {
		ctx := &openingHashCancellation{Context: context.Background(), stop: stop}
		points, err := parsed.Document.OpeningHashCandidates(ctx, budget)
		if !errors.Is(err, context.Canceled) || points != nil {
			t.Fatal("partial publication", points, err)
		}
	}
}
