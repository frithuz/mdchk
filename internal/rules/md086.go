package rules

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func unclosedHTMLComments(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	// This is a distinct outer publication gate after the facade AND comparison.
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
			out.IncompleteReason = ""
		}
	}()
	items, err := c.UnclosedHTMLComments(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	if items.Availability != parser.Available {
		return engine.RuleOutcome{IncompleteReason: "HTML comment context is unavailable; the complete MD086 inventory is withheld."}
	}
	return compareUnclosedHTMLComments(ctx, c, items.Comments)
}

func compareUnclosedHTMLComments(ctx context.Context, c engine.RuleContext, items []parser.UnclosedHTMLComment) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	previous := 0
	for i := 0; ; i++ {
		if out.Err = c.SectionCheckpoint(ctx, 1, 0); out.Err != nil {
			return out
		}
		if i == len(items) {
			break
		}
		s := items[i].Open
		if out.Err = c.SectionCheckpoint(ctx, 32, 0); out.Err != nil {
			return out
		}
		if !s.Present() || s.SourceToken() != c.Source().Token() || s.Start() < previous || s.End() > c.Source().Len() || s.End()-s.Start() != 4 {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		text, err := c.Source().Text(s)
		if err != nil || text != "<!--" {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		previous = s.End()
		if len(out.Findings) == cap(out.Findings) {
			capacity := min(len(items), max(1, cap(out.Findings)*2))
			if out.Err = c.SectionCheckpoint(ctx, int64(len(out.Findings)+capacity)+1, int64(capacity)); out.Err != nil {
				return out
			}
			next := make([]engine.Finding, len(out.Findings), capacity)
			copy(next, out.Findings)
			out.Findings = next
		}
		if out.Err = c.SectionCheckpoint(ctx, 1, 1); out.Err != nil {
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: s, Message: "HTML comment is missing a closing delimiter."})
	}
	return out
}
