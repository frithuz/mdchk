package rules

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func uninformativeLinkLabels(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
			out.IncompleteReason = ""
		}
	}()
	items, err := c.LinkLabels(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return linkLabelOutcome(ctx, c, items)
}

func linkLabelOutcome(ctx context.Context, c engine.RuleContext, items parser.LinkLabelsInventory) engine.RuleOutcome {
	switch items.Availability {
	case parser.Available:
		phrases, ok := c.Options()["phrases"].([]string)
		if !ok {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		return compareLinkLabels(ctx, c, items.Items, phrases)
	case parser.Unavailable:
		return engine.RuleOutcome{IncompleteReason: "Link label context is unavailable; the complete MD059 inventory is withheld."}
	default:
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
}

func compareLinkLabels(ctx context.Context, c engine.RuleContext, items []parser.LinkLabel, configured []string) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	// Phrases are normalized once, like labels; an empty phrase matches nothing.
	if out.Err = c.SectionCheckpoint(ctx, int64(len(configured))+1, int64(len(configured))); out.Err != nil {
		return out
	}
	phrases := make([]string, 0, len(configured))
	for _, phrase := range configured {
		if out.Err = c.SectionCheckpoint(ctx, 2*int64(len(phrase))+1, 0); out.Err != nil {
			return out
		}
		if phrase = parser.NormalizeLinkLabel(phrase); phrase != "" {
			phrases = append(phrases, phrase)
		}
	}
	// Labels of Links outside Image descriptions never overlap, so ranges are ordered.
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
		if out.Err = c.SectionCheckpoint(ctx, 18, 0); out.Err != nil {
			return out
		}
		if !s.Present() || s.SourceToken() != c.Source().Token() || s.Start() < previous || s.End() < s.Start() || s.End() > c.Source().Len() {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		previous = s.End()
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
		matched := false
		for _, phrase := range phrases {
			if out.Err = c.SectionCheckpoint(ctx, int64(min(len(item.Text), len(phrase)))+1, 0); out.Err != nil {
				return out
			}
			if strings.EqualFold(item.Text, phrase) {
				matched = true
				break
			}
		}
		if !matched {
			continue
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
		out.Findings = append(out.Findings, engine.Finding{Span: s, Message: "Link label is uninformative."})
	}
	return out
}
