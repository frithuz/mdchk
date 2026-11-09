package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules/internal/headingblanks"
)

func headingBlankLines(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	headings, err := c.HeadingSpacing(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	facts, err := c.Headings(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	before, after := headingblanks.Minimums(c.Options())
	sides, err := headingblanks.Sides(ctx, c.Source(), c.Lines(), facts, headings, before, after, c.Checkpoint)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	edits := c.InteriorEdits()
	out := engine.RuleOutcome{}
	for i, h := range headings {
		for s, side := range [...]struct {
			point          parser.Span
			count, minimum int
			edge           bool
			message        string
		}{
			{h.Before, h.BeforeBlank, before, h.BeforeEdge, "Heading requires more blank body lines before it."},
			{h.After, h.AfterBlank, after, h.AfterEdge, "Heading requires more blank body lines after it."},
		} {
			// Reserve side construction and any amortized finding-prefix copy.
			work := int64(1)
			if len(out.Findings) == cap(out.Findings) {
				work += int64(len(out.Findings))
			}
			if err := c.Checkpoint(ctx, work); err != nil {
				out.Err = err
				return out
			}
			if !side.edge && side.count < side.minimum {
				finding := engine.Finding{Span: side.point, Message: side.message}
				// The neighbour line touches the insertion, so its protection defers it.
				if plan := sides[i][s]; plan.Known {
					finding.FixEligible, err = edits.InsertionEligible(ctx, "MD022", side.point, plan.Neighbour)
					if err != nil {
						out.Err = err
						return out
					}
				}
				out.Findings = append(out.Findings, finding)
			}
		}
	}
	return out
}
