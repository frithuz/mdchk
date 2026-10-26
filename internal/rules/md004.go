package rules

import (
	"context"
	"github.com/frithuz/mdchk/internal/engine"
)

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
		}
	}
	out.Err = c.SectionCheckpoint(ctx, 0, 0)
	return out
}
