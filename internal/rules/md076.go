package rules

import (
	"context"
	"errors"
	"fmt"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func consistentListItemSpacing(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, err := c.ListItemSpacing(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return compareListItemSpacing(ctx, c, items)
}

func compareListItemSpacing(ctx context.Context, c engine.RuleContext, items []parser.ListItemSpacingFact) (out engine.RuleOutcome) {
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
		message := fmt.Sprintf("List item separator has %d blank body lines; expected %d.", item.Actual, item.Expected)
		out.Findings = append(out.Findings, engine.Finding{Span: item.Span, Message: message})
	}
	return out
}
