package parser

import (
	"context"
	"errors"
	"testing"
)

func TestGFMTaskSourcesAuthorityOwnershipAndBudget(t *testing.T) {
	ctx := context.Background()
	budget, _ := NewOperationBudget(DefaultLimits())
	source := NewSource([]byte("> - [X]\tvisible\n\n- [ ] next"))
	parsed := ParseGFMWithBudget(ctx, source, budget)
	tasks, err := parsed.Document.TaskSources(ctx, budget)
	if err != nil || len(tasks) != 2 {
		t.Fatal(tasks, err)
	}
	marker, _ := source.Text(tasks[0].Properties.Marker)
	separator, _ := source.Text(tasks[0].Properties.Separator)
	if marker != "[X]" || separator != "\t" || tasks[0].Properties.Marker.Start() != 4 || tasks[0].Properties.CheckSpelling != "X" || len(tasks[0].Content) == 0 {
		t.Fatal("source facts", tasks)
	}
	tasks[0].Properties.Segments[0] = Span{}
	tasks[0].Content[0] = Segment{}
	again, err := parsed.Document.TaskSources(ctx, budget)
	if err != nil || !again[0].Properties.Segments[0].Present() || !again[0].Content[0].Source.Present() {
		t.Fatal("borrowed inventory", again, err)
	}
	foreign, _ := NewOperationBudget(DefaultLimits())
	if _, err := parsed.Document.TaskSources(ctx, foreign); err == nil {
		t.Fatal("foreign operation authority")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := parsed.Document.TaskSources(canceled, budget); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := budget.ChargeWork(1 << 60); err == nil {
		t.Fatal("expected exhaustion")
	}
	if _, err := parsed.Document.TaskSources(ctx, budget); err == nil {
		t.Fatal("unmetered traversal")
	} else {
		var limit *LimitExceeded
		if !errors.As(err, &limit) || limit.Name != "work-units" {
			t.Fatal(err)
		}
	}
	coreBudget, _ := NewOperationBudget(DefaultLimits())
	core := ParseWithBudget(ctx, source, coreBudget)
	empty, err := core.Document.TaskSources(ctx, coreBudget)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal("known empty core inventory", empty, err)
	}
	limits := DefaultLimits()
	limits.WorkUnits = 100
	partialBudget, _ := NewOperationBudget(limits)
	partial := ParseGFMWithBudget(ctx, source, partialBudget)
	if partial.Document != nil {
		if _, err := partial.Document.TaskSources(ctx, partialBudget); err == nil {
			t.Fatal("partial authority")
		}
	}
}
