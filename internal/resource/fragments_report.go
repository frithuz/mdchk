package resource

import (
	"slices"

	"github.com/frithuz/mdchk/internal/result"
)

// FragmentReport consumes a logical result using the same incremental cause
// projector as ServiceReport. Append resolutions here once; do not also project
// their raw observations. One query yields at most one affected coverage record,
// even if both acquisition and its later named lookup were incomplete.
// Initial and its slices transfer by value; mutation is serial. Copies share the
// consuming guard. Finish transfers ownership before the shared finalizer runs.
type FragmentReport struct {
	resolver *FragmentResolver
	state    *filesystemReportState
}

func NewFragmentReport(resolver *FragmentResolver, initial result.Result) (*FragmentReport, error) {
	if resolver == nil || resolver.service == nil {
		return nil, ErrContract
	}
	owner, err := NewFilesystemReport(resolver.service.operation, initial)
	if err != nil {
		return nil, err
	}
	return &FragmentReport{resolver, owner.state}, nil
}
func (owner *FragmentReport) Append(view View, resolutions []FragmentResolution) error {
	if owner == nil || owner.state == nil || owner.resolver == nil || owner.state.finished || view.operation != owner.resolver.service.operation || view.identity == nil {
		return ErrContract
	}
	seen := make(map[*fragmentQueryState]bool, len(resolutions))
	// Canonicalization and full validation precede any result mutation.
	problems := make([][4][]Problem, len(resolutions))
	for i, resolution := range resolutions {
		f := resolution.state
		if f == nil || f.query == nil || f.query.resolver.service != owner.resolver.service || !f.query.resolver.normalizer.view.Same(view) || !f.query.consumed.Load() || seen[f.query] {
			return ErrContract
		}
		seen[f.query] = true
		for j, failure := range f.failures[:f.failureCount] {
			var err error
			problems[i][j], err = serviceFailureProblems(failure.err)
			if err != nil {
				return err
			}
		}
		var err error
		problems[i][3], err = serviceFailureProblems(f.controls)
		if err != nil {
			return err
		}
	}
	for i, resolution := range resolutions {
		f := resolution.state
		q := f.query
		// Resolve owns this bounded stage identity. The original locator may
		// be a successful directory, not the document whose anchors stopped.
		key := f.finalCauseIdentity
		var ids []string
		add := func(identity string, p Problem) {
			if id := owner.state.problem(identity, p); id != "" && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		for _, observation := range f.observations[:f.count] {
			add(observation.request.Key().value, observation.problem)
		}
		add(key, f.anchors.Problem())
		for j, failure := range f.failures[:f.failureCount] {
			for _, problem := range problems[i][j] {
				add(failure.key, problem)
			}
		}
		for _, problem := range problems[i][3] {
			add(key, problem)
		}
		if f.outcome == FragmentContextUnavailable {
			location, snapshot := q.location, q.snapshot
			owner.state.out.Coverage = append(owner.state.out.Coverage, result.Coverage{Path: q.normalized.link.path, Rule: q.normalized.link.rule, Range: &location, Snapshot: &snapshot, Reason: result.ContextUnavailable, Message: "Required fragment context is unavailable.", CauseIDs: ids})
		}
	}
	return nil
}
func (owner *FragmentReport) Finish() (result.Result, error) {
	if owner == nil || owner.state == nil {
		return result.Result{}, ErrContract
	}
	return (&FilesystemReport{state: owner.state}).Finish()
}
