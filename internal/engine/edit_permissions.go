package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// CorrectionSupport verifies catalog identity against the registry that resolved
// this authority's settings. Planned support permits structural experiments; it
// does not establish a producer or positive fix availability.
func (r DirectiveResult) CorrectionSupport(registry *Registry, id string) (FixSupport, bool) {
	if registry == nil || !r.complete || r.catalogs[id] != registry.identity {
		return NoFix, false
	}
	m, ok := registry.metadata[id]
	return m.Fix, ok
}

// InOperation identifies the exact shared residual budget used for recognition.
func (r DirectiveResult) InOperation(budget *parser.OperationBudget) bool {
	return budget != nil && r.operation == budget.Token()
}

// RangePermission explains original-source directive permission, independently of
// implementation availability. Owner and Token retain the responsible instruction.
type RangePermission struct {
	Allowed bool
	Reason  string
	Owner   int
	Token   parser.Span
}

// AllowsEdit tests the diagnostic start, then every replaced byte (or the exact
// insertion position). No caller-owned settings or serialized views authorize it.
func (r DirectiveResult) AllowsEdit(ctx context.Context, budget *parser.OperationBudget, id string, anchor, span parser.Span) (RangePermission, error) {
	no := RangePermission{Reason: "directive-state-unresolved", Owner: -1}
	if ctx == nil || budget == nil {
		return no, errors.New("nil permission context or budget")
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return budget.ChargeWork(1)
	}
	if err := check(); err != nil {
		return no, err
	}
	if !r.complete || !r.InOperation(budget) {
		return no, nil
	}
	if _, err := r.source.Text(anchor); err != nil {
		return no, nil
	}
	if _, err := r.source.Text(span); err != nil {
		return no, nil
	}
	intervals, ok := r.intervals[id]
	if !ok {
		return no, nil
	}
	at := func(offset int) (DirectiveInterval, error) {
		// Binary search selects the final equal-start transition, including EOF.
		lo, hi := 0, len(intervals)
		for lo < hi {
			if err := check(); err != nil {
				return DirectiveInterval{}, err
			}
			mid := lo + (hi-lo)/2
			if intervals[mid].Start <= offset {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo == 0 {
			return DirectiveInterval{}, errors.New("missing directive interval")
		}
		return intervals[lo-1], nil
	}
	a, err := at(anchor.Start())
	if err != nil {
		return no, err
	}
	if !a.Enabled {
		return RangePermission{false, "disabled-anchor", a.Owner, a.Token}, nil
	}
	if span.Start() == span.End() {
		v, err := at(span.Start())
		if err != nil {
			return no, err
		}
		if !v.Enabled {
			return RangePermission{false, "disabled-insertion", v.Owner, v.Token}, nil
		}
	} else {
		// Begin near the replaced range rather than rescanning preceding intervals.
		lo, hi := 0, len(intervals)
		for lo < hi {
			if err := check(); err != nil {
				return no, err
			}
			mid := lo + (hi-lo)/2
			if intervals[mid].End <= span.Start() {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		for i := lo; i < len(intervals) && intervals[i].Start < span.End(); i++ {
			if err := check(); err != nil {
				return no, err
			}
			v := intervals[i]
			if !v.Enabled && v.Start < v.End {
				return RangePermission{false, "disabled-span", v.Owner, v.Token}, nil
			}
		}
	}
	return RangePermission{Allowed: true, Owner: -1}, nil
}

// CandidateSpans copies only authoritative comment envelopes, including invalid,
// blocked and inactive candidates; payload/options are unnecessary for protection.
func (r DirectiveResult) CandidateSpans(ctx context.Context, budget *parser.OperationBudget) ([]parser.Span, error) {
	if ctx == nil || budget == nil {
		return nil, errors.New("nil candidate context or budget")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := budget.ChargeWork(int64(len(r.candidates) + 1)); err != nil {
		return nil, err
	}
	if err := budget.ChargeNodes(int64(len(r.candidates))); err != nil {
		return nil, err
	}
	if !r.complete || !r.InOperation(budget) {
		return nil, errors.New("unresolved candidate operation")
	}
	out := make([]parser.Span, len(r.candidates))
	for i, c := range r.candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out[i] = c.Span
	}
	return out, nil
}
