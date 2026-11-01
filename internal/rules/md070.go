package rules

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func nestedMarkdownExamples(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
			out.IncompleteReason = ""
		}
	}()
	items, err := c.NestedExamples(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	switch items.Availability {
	case parser.Available:
		return compareNestedMarkdownExamples(ctx, c, items.Facts)
	case parser.Unavailable:
		return engine.RuleOutcome{IncompleteReason: "Nested-example context is unavailable; the complete MD070 inventory is withheld."}
	default:
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
}

func compareNestedMarkdownExamples(ctx context.Context, c engine.RuleContext, items []parser.NestedExampleFact) (out engine.RuleOutcome) {
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
		if out.Err = c.SectionCheckpoint(ctx, 4*int64(len(item.Kind))+18, 0); out.Err != nil {
			return out
		}
		if !s.Present() || s.SourceToken() != c.Source().Token() || s.Start() < previous || s.End() < s.Start() || s.End() > c.Source().Len() {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		message := ""
		switch item.Kind {
		case "length":
			message = "Outer Markdown fence must be longer than every matching inner fence."
		case "opening":
			message = "Expected an outer Markdown opening fence."
		case "language":
			message = "Outer example fence must use md or markdown language."
		case "closing":
			message = "Expected a matching outer closing fence."
		default:
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		if item.Kind == "length" && s.Start() == s.End() {
			return engine.RuleOutcome{Err: parser.ErrModel}
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
