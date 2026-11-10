// Package md004 checks and corrects MD004, which requires one bullet marker style for unordered lists.
package md004

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// Registration returns the MD004 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD004", Check: bulletMarkerStyle}
}

func bulletMarkerStyle(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, err := c.ListItems(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	expected, err := c.BulletMarker(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	out := engine.RuleOutcome{}
	var flagged []parser.ListItemFact
	for _, item := range items {
		if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
			out.Err = err
			return out
		}
		if item.Ordered {
			continue
		}
		actual := item.Bullet.Value
		// One baseline spans all groups, nesting and suppressed original context.
		if expected == 0 {
			expected = actual
		}
		if actual != expected {
			work := int64(1)
			if len(out.Findings) == cap(out.Findings) {
				work += int64(len(out.Findings))
			}
			if err := c.SectionCheckpoint(ctx, work, 1); err != nil {
				out.Err = err
				return out
			}
			out.Findings = append(out.Findings, engine.Finding{Span: item.Marker, Message: "Bullet marker must be " + string(expected) + "."})
			flagged = append(flagged, item)
		}
	}
	if len(flagged) > 0 {
		out.Err = bulletFixEligibility(ctx, c, items, flagged, expected, out.Findings)
		if out.Err != nil {
			return out
		}
	}
	out.Err = c.SectionCheckpoint(ctx, 0, 0)
	return out
}

// bulletFixEligibility marks the findings of a list fixable only when every one
// of its items is eligible and the list is structurally safe to rewrite. A list
// is corrected whole or not at all, so a correction never splits it.
func bulletFixEligibility(ctx context.Context, c engine.RuleContext, items, flagged []parser.ListItemFact, expected byte, findings []engine.Finding) error {
	lists, err := c.BulletLists(ctx)
	if err != nil {
		return err
	}
	safe, err := safeBulletLists(ctx, c.Source(), items, lists, expected, c.Checkpoint)
	if err != nil {
		return err
	}
	edits := c.InteriorEdits()
	eligible := make([]bool, len(flagged))
	for i, item := range flagged {
		if !safe[item.Group.Start()] {
			continue
		}
		eligible[i], err = edits.Eligible(ctx, "MD004", item.Marker, item.Marker)
		if err != nil {
			return err
		}
		if !eligible[i] {
			safe[item.Group.Start()] = false
		}
	}
	for i, item := range flagged {
		findings[i].FixEligible = eligible[i] && safe[item.Group.Start()]
	}
	return c.SectionCheckpoint(ctx, int64(len(flagged)), 0)
}
