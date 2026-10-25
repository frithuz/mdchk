package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
)

func headingStyle(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	headings, err := c.Headings(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	expected, _ := c.Options()["style"].(string)
	out := engine.RuleOutcome{}
	for _, heading := range headings {
		if err := c.Checkpoint(ctx, 1); err != nil {
			out.Err = err
			return out
		}
		actual := heading.Style
		if heading.Closed {
			actual = "atx_closed"
		}
		// The first actual heading fixes one document-wide baseline, including
		// headings whose observations the engine will subsequently suppress.
		if expected == "consistent" {
			expected = actual
		}
		if actual != expected {
			out.Findings = append(out.Findings, engine.Finding{Span: heading.Envelope, Message: "Heading syntax must be " + expected + "."})
		}
	}
	return out
}
