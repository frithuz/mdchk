package rules

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func siblingListAlignment(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, err := c.ListItems(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return compareListAlignment(ctx, c, items)
}

// The detached inventory is borrowed for this invocation only. Full source-owned
// group identities retain the first direct sibling baseline across nested groups.
func compareListAlignment(ctx context.Context, c engine.RuleContext, items []parser.ListItemFact) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	// Reserve the maximum number of baseline records before constructing the map,
	// including aligned groups that never produce a finding. Association is linear
	// in inventory size, rather than repeatedly searching remaining siblings.
	if err := c.SectionCheckpoint(ctx, int64(len(items))+1, int64(len(items))); err != nil {
		out.Err = err
		return out
	}
	type groupKey struct {
		source     parser.SourceToken
		start, end int
		present    bool
	}
	baselines := make(map[groupKey]int, len(items))
	for _, item := range items {
		if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
			out.Err = err
			return out
		}
		indent := item.ContentIndent - item.MarkerWidth - item.Padding
		// Hash only constant-size identity and offsets, never Source content.
		group := groupKey{item.Group.SourceToken(), item.Group.Start(), item.Group.End(), item.Group.Present()}
		expected, exists := baselines[group]
		if !exists {
			baselines[group] = indent
			continue
		}
		if indent == expected {
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
		out.Findings = append(out.Findings, engine.Finding{Span: item.Marker, Message: "Sibling list item markers must align with the first item."})
	}
	return out
}
