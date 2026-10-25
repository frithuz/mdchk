package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func headingBlankLines(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	headings, err := c.HeadingSpacing(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	options := c.Options()
	before, _ := options["before"].(int)
	after, _ := options["after"].(int)
	out := engine.RuleOutcome{}
	for _, h := range headings {
		for _, side := range [...]struct {
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
				out.Findings = append(out.Findings, engine.Finding{Span: side.point, Message: side.message})
			}
		}
	}
	return out
}
