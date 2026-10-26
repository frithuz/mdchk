package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/resource"
)

func collidingHeadingAnchors(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	headings, availability, err := c.HeadingAnchors(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	if availability != resource.Available {
		return engine.RuleOutcome{IncompleteReason: "Heading anchor context is unavailable or uncertain."}
	}
	return compareHeadingAnchorBases(ctx, c, headings)
}

func compareHeadingAnchorBases(ctx context.Context, c engine.RuleContext, headings []resource.HeadingAnchor) engine.RuleOutcome {
	// The original provider's finite record/source limits bound these widened
	// linear reservations. String bytes are borrowed, not cloned node records.
	if err := c.HeadingAnchorCheckpoint(ctx, 16*int64(len(headings))+1, int64(len(headings))+1); err != nil {
		return engine.RuleOutcome{Err: err}
	}
	seen := make(map[string]struct{}, len(headings))
	out := engine.RuleOutcome{}
	for _, heading := range headings {
		if err := c.HeadingAnchorCheckpoint(ctx, 1, 0); err != nil {
			out.Err = err
			return out
		}
		if !heading.Base.Present {
			continue
		}
		base := heading.Base.Value
		if err := c.HeadingAnchorCheckpoint(ctx, 4*int64(len(base))+16, 0); err != nil {
			out.Err = err
			return out
		}
		_, duplicate := seen[base]
		if !duplicate {
			seen[base] = struct{}{}
			continue
		}
		work := int64(1)
		if len(out.Findings) == cap(out.Findings) {
			work += int64(len(out.Findings))
		}
		if err := c.HeadingAnchorCheckpoint(ctx, work, 1); err != nil {
			out.Err = err
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: heading.Label, Message: "Heading anchor base must not collide."})
	}
	out.Err = c.HeadingAnchorCheckpoint(ctx, 0, 0)
	return out
}
