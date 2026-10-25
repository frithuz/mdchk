package parser_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestClosingHashCandidatesOriginalSourceOperation(t *testing.T) {
	const text = "> - ## 雪##\r\n# last##"
	source := parser.NewSource([]byte(text))
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	if parsed.Status != parser.Complete {
		t.Fatal(parsed)
	}
	before := parsed.Document.Root()
	points, err := parsed.Document.ClosingHashCandidates(context.Background(), budget)
	if err != nil || len(points) != 2 || points[0].Start() != 10 || points[0].End() != 10 || points[1].Start() != 20 || points[1].End() != 20 {
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
	again, err := parsed.Document.ClosingHashCandidates(context.Background(), budget)
	if err != nil || again[0].Start() != 10 || string(source.Bytes()) != text || !reflect.DeepEqual(before, parsed.Document.Root()) {
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
		if _, err := tc.document.ClosingHashCandidates(context.Background(), tc.budget); err == nil {
			t.Fatal("manufactured recognition")
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if partial, err := parsed.Document.ClosingHashCandidates(cancelled, budget); !errors.Is(err, context.Canceled) || partial != nil {
		t.Fatal(partial, err)
	}
	_ = budget.ChargeNodes(budget.Limits().TotalNodes)
	var limit *parser.LimitExceeded
	if partial, err := parsed.Document.ClosingHashCandidates(context.Background(), budget); !errors.As(err, &limit) || partial != nil {
		t.Fatal(partial, err)
	}
	unfinishedBudget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	unfinished := parser.ParseWithBudget(cancelled, source, unfinishedBudget)
	if partial, err := unfinished.Document.ClosingHashCandidates(context.Background(), unfinishedBudget); err == nil || partial != nil {
		t.Fatal(partial, err)
	}
}

// Fail immediately before publication as well as during filtering, after work
// has already accumulated. Neither stop may return complete-looking facts.
type closingHashCancellation struct {
	context.Context
	calls, stop int
}

func (c *closingHashCancellation) Err() error {
	c.calls++
	if c.stop != 0 && c.calls >= c.stop {
		return context.Canceled
	}
	return nil
}
func TestClosingHashCandidatesWithholdInterruptedInventory(t *testing.T) {
	source := parser.NewSource([]byte("# first##\n# <script>hidden###\n\n# last##"))
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	baseline := &closingHashCancellation{Context: context.Background()}
	points, err := parsed.Document.ClosingHashCandidates(baseline, budget)
	if err != nil || len(points) != 1 {
		t.Fatal(points, err)
	}
	for _, stop := range []int{baseline.calls / 2, baseline.calls} {
		ctx := &closingHashCancellation{Context: context.Background(), stop: stop}
		points, err := parsed.Document.ClosingHashCandidates(ctx, budget)
		if !errors.Is(err, context.Canceled) || points != nil {
			t.Fatal("partial publication", points, err)
		}
	}
}

func TestClosingHashFixturesContainActualNUL(t *testing.T) {
	// The public projection must use original bounds, despite normalized NUL
	// contributions expanding semantic text to a three-byte replacement rune.
	source := parser.NewSource([]byte("# x\x00##"))
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	points, err := parsed.Document.ClosingHashCandidates(context.Background(), budget)
	if err != nil || len(points) != 1 || points[0].Start() != 4 || points[0].End() != 4 || source.Bytes()[3] != 0 {
		t.Fatal(points, err)
	}
}
