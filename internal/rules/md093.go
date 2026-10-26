package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
)

func inlineFormattingInHeadings(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	facts, err := c.DisallowedHeadingConstructs(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	out := engine.RuleOutcome{}
	for _, fact := range facts {
		work := int64(1)
		if len(out.Findings) == cap(out.Findings) {
			work += int64(len(out.Findings))
		}
		if err := c.SectionCheckpoint(ctx, work, 1); err != nil {
			out.Err = err
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: fact.Span, Message: "Inline formatting is not allowed in headings."})
	}
	out.Err = c.SectionCheckpoint(ctx, 0, 0)
	return out
}
