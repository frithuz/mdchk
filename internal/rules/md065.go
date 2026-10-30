package rules

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func horizontalRuleBlankLines(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	items, err := c.ThematicSpacing(ctx)
	if err != nil {
		out.Err = err
		return out
	}
	return compareHorizontalRuleBlankLines(ctx, c, items)
}

func compareHorizontalRuleBlankLines(ctx context.Context, c engine.RuleContext, items []parser.ThematicSpacingFact) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
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
		message := "Horizontal rule requires a blank body line after it."
		if item.Before {
			message = "Horizontal rule requires a blank body line before it."
		}
		out.Findings = append(out.Findings, engine.Finding{Span: item.Point, Message: message})
	}
	return out
}
