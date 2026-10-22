package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// Evaluation records actual callback execution independently of findings. Only
// a successful complete callback proves full original-source coverage. A callback
// with incomplete context or failure supplies no affirmative coverage claim.
type Evaluation struct {
	Rule, Active, Observational           string
	Reasons                               []string
	ActiveCoverage, ObservationalCoverage []parser.Span
}

// InspectionOptions selects optional observation and pre-callback projection.
type InspectionOptions struct {
	ObserveInactive bool
	// Prepare reserves/materializes source projections before callbacks can exhaust
	// their shared budget. It receives owned trace views and must use that budget.
	Prepare func(*InspectionTrace) error
}

// InspectionTrace owns observations of the shared lint pipeline. Accessors return
// detached collections; parser objects retain their immutable original source.
type InspectionTrace struct {
	prepare         func(*InspectionTrace) error
	parsed          parser.ParseResult
	lines           parser.LineIndex
	directives      DirectiveResult
	evaluations     []Evaluation
	observeInactive bool
	suppressed      []DirectiveSuppression
	errors          []result.Error
	interruption    error
	failures        []error
}

func (t *InspectionTrace) Parsed() parser.ParseResult {
	p := t.parsed
	p.Uncertainty = slices.Clone(p.Uncertainty)
	if p.Document != nil {
		copy := *p.Document
		p.Document = &copy
	}
	if p.Limit != nil {
		copy := *p.Limit
		p.Limit = &copy
	}
	if p.Failure != nil {
		copy := *p.Failure
		p.Failure = &copy
	}
	return p
}
func (t *InspectionTrace) Directives() DirectiveResult { return t.directives }
func (t *InspectionTrace) Evaluations() []Evaluation {
	out := slices.Clone(t.evaluations)
	for i := range out {
		out[i].Reasons = slices.Clone(out[i].Reasons)
		out[i].ActiveCoverage = slices.Clone(out[i].ActiveCoverage)
		out[i].ObservationalCoverage = slices.Clone(out[i].ObservationalCoverage)
	}
	return out
}
func (t *InspectionTrace) Suppressed() []DirectiveSuppression { return slices.Clone(t.suppressed) }
func (t *InspectionTrace) Errors() []result.Error {
	out := slices.Clone(t.errors)
	for i := range out {
		if p := out[i].Range; p != nil {
			copy := *p
			out[i].Range = &copy
		}
		if p := out[i].Snapshot; p != nil {
			copy := *p
			out[i].Snapshot = &copy
		}
		if p := out[i].ByteOffset; p != nil {
			copy := *p
			out[i].ByteOffset = &copy
		}
	}
	return out
}
func (t *InspectionTrace) Interruption() error {
	var limit *parser.LimitExceeded
	if errors.As(t.interruption, &limit) {
		copy := *limit
		return &copy
	}
	return t.interruption
}

// Failures retains actual callback causes for in-memory correction validation.
func (t *InspectionTrace) Failures() []error { return slices.Clone(t.failures) }

