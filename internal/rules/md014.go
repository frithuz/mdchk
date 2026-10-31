package rules

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func shellPrompts(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	// This whole-callback gate is distinct from producer, facade and comparison.
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
			out.IncompleteReason = ""
		}
	}()
	items, err := c.ShellPrompts(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	switch items.Availability {
	case parser.Unavailable:
		return engine.RuleOutcome{IncompleteReason: "Shell-prompt context is unavailable; the complete MD014 inventory is withheld."}
	case parser.Available:
		return compareShellPrompts(ctx, c, items.Dollars)
	default:
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
}

func compareShellPrompts(ctx context.Context, c engine.RuleContext, dollars []parser.Span) (out engine.RuleOutcome) {
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
		if i == len(dollars) {
			break
		}
		s := dollars[i]
		if out.Err = c.SectionCheckpoint(ctx, 16, 0); out.Err != nil {
			return out
		}
		if !s.Present() || s.SourceToken() != c.Source().Token() || s.Start() < previous || s.End() <= s.Start() || s.End()-s.Start() != 1 || s.End() > c.Source().Len() {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		if out.Err = c.SectionCheckpoint(ctx, 2, 0); out.Err != nil {
			return out
		}
		text, err := c.Source().Text(s)
		if err != nil || text != "$" {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		previous = s.End()
		if len(out.Findings) == cap(out.Findings) {
			capacity := len(dollars)
			if cap(out.Findings) <= len(dollars)/2 {
				capacity = max(1, cap(out.Findings)*2)
			}
			if out.Err = c.SectionCheckpoint(ctx, int64(len(out.Findings))+int64(capacity)+1, int64(capacity)); out.Err != nil {
				return out
			}
			next := make([]engine.Finding, len(out.Findings), capacity)
			copy(next, out.Findings)
			out.Findings = next
		}
		if out.Err = c.SectionCheckpoint(ctx, 1, 1); out.Err != nil {
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: s, Message: "Shell command has a dollar prompt without output."})
	}
	return out
}
