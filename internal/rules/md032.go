package rules

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func listBlankLines(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, err := c.ListSpacing(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return compareListBlankLines(ctx, c, items)
}

func compareListBlankLines(ctx context.Context, c engine.RuleContext, items []parser.ListSpacingFact) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	for _, item := range items {
		if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
			out.Err = err
			return out
		}
		if len(out.Findings) == cap(out.Findings) {
			capacity := min(len(items), max(1, len(out.Findings)*2))
			if err := c.SectionCheckpoint(ctx, int64(len(out.Findings))+1, int64(capacity)); err != nil {
				out.Err = err
				return out
			}
			next := make([]engine.Finding, len(out.Findings), capacity)
			copy(next, out.Findings)
			out.Findings = next
		}
		if err := c.SectionCheckpoint(ctx, 1, 1); err != nil {
			out.Err = err
			return out
		}
		message := "List requires a blank body line after it."
		if item.Before {
			message = "List requires a blank body line before it."
		}
		out.Findings = append(out.Findings, engine.Finding{Span: item.Point, Message: message})
	}
	return out
}
