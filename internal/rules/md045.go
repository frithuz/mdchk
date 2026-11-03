package rules

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func imageAlternatives(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out = engine.RuleOutcome{Err: out.Err}
		}
	}()
	inventory, err := c.ImageAlternatives(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	if inventory.Availability != parser.Available && inventory.Availability != parser.Unavailable {
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
	for _, span := range inventory.Spans {
		if out.Err = c.SectionCheckpoint(ctx, 2, 1); out.Err != nil {
			return out
		}
		out.Findings, out.Err = imageAlternativeAppend(ctx, c, out.Findings, engine.Finding{Span: span, Message: "Image alternative text is missing or blank."})
		if out.Err != nil {
			return out
		}
	}
	for _, span := range inventory.Uncertainty {
		if out.Err = c.SectionCheckpoint(ctx, 2, 1); out.Err != nil {
			return out
		}
		out.ParsingProblems, out.Err = imageAlternativeAppend(ctx, c, out.ParsingProblems, engine.Finding{Span: span, Message: "Image alternative text or decorative attributes cannot be interpreted reliably."})
		if out.Err != nil {
			return out
		}
	}
	if inventory.Availability == parser.Unavailable && len(inventory.Uncertainty) == 0 {
		out.IncompleteReason = "Image alternative context is unavailable."
	}
	return out
}

func imageAlternativeAppend(ctx context.Context, c engine.RuleContext, items []engine.Finding, f engine.Finding) ([]engine.Finding, error) {
	if len(items) == cap(items) {
		if cap(items) > int(^uint(0)>>1)/2 {
			return nil, parser.ErrModel
		}
		capacity := max(1, cap(items)*2)
		if err := c.SectionCheckpoint(ctx, int64(len(items))+int64(capacity)+1, int64(capacity)); err != nil {
			return nil, err
		}
		next := make([]engine.Finding, len(items), capacity)
		copy(next, items)
		items = next
	}
	return append(items, f), nil
}
