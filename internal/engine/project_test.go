package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// One projector serves the operation: an exhausted budget or cancelled
// context is retained as Stop, and the single reporting reserve it falls back
// to never restarts, so projection ends instead of running unmetered.
func TestSpanProjectorSharesOperationControls(t *testing.T) {
	source := parser.NewSource([]byte(strings.Repeat("x", 1<<20) + "\nnav: []\n"))
	limits := parser.DefaultLimits()
	limits.WorkUnits = 4 << 20
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	projector := engine.NewSpanProjector(context.Background(), budget, source)
	span, _ := source.Span(1<<20+6, 1<<20+8)
	got, bytes, snap, err := projector.Project(span)
	if err != nil || got.Start.Line != 2 || got.Start.Column != 6 || bytes.Start != 1<<20+6 || snap.SizeBytes != source.Len() {
		t.Fatal(got, bytes, snap, err)
	}
	if projector.Stop() != nil {
		t.Fatal("stopped within budget", projector.Stop())
	}
	stopped := 0
	for offset := 0; offset < 1<<20; offset += 997 {
		span, _ := source.Span(offset, offset)
		if _, _, _, err = projector.Project(span); err != nil {
			break
		}
		stopped++
	}
	var limit *parser.LimitExceeded
	if err == nil || !errors.As(projector.Stop(), &limit) || limit.Name != "work-units" || stopped >= 300 {
		t.Fatal("projection outran the operation and its reserve", stopped, err, projector.Stop())
	}
	if _, _, _, again := projector.Project(span); again == nil {
		t.Fatal("projection restarted after the reserve stopped")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fresh, _ := parser.NewOperationBudget(parser.DefaultLimits())
	cancelled := engine.NewSpanProjector(ctx, fresh, source)
	if _, _, _, err := cancelled.Project(span); err != nil || !errors.Is(cancelled.Stop(), context.Canceled) {
		t.Fatal("cancellation was not retained", err, cancelled.Stop())
	}
}
