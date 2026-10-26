package rules

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func bulletNestingIndentation(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	indent, start, err := c.BulletIndentation(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	items, err := c.ListItems(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return compareBulletIndentation(ctx, c, items, indent, start)
}

// Compare each actual item's local structural indentation to its actual parent.
// Ordered items remain anchors across groups and quotes, but are never targets.
func compareBulletIndentation(ctx context.Context, c engine.RuleContext, items []parser.ListItemFact, indent int, start bool) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	// Reserve all associations before constructing state, including clean inputs.
	if err := c.SectionCheckpoint(ctx, int64(len(items))+1, int64(len(items))); err != nil {
		out.Err = err
		return out
	}
	type itemKey struct {
		source     parser.SourceToken
		start, end int
		present    bool
	}
	key := func(s parser.Span) itemKey { return itemKey{s.SourceToken(), s.Start(), s.End(), s.Present()} }
	parents := make(map[itemKey]parser.ListItemFact, len(items))
	for _, item := range items {
		if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
			out.Err = err
			return out
		}
		// Never hash source-backed spans or search raw source for missing ancestry.
		var parent parser.ListItemFact
		if item.ParentItem.Present() {
			var exists bool
			parent, exists = parents[key(item.ParentItem)]
			if !exists {
				out.Err = parser.ErrModel
				return out
			}
		}
		identity := key(item.Item)
		if !identity.present {
			out.Err = parser.ErrModel
			return out
		}
		if _, exists := parents[identity]; exists {
			out.Err = parser.ErrModel
			return out
		}
		parents[identity] = item
		if item.Ordered {
			continue
		}
		actual := item.ContentIndent - item.MarkerWidth - item.Padding
		expected := 0
		if item.ParentItem.Present() {
			if !parent.Ordered {
				actual += parent.MarkerWidth + parent.Padding
				expected = indent
			}
		} else if start {
			expected = indent
		}
		// No arithmetic depends on configured indent, even at the largest int.
		if actual == expected {
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
		out.Findings = append(out.Findings, engine.Finding{Span: item.Marker, Message: "Bullet list item indentation must match its configured parent offset."})
	}
	return out
}
