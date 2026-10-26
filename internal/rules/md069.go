package rules

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func repeatedListMarkers(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, err := c.ListRepetitions(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return compareRepeatedListMarkers(ctx, c, items)
}

func compareRepeatedListMarkers(ctx context.Context, c engine.RuleContext, items []parser.Span) (out engine.RuleOutcome) {
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
		out.Findings = append(out.Findings, engine.Finding{Span: item, Message: "Repeated identical list markers."})
	}
	return out
}
