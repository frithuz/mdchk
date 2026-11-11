// Package md048 checks MD048, which requires one code fence style: backticks or tildes.
package md048

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// Registration returns the MD048 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD048", Check: fencedBlockStyles}
}

func fencedBlockStyles(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
			out.IncompleteReason = ""
		}
	}()
	items, err := c.FenceStyles(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	switch items.Availability {
	case parser.Available:
		return compareFencedBlockStyles(ctx, c, items.Facts)
	case parser.Unavailable:
		return engine.RuleOutcome{IncompleteReason: "Fence-style context is unavailable; the complete MD048 inventory is withheld."}
	default:
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
}

func compareFencedBlockStyles(ctx context.Context, c engine.RuleContext, items []parser.FenceStyleFact) (out engine.RuleOutcome) {
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
		if out.Err = c.SectionCheckpoint(ctx, 3*int64(len(item.Expected))+18, 0); out.Err != nil {
			return out
		}
		if !s.Present() || s.SourceToken() != c.Source().Token() || s.Start() < previous || s.End() <= s.Start() || s.End() > c.Source().Len() || item.Expected != "backtick" && item.Expected != "tilde" {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		// Validate each endpoint with a two-byte source window, not a full copy.
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
		message := "Fenced code block should use backtick delimiters."
		if item.Expected == "tilde" {
			message = "Fenced code block should use tilde delimiters."
		}
		out.Findings = append(out.Findings, engine.Finding{Span: s, Message: message})
	}
	return out
}
