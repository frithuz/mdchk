package rules

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func numberedListPrefixes(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	one, err := c.OrderedListStyle(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	items, err := c.ListItems(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return compareNumberedPrefixes(ctx, c, items, one)
}

// Facts and admitted style are borrowed only for this invocation. Each group's
// first direct numeric start remains immutable across preorder child interleaves.
func compareNumberedPrefixes(ctx context.Context, c engine.RuleContext, items []parser.ListItemFact, one bool) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	// Reserve all possible associations before construction, including clean input.
	if err := c.SectionCheckpoint(ctx, int64(len(items))+1, int64(len(items))); err != nil {
		out.Err = err
		return out
	}
	type groupKey struct {
		source     parser.SourceToken
		start, end int
		present    bool
	}
	type sequence struct{ first, count int }
	groups := make(map[groupKey]sequence, len(items))
	for _, item := range items {
		if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
			out.Err = err
			return out
		}
		if !item.Ordered {
			continue
		}
		if !item.Group.Present() || !item.Start.Present || item.Start.Value < 0 || item.Start.Value > 999999999 {
			out.Err = parser.ErrModel
			return out
		}
		// Constant-size identity only: a Span also retains the entire source content.
		group := groupKey{item.Group.SourceToken(), item.Group.Start(), item.Group.End(), item.Group.Present()}
		state, exists := groups[group]
		if !exists {
			state.first = item.Start.Value
		}
		actual := item.Start.Value
		matches := actual-state.first == state.count
		if one {
			matches = actual == 1
		}
		// count cannot exceed the finite inventory length: increment once per item.
		state.count++
		groups[group] = state
		if matches {
			continue
		}
		if len(out.Findings) == cap(out.Findings) {
			capacity := min(len(items), max(1, len(out.Findings)*2))
			if err := c.SectionCheckpoint(ctx, int64(len(out.Findings))+1, int64(capacity)); err != nil {
				out.Err = err
				return out
			}
			next := make([]engine.Finding, len(out.Findings), capacity)
			copy(next, out.Findings)
			out.Findings = next
		}
		if err := c.SectionCheckpoint(ctx, 1, 1); err != nil {
			out.Err = err
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: item.Marker, Message: "Numbered list prefixes must follow the configured numeric sequence."})
	}
	return out
}
