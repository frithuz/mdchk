package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

type projectionCancelContext struct {
	context.Context
	remaining int
}

func (c *projectionCancelContext) Err() error {
	c.remaining--
	if c.remaining <= 0 {
		return context.Canceled
	}
	return nil
}

func TestProjectionObservesCancellationBetweenEndpoints(t *testing.T) {
	source := parser.NewSource([]byte("π\tword"))
	span, _ := source.Span(2, 3)
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	reserve := &reportingReserve{}
	p := projector{ctx: &projectionCancelContext{Context: context.Background(), remaining: 3}, source: source, index: parser.NewLineIndex(source), budget: budget, reporting: reserve}
	in := Input{path: "a", source: source}
	d, err := makeDiagnostic(in, &p, snapshot(source), Metadata{ID: "MD001", Fix: NoFix}, Finding{Message: "tab", Span: span})
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(p.stop, context.Canceled) || reserve.budget == nil {
		t.Fatal("cancellation within projection was not observed")
	}
	if d.Range.Start != (parser.Position{Line: 1, Column: 2}) || d.Range.End != (parser.Position{Line: 1, Column: 3}) {
		t.Fatal(d)
	}
}

func TestProjectionReserveIsNotResetAcrossProjectors(t *testing.T) {
	source := parser.NewSource(nil)
	span, _ := source.Span(0, 0)
	limits := parser.DefaultLimits()
	limits.WorkUnits = 6
	reportingBudget, _ := parser.NewOperationBudget(limits)
	reserve := &reportingReserve{budget: reportingBudget}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	newProjection := func() projector {
		return projector{ctx: context.Background(), budget: budget, reporting: reserve, source: source, index: parser.NewLineIndex(source), stop: context.Canceled}
	}
	first := newProjection()
	if _, err := makeDiagnostic(Input{path: "a", source: source}, &first, snapshot(source), Metadata{ID: "MD001"}, Finding{Message: "a", Span: span}); err != nil {
		t.Fatal(err)
	}
	second := newProjection()
	if _, err := makeDiagnostic(Input{path: "b", source: source}, &second, snapshot(source), Metadata{ID: "MD001"}, Finding{Message: "b", Span: span}); !omittedProjection(err) {
		t.Fatalf("reserve reset between targets: %v", err)
	}
}

func TestIncompleteCoverageLocatesOnlyAValidSpan(t *testing.T) {
	source := parser.NewSource([]byte("one\ntwo\n"))
	span, _ := source.Span(4, 7)
	foreign, _ := parser.NewSource([]byte("one\ntwo\n")).Span(4, 7)
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	projection := projector{ctx: context.Background(), budget: budget, source: source, index: parser.NewLineIndex(source)}
	snap := snapshot(source)
	in := Input{path: "a", source: source}
	located := incompleteCoverage(in, &projection, snap, "MD072", RuleOutcome{IncompleteReason: "why", IncompleteSpan: span})
	if located.Message != "why" || located.Range == nil || located.Range.Start.Line != 2 || located.Range.End.Column != 4 || located.Snapshot == nil || *located.Snapshot != snap {
		t.Fatalf("%+v", located)
	}
	for _, outcome := range []RuleOutcome{{IncompleteReason: "why"}, {IncompleteReason: "why", IncompleteSpan: foreign}} {
		if plain := incompleteCoverage(in, &projection, snap, "MD072", outcome); plain.Message != "why" || plain.Range != nil || plain.Snapshot != nil {
			t.Fatalf("%+v", plain)
		}
	}
}
