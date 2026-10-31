package rules

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func fencedBlockBlankLines(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
			out.IncompleteReason = ""
		}
	}()
	items, err := c.FencedSpacing(ctx)
	if err != nil {
		out.Err = err
		return out
	}
	switch items.Availability {
	case parser.Available:
		return compareFencedBlockBlankLines(ctx, c, items.Facts)
	case parser.Unavailable:
		return engine.RuleOutcome{IncompleteReason: "Fenced-block spacing context is unavailable; the complete MD031 inventory is withheld."}
	default:
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
}

func compareFencedBlockBlankLines(ctx context.Context, c engine.RuleContext, items []parser.FencedSpacingFact) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	for i := 0; ; i++ {
		// Include the negative/terminal probe, including an empty inventory.
		if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
			out.Err = err
			return out
		}
		if i == len(items) {
			break
		}
		item := items[i]
		// Five ownership/bounds predicates, two boundary bytes and two source
		// associations are reserved before validating detached evidence.
		if err := c.SectionCheckpoint(ctx, 9, 0); err != nil {
			out.Err = err
			return out
		}
		if !item.Point.Present() || item.Point.SourceToken() != c.Source().Token() || item.Point.Start() != item.Point.End() || item.Point.Start() < 0 || item.Point.End() > c.Source().Len() {
			out.Err = parser.ErrModel
			return out
		}
		point := item.Point.Start()
		window, err := c.Source().Span(max(0, point-1), min(c.Source().Len(), point+1))
		if err != nil {
			out.Err = parser.ErrModel
			return out
		}
		text, err := c.Source().Text(window)
		at := point - window.Start()
		if err != nil || (point < c.Source().Len() && !utf8.RuneStart(text[at])) || (point > 0 && point < c.Source().Len() && text[at-1] == '\r' && text[at] == '\n') {
			out.Err = parser.ErrModel
			return out
		}
		if len(out.Findings) == cap(out.Findings) {
			capacity := len(items)
			if cap(out.Findings) <= len(items)/2 {
				capacity = max(1, cap(out.Findings)*2)
			}
			if err := c.SectionCheckpoint(ctx, int64(len(out.Findings))+int64(capacity)+1, int64(capacity)); err != nil {
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
		message := "Fenced code block requires a blank body line after it."
		if item.Before {
			message = "Fenced code block requires a blank body line before it."
		}
		out.Findings = append(out.Findings, engine.Finding{Span: item.Point, Message: message})
	}
	return out
}
