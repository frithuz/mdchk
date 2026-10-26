package rules

import (
	"context"
	"github.com/frithuz/mdchk/internal/engine"
)

func emphasisUsedAsHeading(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	out := engine.RuleOutcome{}
	labels, err := c.EmphasisHeadings(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	for _, label := range labels {
		work := int64(1)
		if len(out.Findings) == cap(out.Findings) {
			work += int64(len(out.Findings))
		}
		if err := c.HeadingComparisonCheckpoint(ctx, work, 1); err != nil {
			out.Err = err
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: label, Message: "Use a heading instead of an emphasized paragraph before body text."})
	}
	return out
}
