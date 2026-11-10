// Package md007 checks and corrects MD007, which requires nested bullets to use the configured indentation.
package md007

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// Registration returns the MD007 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD007", Check: bulletNestingIndentation}
}

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
	var shifts bulletEligibility
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
		actual, expected := bulletOffsets(item, parent, indent, start)
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
		finding := engine.Finding{Span: item.Marker, Message: "Bullet list item indentation must match its configured parent offset."}
		finding.FixEligible, out.Err = shifts.eligible(ctx, c, item, expected-actual)
		if out.Err != nil {
			return out
		}
		out.Findings = append(out.Findings, finding)
	}
	return out
}

// bulletOffsets returns an unordered item's actual and expected local indentation.
// Analysis and correction share it, so the target column cannot drift.
func bulletOffsets(item, parent parser.ListItemFact, indent int, start bool) (actual, expected int) {
	actual = item.ContentIndent - item.MarkerWidth - item.Padding
	if item.ParentItem.Present() {
		if !parent.Ordered {
			actual += parent.MarkerWidth + parent.Padding
			expected = indent
		}
	} else if start {
		expected = indent
	}
	return actual, expected
}

// bulletEligibility answers MD007 fix eligibility for one rule run. The source
// text and the interior edit query are built once, on the first finding.
type bulletEligibility struct {
	text  string
	edits *engine.RuleInteriorEdits
}

// eligible reports whether the lines a shift would move are plainly
// space-indented list content that the interior edit query allows editing.
func (b *bulletEligibility) eligible(ctx context.Context, c engine.RuleContext, item parser.ListItemFact, delta int) (bool, error) {
	if b.edits == nil {
		source := c.Source()
		if err := c.Checkpoint(ctx, int64(source.Len())+1); err != nil {
			return false, err
		}
		all, err := source.Span(0, source.Len())
		if err != nil {
			return false, err
		}
		if b.text, err = source.Text(all); err != nil {
			return false, err
		}
		b.edits = c.InteriorEdits()
	}
	if err := c.Checkpoint(ctx, int64(item.Item.End()-item.Item.Start())+1); err != nil {
		return false, err
	}
	starts, ok := bulletShift(b.text, item, delta)
	if !ok || len(starts) == 0 {
		return false, nil
	}
	affected, err := c.Source().Span(starts[0], item.Item.End())
	if err != nil {
		return false, err
	}
	return b.edits.Eligible(ctx, "MD007", item.Marker, affected)
}
