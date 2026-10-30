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
// a successful complete callback proves its eligible original-source coverage.
// Ordinary coverage excludes an opaque header; explicit metadata coverage can
// include it. A callback
// with incomplete context or failure supplies no affirmative coverage claim.
type Evaluation struct {
	Rule, Active, Observational           string
	Reasons                               []string
	ActiveCoverage, ObservationalCoverage []parser.Span
}

// InspectionOptions selects optional observation and pre-callback projection.
type InspectionOptions struct {
	ObserveInactive bool
	// PublishSuppression updates a prepared presentation using bounded indices.
	// It must not inspect source, allocate, or execute a provider. The engine
	// charges each invocation before publication.
	PublishSuppression func(SuppressionDecision)
	// Prepare reserves/materializes source projections before callbacks can exhaust
	// their shared budget. It receives owned trace views and must use that budget.
	Prepare func(*InspectionTrace) error
}

// InspectionTrace owns observations of the shared lint pipeline. Accessors return
// detached collections; parser objects retain their immutable original source.
type InspectionTrace struct {
	publishSuppression func(SuppressionDecision)
	audits             []SuppressionDecision
	auditOperation     context.Context
	prepare            func(*InspectionTrace) error
	documentContext    DocumentContext
	contextKnown       bool
	preassessed        map[string]bool
	parsed             parser.ParseResult
	lines              parser.LineIndex
	directives         DirectiveResult
	evaluations        []Evaluation
	observeInactive    bool
	suppressed         []DirectiveSuppression
	errors             []result.Error
	interruption       error
	failures           []error
}

// SuppressionDecisions copies the source-local records using the originating
// meter. Prepared inspection calls this before callbacks, then accepts charged
// scalar publications; it never rescans source after interruption.
func (t *InspectionTrace) SuppressionDecisions(ctx context.Context, budget *parser.OperationBudget) ([]SuppressionDecision, error) {
	if ctx == nil || budget == nil || budget.Token() != t.directives.operation {
		return nil, parser.ErrModel
	}
	if t.auditOperation != nil {
		if err := t.auditOperation.Err(); err != nil {
			return nil, err
		}
	} else if len(t.audits) != 0 {
		return nil, parser.ErrModel
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := budget.ChargeWork(int64(len(t.audits)) + 1); err != nil {
		return nil, err
	}
	if err := budget.ChargeNodes(int64(len(t.audits))); err != nil {
		return nil, err
	}
	return slices.Clone(t.audits), nil
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
func (t *InspectionTrace) DocumentContext() (DocumentContext, bool) {
	return t.documentContext, t.contextKnown
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
func (t *InspectionTrace) ran(id string, outcome RuleOutcome, observational bool, size int, usesFrontmatter bool) {
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
		start := 0
		if !usesFrontmatter && t.parsed.Document != nil {
			if header, present := t.parsed.Document.Frontmatter(); present {
				start = header.BodyOffset()
			}
		}
		span, _ := t.parsed.Source.Span(start, size)
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

// Inspect executes exactly one admitted original input through Lint and retains
// parsing and actual scheduling observations. It does not reset the shared meter.
func (p *ExecutionPlan) Inspect(ctx context.Context, in Input, options RunOptions, inspection InspectionOptions) (result.Result, *InspectionTrace, error) {
	if p == nil || p.registry == nil || in.plan != p {
		return result.Result{}, nil, fmt.Errorf("input does not belong to inspection plan")
	}
	trace := &InspectionTrace{observeInactive: inspection.ObserveInactive, prepare: inspection.Prepare, publishSuppression: inspection.PublishSuppression}
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
	// One target executes once; repeated invalid findings do not duplicate
	// this finite execution reason or grow an unbounded reason inventory.
	if len(e.Reasons) == 0 || e.Reasons[len(e.Reasons)-1] != "invalid-finding" {
		e.Reasons = append(e.Reasons, "invalid-finding")
	}
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
