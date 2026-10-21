package result

import (
	"cmp"
	"slices"
)

// The coverage reason vocabulary is open; these are the core analysis reasons.
const (
	RuleUnavailable    = "rule-unavailable"
	ParserUncertain    = "parser-uncertain"
	ContextUnavailable = "context-unavailable"
)

// CanonicalizeCoverage orders and deduplicates coverage by its semantic fields.
// CauseIDs are a set of references to already-assigned canonical cause records.
// Generated IDs do not distinguish observations and are cleared; the producer
// assigns new IDs and file references after this call. Ranges and snapshots are
// read-only; the slice and cause lists are copied before mutation.
func (r *Result) CanonicalizeCoverage() {
	r.Coverage = slices.Clone(r.Coverage)
	for i := range r.Coverage {
		c := &r.Coverage[i]
		c.ID = ""
		c.CauseIDs = slices.Clone(c.CauseIDs)
		slices.Sort(c.CauseIDs)
		c.CauseIDs = slices.Compact(c.CauseIDs)
	}
	slices.SortFunc(r.Coverage, compareCoverage)
	r.Coverage = slices.CompactFunc(r.Coverage, func(a, b Coverage) bool {
		return compareCoverage(a, b) == 0
	})
}

func compareCoverage(a, b Coverage) int {
	// A range's start is its source position. Range ends are tie-breaks only
	// after the required path/position/rule/reason keys.
	if c := cmp.Compare(a.Path, b.Path); c != 0 {
		return c
	}
	if c := comparePosition(a.Range, b.Range); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Rule, b.Rule); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Reason, b.Reason); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Message, b.Message); c != 0 {
		return c
	}
	if a.Range != nil && b.Range != nil {
		if c := cmp.Compare(a.Range.End.Line, b.Range.End.Line); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Range.End.Column, b.Range.End.Column); c != 0 {
			return c
		}
	}
	if a.Snapshot == nil {
		if b.Snapshot != nil {
			return -1
		}
	} else {
		if b.Snapshot == nil {
			return 1
		}
		if c := cmp.Compare(a.Snapshot.View, b.Snapshot.View); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Snapshot.SHA256, b.Snapshot.SHA256); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Snapshot.SizeBytes, b.Snapshot.SizeBytes); c != 0 {
			return c
		}
	}
	return slices.Compare(a.CauseIDs, b.CauseIDs)
}

func comparePosition(a, b *Range) int {
	if a == nil {
		if b == nil {
			return 0
		}
		return -1
	}
	if b == nil {
		return 1
	}
	if c := cmp.Compare(a.Start.Line, b.Start.Line); c != 0 {
		return c
	}
	return cmp.Compare(a.Start.Column, b.Start.Column)
}

// FinalizeLintFixOutcome applies shared analysis precedence for lint and fix.
// It changes only Status and Complete and preserves partial observations.
// Operational failures outrank deficits, then violations, then clean results.
// Completeness describes assessed/returned work, independently of success.
// Inspect and configuration validation retain their operation-specific state.
func (r *Result) FinalizeLintFixOutcome() {
	if r.Operation != "lint" && r.Operation != "fix" {
		return
	}
	r.Complete = len(r.Coverage) == 0 && len(r.Interruptions) == 0 && len(r.Omissions) == 0
	r.Status = "clean"
	if len(r.Diagnostics) > 0 {
		r.Status = "violations"
	}
	if !r.Complete {
		r.Status = "incomplete"
	}
	for _, e := range r.Errors {
		if e.Scope == "operation" {
			r.Status = "error"
			break
		}
	}
}
