package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func TestInspectionSchedulingCoverageAndInapplicability(t *testing.T) {
	calls := 0
	r, err := NewRegistry([]Metadata{{ID: "MD001", Name: "clean", DefaultEnabled: true, Fix: NoFix}, {ID: "MD003", Name: "unavailable", DefaultEnabled: true, Fix: NoFix}, {ID: "MD004", Name: "inapplicable", DefaultEnabled: true, Fix: NoFix, AppliesTo: func(DocumentContext) bool { return false }}}, []Registration{{ID: "MD001", Check: func(context.Context, RuleContext) RuleOutcome { calls++; return RuleOutcome{} }}, {ID: "MD004", Check: func(context.Context, RuleContext) RuleOutcome {
		t.Fatal("inapplicable callback ran")
		return RuleOutcome{}
	}}})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := r.SelectRules(nil, ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(DocumentContext{Flavor: "commonmark"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := parser.NewSource([]byte("clean\n"))
	in, err := plan.NewSourceInput("x.md", source)
	if err != nil {
		t.Fatal(err)
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	out, trace, err := plan.Inspect(context.Background(), in, RunOptions{Budget: budget}, InspectionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	evaluations := trace.Evaluations()
	if calls != 1 || len(out.Diagnostics) != 0 || evaluations[0].Active != "complete" || len(evaluations[0].ActiveCoverage) != 1 || evaluations[1].Reasons[0] != "rule-unavailable" || evaluations[2].Reasons[0] != "inapplicable" {
		t.Fatal(calls, evaluations)
	}
	parsed := trace.Parsed()
	*parsed.Document = parser.Document{}
	evaluations[0].Reasons = append(evaluations[0].Reasons, "mutated")
	evaluations[0].ActiveCoverage[0] = parser.Span{}
	if trace.Parsed().Document.Root() == nil || !trace.Evaluations()[0].ActiveCoverage[0].Present() {
		t.Fatal("trace view aliases retained state")
	}
}
func TestInspectionNestedFailureViewsAreOwned(t *testing.T) {
	position := result.Range{Start: parser.Position{Line: 1, Column: 1}, End: parser.Position{Line: 1, Column: 1}}
	snapshot := result.Snapshot{View: "original", SizeBytes: 5}
	offset := 2
	limit := &parser.LimitExceeded{Name: "work-units", Observed: 10, Allowed: 9}
	trace := &InspectionTrace{errors: []result.Error{{Range: &position, Snapshot: &snapshot, ByteOffset: &offset}}, interruption: limit, parsed: parser.ParseResult{Limit: limit, Failure: &parser.Failure{Code: parser.InternalFailure, Message: "retained"}}}
	view := trace.Errors()
	view[0].Range.Start.Line = 99
	view[0].Snapshot.SizeBytes = 100
	*view[0].ByteOffset = 4
	parsed := trace.Parsed()
	parsed.Limit.Name = "mutated"
	parsed.Failure.Message = "mutated"
	var returned *parser.LimitExceeded
	if !errors.As(trace.Interruption(), &returned) {
		t.Fatal("limit identity lost")
	}
	returned.Name = "mutated"
	fresh := trace.Errors()
	if fresh[0].Range.Start.Line != 1 || fresh[0].Snapshot.SizeBytes != 5 || *fresh[0].ByteOffset != 2 || trace.Parsed().Limit.Name != "work-units" || trace.Parsed().Failure.Message != "retained" {
		t.Fatal("nested trace aliases retained observations")
	}
}