func (t *InspectionTrace) initialize(r *Registry) {
	for _, id := range r.ids {
		t.evaluations = append(t.evaluations, Evaluation{Rule: id, Active: "skipped", Observational: "not-requested", Reasons: []string{"context-unavailable"}, ActiveCoverage: []parser.Span{}, ObservationalCoverage: []parser.Span{}})
	}
}
func (t *InspectionTrace) entry(id string) *Evaluation {
	for i := range t.evaluations {
		if t.evaluations[i].Rule == id {
			return &t.evaluations[i]
		}
	}
	return nil
}
func (t *InspectionTrace) encoding(err *result.Error, source parser.Source) {
	if e := t.entry("MD094"); e != nil {
		e.Active = "complete"
		span, _ := source.Span(0, source.Len())
		e.ActiveCoverage = []parser.Span{span}
		e.Reasons = []string{}
		if err != nil {
			e.Active = "failed"
			e.ActiveCoverage = []parser.Span{}
			e.Reasons = []string{"decoding-failed"}
		}
	}
}
func (t *InspectionTrace) skip(id, reason string) { e := t.entry(id); e.Reasons = []string{reason} }
func (t *InspectionTrace) ran(id string, outcome RuleOutcome, observational bool, size int) {
	e := t.entry(id)
	if outcome.Err != nil {
		t.failures = append(t.failures, outcome.Err)
	}
	status := "complete"
	e.Reasons = []string{}
	if outcome.IncompleteReason != "" {
		status = "partial"
		e.Reasons = append(e.Reasons, "context-unavailable")
	}
	if outcome.Err != nil {
		status = "failed"
		e.Reasons = append(e.Reasons, "rule-failure")
		if isInterruption(outcome.Err) {
			status = "partial"
			e.Reasons = append(e.Reasons, "interrupted")
		}
	}
	coverage := []parser.Span{}
	if status == "complete" {
		span, _ := t.parsed.Source.Span(0, size)
		coverage = append(coverage, span)
	}
	if observational {
		e.Observational = status
		e.ObservationalCoverage = coverage
	} else {
		e.Active = status
		e.ActiveCoverage = coverage
	}
}
func (t *InspectionTrace) observe(ctx context.Context, r *Registry, rc RuleContext, parsed parser.ParseResult, directives DirectiveResult, projection *projector, snap result.Snapshot, reliable bool) {
	if !t.observeInactive {
		return
	}
	for _, id := range r.ids {
		if id == "MD094" || id == "MD087" || directives.EnabledSomewhere(id) {
			continue
		}
		e := t.entry(id)
		e.Observational = "skipped"
		m := r.metadata[id]
		switch {
		case m.AppliesTo != nil && !m.AppliesTo(rc.input.documentContext):
			e.Reasons = []string{"inapplicable"}
			continue
		case r.checks[id] == nil:
			e.Reasons = []string{"rule-unavailable"}
			continue
		case m.RequiresSyntax && !reliable:
			e.Reasons = []string{"parser-uncertain"}
			continue
		}
		if err := rc.Checkpoint(ctx, 1); err != nil {
			t.interruption = err
			return
		}
		rc.options = directives.rules[id].Options
		rc.document = parsed.Document
		if !reliable {
			rc.document = nil
		}
		outcome := r.checks[id](ctx, rc)
		recorded := outcome
		if recorded.Err == nil {
			recorded.Err = rc.Checkpoint(ctx, 0)
		}
		t.ran(id, recorded, true, rc.Source().Len())
		for _, f := range outcome.Findings {
			d, err := makeDiagnostic(rc.input, projection, snap, m, f)
			if err != nil {
				if isInterruption(err) {
					t.interruption = err
					return
				}
				t.invalid(id, true)
				t.errors = append(t.errors, result.Error{Category: result.Internal, Code: "invalid-finding", Scope: "operation", Path: rc.Path(), Rule: id, Message: err.Error()})
				continue
			}
			state, err := directives.StateAt(id, f.Span.Start())
			if err != nil {
				t.errors = append(t.errors, result.Error{Category: result.Internal, Code: "directive-state", Scope: "operation", Path: rc.Path(), Rule: id, Message: err.Error()})
				continue
			}
			t.suppressed = append(t.suppressed, DirectiveSuppression{Diagnostic: d, Owner: state.Owner, Token: state.Token})
		}
		if outcome.Err != nil {
			if isInterruption(outcome.Err) {
				t.interruption = outcome.Err
				return
			}
			t.errors = append(t.errors, result.Error{Category: result.Internal, Code: "observational-rule-failure", Scope: "operation", Path: rc.Path(), Rule: id, Message: outcome.Err.Error()})
		}
		if err := rc.Checkpoint(ctx, 0); err != nil {
			t.interruption = err
			return
		}
	}
}

// Inspect executes exactly one admitted original input through Lint and retains
// parsing and actual scheduling observations. It does not reset the shared meter.
func (p *ExecutionPlan) Inspect(ctx context.Context, in Input, options RunOptions, inspection InspectionOptions) (result.Result, *InspectionTrace, error) {
	if p == nil || p.registry == nil || in.plan != p {
		return result.Result{}, nil, fmt.Errorf("input does not belong to inspection plan")
	}
	trace := &InspectionTrace{observeInactive: inspection.ObserveInactive, prepare: inspection.Prepare}
	options.inspection = trace
	out, err := p.Lint(ctx, []Input{in}, options)
	return out, trace, err
}

func (t *InspectionTrace) invalid(id string, observational bool) {
	e := t.entry(id)
	if observational {
		e.Observational = "failed"
		e.ObservationalCoverage = []parser.Span{}
	} else {
		e.Active = "failed"
		e.ActiveCoverage = []parser.Span{}
	}
	e.Reasons = append(e.Reasons, "invalid-finding")
}

// InspectCandidate runs shared analysis with candidate snapshot identity on every
// located observation. Settings, context and the residual budget remain unchanged.
func (p *ExecutionPlan) InspectCandidate(ctx context.Context, in Input, options RunOptions) (result.Result, *InspectionTrace, error) {
	options.candidate = true
	return p.Inspect(ctx, in, options, InspectionOptions{})
}

// Lines retains the original shared physical-line index without rescanning.
func (t *InspectionTrace) Lines() parser.LineIndex { return t.lines }

// InspectSaved analyzes an actually observed saved snapshot using the residual
// operation budget. It does not infer saved bytes from a validated candidate.
func (p *ExecutionPlan) InspectSaved(ctx context.Context, in Input, options RunOptions) (result.Result, *InspectionTrace, error) {
	options.saved = true
	return p.Inspect(ctx, in, options, InspectionOptions{})
}
