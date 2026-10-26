package rules

import (
	"context"
	"github.com/frithuz/mdchk/internal/engine"
)

func sectionsWithoutContent(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	sections, allowFinal, err := c.EmptySections(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	out := engine.RuleOutcome{}
	for _, section := range sections {
		if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
			out.Err = err
			return out
		}
		if section.Final && allowFinal {
			continue
		}
		work := int64(1)
		if len(out.Findings) == cap(out.Findings) {
			work += int64(len(out.Findings))
		}
		if err := c.SectionCheckpoint(ctx, work, 1); err != nil {
			out.Err = err
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: section.Label, Message: "Section must contain substantive content."})
	}
	out.Err = c.SectionCheckpoint(ctx, 0, 0)
	return out
}
