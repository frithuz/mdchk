package rules

import (
	"context"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func thematicHeadingsApply(ctx context.Context, c engine.RuleContext) (bool, error) {
	allow, err := c.BooleanOption(ctx, "allow_slides")
	if err != nil {
		return false, err
	}
	slides := c.Context().Slides
	if !slides.Known {
		return false, parser.ErrModel
	}
	return !(allow && slides.Slides), nil
}

func thematicBreaksBeforeHeadings(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	spans, err := c.ThematicBreaksBeforeHeadings(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	out := engine.RuleOutcome{}
	for _, span := range spans {
		work := int64(1)
		if len(out.Findings) == cap(out.Findings) {
			work += int64(len(out.Findings))
		}
		if err := c.SectionCheckpoint(ctx, work, 1); err != nil {
			out.Err = err
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: span, Message: "Thematic break must not immediately precede a heading."})
	}
	out.Err = c.SectionCheckpoint(ctx, 0, 0)
	return out
}
