package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
)

func multipleDocumentTitles(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	out := engine.RuleOutcome{}
	seen := false
	if c.Options()["frontmatter_title"] == true {
		title, err := c.FrontmatterTitle(ctx)
		if err != nil {
			return engine.RuleOutcome{Err: err}
		}
		if !title.Known {
			out.IncompleteReason = "Frontmatter title is unavailable; the first body title's metadata-dependent comparison is withheld."
		} else {
			seen = title.Present
		}
	}
	headings, err := c.Headings(ctx)
	if err != nil {
		out.Err = err
		return out
	}
	level := c.Options()["level"].(int)
	for _, h := range headings {
		if err := c.HeadingComparisonCheckpoint(ctx, 2, 0); err != nil {
			out.Err = err
			return out
		}
		if h.Level != level {
			continue
		}
		if !seen {
			seen = true
			continue
		}
		// Reserve the new finding and any copying caused by slice growth before
		// retaining it. Hidden headings still establish the document-wide baseline.
		work := int64(1)
		if len(out.Findings) == cap(out.Findings) {
			work += int64(len(out.Findings))
		}
		if err := c.HeadingComparisonCheckpoint(ctx, work, 1); err != nil {
			out.Err = err
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: h.Label, Message: "Document must not contain multiple titles."})
	}
	return out
}
