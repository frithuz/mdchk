package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
)

func repeatedHeadingText(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	headings, err := c.Headings(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	siblings, _ := c.Options()["siblings_only"].(bool)
	type scopeKey struct {
		parent int
		text   string
	}
	// IDs are source-order indices plus one, reserving zero for the root.
	// The outline has at most six actual levels, even across containers.
	var stack [6]int
	depth := 0
	// Reserve capacity for the complete finite inventory before map creation.
	// No insertion can exceed this capacity, avoiding geometric growth and
	// rehashing prior variable-length keys during ordinary insertion.
	if err := c.HeadingComparisonCheckpoint(ctx, 16*int64(len(headings))+6, int64(len(headings))+6); err != nil {
		return engine.RuleOutcome{Err: err}
	}
	seen := make(map[scopeKey]struct{}, len(headings))
	out := engine.RuleOutcome{}
	for i, h := range headings {
		if err := c.HeadingComparisonCheckpoint(ctx, 8, 0); err != nil {
			out.Err = err
			return out
		}
		key, err := c.HeadingKey(ctx, h)
		if err != nil {
			out.Err = err
			return out
		}
		parent := 0
		if siblings {
			for depth > 0 && headings[stack[depth-1]-1].Level >= h.Level {
				depth--
			}
			if depth > 0 {
				parent = stack[depth-1]
			}
			stack[depth] = i + 1
			depth++
		}
		// Reserve both whole-key hashes/comparisons and retained key/map growth.
		// Charging a prospective entry on hits is conservative and finite.
		if err := c.HeadingComparisonCheckpoint(ctx, 4*int64(len(key))+16, int64(len(key))+1); err != nil {
			out.Err = err
			return out
		}
		k := scopeKey{parent, key}
		_, duplicate := seen[k]
		if !duplicate {
			seen[k] = struct{}{}
			continue
		}
		work := int64(1)
		if len(out.Findings) == cap(out.Findings) {
			work += int64(len(out.Findings))
		}
		if err := c.HeadingComparisonCheckpoint(ctx, work, 1); err != nil {
			out.Err = err
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: h.Label, Message: "Heading text must not repeat."})
	}
	return out
}
