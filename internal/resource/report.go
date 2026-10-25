package resource

import (
	"fmt"
	"slices"

	"github.com/frithuz/mdchk/internal/result"
)

// AppendEffects projects unavailable work and all encountered operational causes
// independently of each request's satisfied capability, using the
// existing shared result API. It does not register diagnostics, targets, checked
// files, changes or writes. Call once for the complete view's request batch;
// the existing engine finalizer canonicalizes IDs/coverage and keeps precedence.
// Resource errors are deduplicated by resource identity and failure kind, while
// every affected source link retains its own coverage and explicit cause link.
func (v View) AppendEffects(out *result.Result, observations []Observation) error {
	return v.appendEffects(out, observations, nil)
}

func (v View) appendEffects(out *result.Result, observations []Observation, additional map[*requestIdentity]Problem) error {
	if v.operation == nil || v.identity == nil || out == nil {
		return ErrContract
	}
	seen := map[*requestIdentity]bool{}
	for _, o := range observations {
		if !o.request.view.Same(v) || o.request.identity == nil || seen[o.request.identity] {
			return ErrContract
		}
		seen[o.request.identity] = true
	}
	usedIDs := map[string]bool{}
	for _, e := range out.Errors {
		usedIDs[e.ID] = true
	}
	for _, stop := range out.Interruptions {
		usedIDs[stop.ID] = true
	}
	nextOrdinals := map[string]int{}
	nextID := func(prefix string) string {
		for n := nextOrdinals[prefix] + 1; ; n++ {
			id := fmt.Sprintf("%s-%d", prefix, n)
			if !usedIDs[id] {
				usedIDs[id] = true
				nextOrdinals[prefix] = n
				return id
			}
		}
	}
	errorsByIdentity := map[string]string{}
	stops := map[string]string{}
	project := func(key string, problem Problem) string {
		identity := key + "\x00" + string(problem.Kind)
		switch problem.Kind {
		case ReadFailure, DecodeFailure:
			id := errorsByIdentity[identity]
			if id == "" {
				id = nextID("resource-error")
				errorsByIdentity[identity] = id
				out.Errors = append(out.Errors, result.Error{ID: id, Category: result.IO, Scope: "operation", Code: string(problem.Kind), Message: problem.Message, Path: key})
			}
			return id
		case Cancelled, Deadline, Limited:
			id := stops[identity]
			if id == "" {
				id = nextID("resource-stop")
				stops[identity] = id
				p := copyProblem(problem)
				out.Interruptions = append(out.Interruptions, result.Interruption{ID: id, Reason: string(problem.Kind), Phase: "acquisition", Message: p.Message, Limit: p.Limit})
			}
			return id
		default:
			return ""
		}
	}
	for _, o := range observations {
		var causeIDs []string
		for _, problem := range []Problem{o.problem, additional[o.request.identity]} {
			if id := project(o.request.Key().String(), problem); id != "" && !slices.Contains(causeIDs, id) {
				causeIDs = append(causeIDs, id)
			}
		}
		if o.Treatment() == ContextIncomplete {
			origin := o.request.Link()
			location, snapshot := o.request.location, o.request.snapshot
			coverage := result.Coverage{Path: origin.path, Rule: origin.rule, Range: &location, Snapshot: &snapshot, Reason: result.ContextUnavailable, Message: "Required resource context is unavailable.", CauseIDs: causeIDs}
			out.Coverage = append(out.Coverage, coverage)
		}
	}
	return nil
}

// Observations returns a defensive copy suitable for an immutable view owner.
// Service caches raw acquisition facts; request-bound observations are not cache entries.
func Observations(values ...Observation) []Observation { return slices.Clone(values) }
