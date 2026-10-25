package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
)

func headingIncrements(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	out := engine.RuleOutcome{}
	previous := 0
	if c.Options()["frontmatter_title"] == true {
		title, err := c.FrontmatterTitle(ctx)
		if err != nil {
			return engine.RuleOutcome{Err: err}
		}
		if !title.Known {
			out.IncompleteReason = "Frontmatter title is unavailable; the first heading's comparison with a virtual H1 is withheld."
		} else if title.Present {
			previous = 1
		}
	}
	headings, err := c.Headings(ctx)
	if err != nil {
		out.Err = err
		return out
	}
	for _, heading := range headings {
		if err := c.Checkpoint(ctx, 1); err != nil {
			out.Err = err
			return out
		}
		if previous != 0 && heading.Level > previous+1 {
			out.Findings = append(out.Findings, engine.Finding{Span: heading.Envelope, Message: "Heading level must not increase by more than one."})
		}
		// Actual headings remain context even after violations or suppression.
		previous = heading.Level
	}
	return out
}
