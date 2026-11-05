package resource

import (
	"slices"

	"github.com/frithuz/mdchk/internal/result"
)

// ExistenceReport consumes a logical result using the same incremental cause
// projector as FragmentReport. Append resolutions here once; do not also project
// their raw observations. One query yields at most one affected coverage record.
// Ownership and consuming semantics match FragmentReport.
type ExistenceReport struct {
	resolver *ExistenceResolver
	state    *filesystemReportState
}

func NewExistenceReport(resolver *ExistenceResolver, initial result.Result) (*ExistenceReport, error) {
	if resolver == nil || resolver.service == nil {
		return nil, ErrContract
	}
	owner, err := NewFilesystemReport(resolver.service.operation, initial)
	if err != nil {
		return nil, err
	}
	return &ExistenceReport{resolver, owner.state}, nil
}

func (owner *ExistenceReport) Append(view View, resolutions []ExistenceResolution) error {
	if owner == nil || owner.state == nil || owner.resolver == nil || owner.state.finished || view.operation != owner.resolver.service.operation || view.identity == nil {
		return ErrContract
	}
	seen := make(map[*existenceQueryState]bool, len(resolutions))
	// Canonicalization and full validation precede any result mutation.
	problems := make([][3][]Problem, len(resolutions))
	for i, resolution := range resolutions {
		e := resolution.state
		if e == nil || e.query == nil || e.query.resolver.service != owner.resolver.service || !e.query.resolver.normalizer.view.Same(view) || !e.query.consumed.Load() || seen[e.query] {
			return ErrContract
		}
		seen[e.query] = true
		for j, failure := range e.failures[:e.failureCount] {
			var err error
			problems[i][j], err = serviceFailureProblems(failure.err)
			if err != nil {
				return err
			}
		}
		var err error
		problems[i][2], err = serviceFailureProblems(e.controls)
		if err != nil {
			return err
		}
	}
	for i, resolution := range resolutions {
		e := resolution.state
		q := e.query
		key := e.finalCauseIdentity
		var ids []string
		add := func(identity string, p Problem) {
			if id := owner.state.problem(identity, p); id != "" && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		for _, observation := range e.observations[:e.count] {
			add(observation.request.Key().value, observation.problem)
		}
		for j, failure := range e.failures[:e.failureCount] {
			for _, problem := range problems[i][j] {
				add(failure.key, problem)
			}
		}
		for _, problem := range problems[i][2] {
			add(key, problem)
		}
		if e.outcome == ExistenceContextUnavailable {
			location, snapshot := q.location, q.snapshot
			owner.state.out.Coverage = append(owner.state.out.Coverage, result.Coverage{Path: q.normalized.link.path, Rule: q.normalized.link.rule, Range: &location, Snapshot: &snapshot, Reason: result.ContextUnavailable, Message: "Required resource context is unavailable.", CauseIDs: ids})
		}
	}
	return nil
}

func (owner *ExistenceReport) Finish() (result.Result, error) {
	if owner == nil || owner.state == nil {
		return result.Result{}, ErrContract
	}
	return (&FilesystemReport{state: owner.state}).Finish()
}
