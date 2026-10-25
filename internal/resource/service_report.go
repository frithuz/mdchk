package resource

import (
	"context"
	"errors"
	"slices"

	"github.com/frithuz/mdchk/internal/result"
)

// ServiceFailure retains the independent error returned with a Service.Read
// fact. Append complete view batches to one consuming ServiceReport owner.
type ServiceFailure struct {
	Request Request
	Err     error
}

// ServiceReport owns one logical result projection. Construct one owner for
// all original/candidate/saved batches, then Finish before shared finalization.
// Ownership/copy/consuming semantics match FilesystemReport: mutation is serial,
// initial and its slices are transferred by value, and aliases share the guard.
// It retains no caller Result pointer or cache-reader-local projection state.
type ServiceReport struct {
	service *Service
	state   *filesystemReportState
}

func NewServiceReport(service *Service, initial result.Result) (*ServiceReport, error) {
	if service == nil || service.operation == nil {
		return nil, ErrContract
	}
	owner, err := NewFilesystemReport(service.operation, initial)
	if err != nil {
		return nil, err
	}
	return &ServiceReport{service: service, state: owner.state}, nil
}

// Append validates the complete view batch before mutation. Only report-owned
// cause indexes persist across batches; existing arbitrary paths never grant
// resource-cause authority. Each coverage is bound to its own source request.
func (owner *ServiceReport) Append(view View, observations []Observation, failures []ServiceFailure) error {
	if owner == nil || owner.state == nil || owner.service == nil || owner.state.finished {
		return ErrContract
	}
	additional, err := owner.service.effectProblems(view, observations, failures)
	if err != nil {
		return err
	}
	p := owner.state
	for _, o := range observations {
		var ids []string
		for _, problem := range append([]Problem{o.problem}, additional[o.request.identity]...) {
			if id := p.problem(o.request.Key().String(), problem); id != "" && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		if o.Treatment() == ContextIncomplete {
			location, snapshot := o.request.location, o.request.snapshot
			p.out.Coverage = append(p.out.Coverage, result.Coverage{Path: o.request.Link().Path(), Rule: o.request.Link().Rule(), Range: &location, Snapshot: &snapshot, Reason: result.ContextUnavailable, Message: "Required resource context is unavailable.", CauseIDs: ids})
		}
	}
	return nil
}

// Finish consumes every alias before transferring the result to its caller.
func (owner *ServiceReport) Finish() (result.Result, error) {
	if owner == nil || owner.state == nil {
		return result.Result{}, ErrContract
	}
	return (&FilesystemReport{state: owner.state}).Finish()
}

func (s *Service) effectProblems(view View, observations []Observation, failures []ServiceFailure) (map[*requestIdentity][]Problem, error) {
	if s == nil || s.operation == nil || view.operation != s.operation || view.identity == nil {
		return nil, ErrContract
	}

	seen := make(map[*requestIdentity]bool, len(observations))
	for _, o := range observations {
		if !o.request.view.Same(view) || o.request.identity == nil || seen[o.request.identity] {
			return nil, ErrContract
		}
		seen[o.request.identity] = true
	}
	additional := make(map[*requestIdentity][]Problem, len(failures))
	for _, failure := range failures {
		if !failure.Request.view.Same(view) || !seen[failure.Request.identity] || failure.Err == nil {
			return nil, ErrContract
		}
		if _, duplicate := additional[failure.Request.identity]; duplicate {
			return nil, ErrContract
		}
		_, _, detached := snapshotServiceError(failure.Err)
		var returned *serviceReadError
		if !errors.As(detached, &returned) {
			if _, controlled := InterruptionProblem(detached); !controlled {
				return nil, ErrContract
			}
		}
		normalized := joinServiceErrors(detached)
		var err *serviceReadError
		if !errors.As(normalized, &err) {
			return nil, ErrContract
		}
		var problems []Problem
		for _, cause := range err.causes {
			problems = append(problems, Problem{Kind: cause.kind, Message: cause.message})
		}

		if err.stop != nil && !err.controls.present() {
			return nil, ErrContract
		}
		problems = append(problems, serviceControlProblems(err.controls)...)
		if len(problems) == 0 {
			return nil, ErrContract
		}
		additional[failure.Request.identity] = problems
	}
	return additional, nil
}

// Projection preserves the fixed set of independently observed reasons. Primary
// ObservationSpec selection does not discard other canonical report causes.
func serviceControlProblems(c serviceControls) []Problem {
	var out []Problem
	add := func(err error) { p, _ := InterruptionProblem(err); out = append(out, p) }
	if c.cancelled {
		add(context.Canceled)
	}
	if c.deadline {
		add(context.DeadlineExceeded)
	}
	if c.limited {
		limit := c.limit
		add(&limit)
	}
	return out
}
