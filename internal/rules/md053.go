package rules

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func unneededDefinitions(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
			out.IncompleteReason = ""
		}
	}()
	items, err := c.UnneededDefinitions(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return unneededDefinitionOutcome(ctx, c, items)
}

// Shadowed findings need only the definitions; unused findings also need
// every reference use, so an unknown use set withholds only those.
func unneededDefinitionOutcome(ctx context.Context, c engine.RuleContext, items parser.UnneededDefinitionInventory) engine.RuleOutcome {
	switch {
	case items.Shadowed == parser.Unavailable:
		return engine.RuleOutcome{IncompleteReason: "Reference-definition context is unavailable; the complete MD053 inventory is withheld."}
	case items.Shadowed != parser.Available:
		return engine.RuleOutcome{Err: parser.ErrModel}
	case items.Unused == parser.Available:
		return compareUnneededDefinitions(ctx, c, items.Definitions, true)
	case items.Unused == parser.Unavailable:
		out := compareUnneededDefinitions(ctx, c, items.Definitions, false)
		if out.Err == nil {
			out.IncompleteReason = "Reference-use context is unavailable; MD053 unused-definition findings are withheld."
		}
		return out
	default:
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
}

func compareUnneededDefinitions(ctx context.Context, c engine.RuleContext, items []parser.UnneededDefinition, unused bool) (out engine.RuleOutcome) {
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
		item := items[i]
		s := item.Span
		if out.Err = c.SectionCheckpoint(ctx, 18+int64(len(item.Label)), 0); out.Err != nil {
			return out
		}
		if !s.Present() || s.SourceToken() != c.Source().Token() || s.Start() < previous || s.End() <= s.Start() || s.End() > c.Source().Len() || item.Label == "" || !item.Shadowed && !unused {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		message := "Definition \"" + item.Label + "\" is not used by any reference."
		if item.Shadowed {
			message = "Definition \"" + item.Label + "\" is shadowed by an earlier definition of the same label."
		}
		for _, point := range [2]int{s.Start(), s.End()} {
			if out.Err = c.SectionCheckpoint(ctx, 9, 0); out.Err != nil {
				return out
			}
			window, err := c.Source().Span(max(0, point-1), min(c.Source().Len(), point+1))
			if err != nil {
				return engine.RuleOutcome{Err: parser.ErrModel}
			}
			text, err := c.Source().Text(window)
			at := point - window.Start()
			if err != nil || point < c.Source().Len() && !utf8.RuneStart(text[at]) || point > 0 && point < c.Source().Len() && text[at-1] == '\r' && text[at] == '\n' {
				return engine.RuleOutcome{Err: parser.ErrModel}
			}
		}
		previous = s.End()
		if len(out.Findings) == cap(out.Findings) {
			capacity := len(items)
			if cap(out.Findings) <= len(items)/2 {
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
		out.Findings = append(out.Findings, engine.Finding{Span: s, Message: message})
	}
	return out
}
