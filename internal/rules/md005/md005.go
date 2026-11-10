// Package md005 checks and corrects MD005, which requires sibling list items to share one indentation.
package md005

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// Registration returns the MD005 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD005", Check: siblingListAlignment}
}

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
	type baseline struct {
		indent int
		spaces bool
	}
	baselines := make(map[groupKey]baseline, len(items))
	edits := c.InteriorEdits()
	checkpoint := func(ctx context.Context, work int64) error { return c.SectionCheckpoint(ctx, work, 0) }
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
			// A tab in the baseline's indentation leaves its siblings' columns ambiguous.
			lead, err := listItemIndentation(ctx, c.Source(), item, checkpoint)
			if err != nil {
				out.Err = err
				return out
			}
			baselines[group] = baseline{indent, lead.spaces}
			continue
		}
		if indent == expected.indent {
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
		finding := engine.Finding{Span: item.Marker, Message: "Sibling list item markers must align with the first item."}
		// Only a spaces-only item whose later lines need not move stays correctable.
		if expected.spaces {
			lead, err := listItemIndentation(ctx, c.Source(), item, checkpoint)
			if err != nil {
				out.Err = err
				return out
			}
			if lead.spaces && lead.single {
				finding.FixEligible, err = edits.Eligible(ctx, "MD005", item.Marker, lead.span)
				if err != nil {
					out.Err = err
					return out
				}
			}
		}
		out.Findings = append(out.Findings, finding)
	}
	return out
}
