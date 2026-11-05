package rules

import (
	"context"
	"errors"
	"slices"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func linkRepresentationStyle(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
			out.IncompleteReason = ""
		}
	}()
	items, err := c.LinkRepresentations(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return linkRepresentationOutcome(ctx, c, items)
}

func linkRepresentationOutcome(ctx context.Context, c engine.RuleContext, items parser.LinkRepresentationsInventory) engine.RuleOutcome {
	switch items.Availability {
	case parser.Available:
		allowed, ok := c.Options()["allowed_styles"].([]string)
		if !ok {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		return compareLinkRepresentations(ctx, c, items.Items, allowed)
	case parser.Unavailable:
		return engine.RuleOutcome{IncompleteReason: "Link representation context is unavailable; the complete MD054 inventory is withheld."}
	default:
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
}

func compareLinkRepresentations(ctx context.Context, c engine.RuleContext, items []parser.LinkRepresentation, allowed []string) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	// Nested Links and Images overlap, so only starts are ordered. Every item's
	// original range is validated, including allowed ones.
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
		if out.Err = c.SectionCheckpoint(ctx, 18+int64(len(allowed)), 0); out.Err != nil {
			return out
		}
		if !s.Present() || s.SourceToken() != c.Source().Token() || s.Start() < previous || s.End() <= s.Start() || s.End() > c.Source().Len() {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		previous = s.Start()
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
		switch item.Style {
		case "inline", "full_reference", "collapsed_reference", "shortcut_reference", "autolink":
		default:
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		if slices.Contains(allowed, item.Style) {
			continue
		}
		message := "Link representation style \"" + item.Style + "\" is not allowed."
		if item.Image {
			message = "Image representation style \"" + item.Style + "\" is not allowed."
		}
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
