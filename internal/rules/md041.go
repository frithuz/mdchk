package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
)

func openingDocumentHeading(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	opening, err := c.OpeningBlock(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	if opening.Level == c.Options()["level"].(int) {
		return engine.RuleOutcome{}
	}
	if c.Options()["frontmatter_title"] == true {
		title, err := c.FrontmatterTitle(ctx)
		if err != nil {
			return engine.RuleOutcome{Err: err}
		}
		if !title.Known {
			return engine.RuleOutcome{IncompleteReason: "Frontmatter title is unavailable; the metadata-dependent opening-heading conclusion is withheld."}
		}
		if title.Present {
			return engine.RuleOutcome{}
		}
	}
	if err := c.HeadingComparisonCheckpoint(ctx, 1, 1); err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return engine.RuleOutcome{Findings: []engine.Finding{{Span: opening.Point, Message: "Document must begin with a heading at the configured level."}}}
}
