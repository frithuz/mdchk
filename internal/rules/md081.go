package rules

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"math"
	"unicode/utf8"
)

func excessiveParagraphEmphasis(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	items, err := c.ParagraphEmphasis(ctx)
	if err != nil {
		out.Err = err
		return out
	}
	ratio, minimum, err := c.ParagraphEmphasisOptions(ctx)
	if err != nil {
		out.Err = err
		return out
	}
	return compareParagraphEmphasis(ctx, c, items, ratio, minimum)
}

func compareParagraphEmphasis(ctx context.Context, c engine.RuleContext, items []parser.ParagraphEmphasisFact, ratio float64, minimum int) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	if out.Err = c.SectionCheckpoint(ctx, 8, 0); out.Err != nil {
		return out
	}
	if minimum < 1 || math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 || ratio > 1 {
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
	previous := 0
	for i := 0; ; i++ {
		if out.Err = c.SectionCheckpoint(ctx, 1, 0); out.Err != nil {
			return out
		}
		if i == len(items) {
			break
		}
		if out.Err = c.SectionCheckpoint(ctx, 16, 0); out.Err != nil {
			return out
		}
		f := items[i]
		if f.Total < 0 || f.Emphasized < 0 || f.Emphasized > f.Total {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		if !f.Content.Present() {
			if f.Total != 0 {
				return engine.RuleOutcome{Err: parser.ErrModel}
			}
			continue
		}
		if f.Content.SourceToken() != c.Source().Token() || f.Content.Start() < previous || f.Content.Start() < 0 || f.Content.End() < f.Content.Start() || f.Content.End() > c.Source().Len() || f.Content.Start() == f.Content.End() && f.Total != 0 {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		previous = f.Content.End()
		// Bound both original endpoint windows before inspecting scalar/CRLF atoms.
		for _, at := range [2]int{f.Content.Start(), f.Content.End()} {
			if out.Err = c.SectionCheckpoint(ctx, 8, 0); out.Err != nil {
				return out
			}
			span, e := c.Source().Span(max(0, at-1), min(c.Source().Len(), at+1))
			if e != nil {
				return engine.RuleOutcome{Err: parser.ErrModel}
			}
			text, e := c.Source().Text(span)
			j := at - span.Start()
			if e != nil || at < c.Source().Len() && !utf8.RuneStart(text[j]) || at > 0 && at < c.Source().Len() && text[j-1] == '\r' && text[j] == '\n' {
				return engine.RuleOutcome{Err: parser.ErrModel}
			}
		}
		if out.Err = c.SectionCheckpoint(ctx, 4, 0); out.Err != nil {
			return out
		}
		if f.Total == 0 || f.Total < minimum || float64(f.Emphasized)/float64(f.Total) <= ratio {
			continue
		}
		if out.Err = c.SectionCheckpoint(ctx, 1, 0); out.Err != nil {
			return out
		}
		if len(out.Findings) == cap(out.Findings) {
			capacity := min(len(items), max(1, cap(out.Findings)*2))
			if out.Err = c.SectionCheckpoint(ctx, int64(len(out.Findings))+1, int64(capacity)); out.Err != nil {
				return out
			}
			next := make([]engine.Finding, len(out.Findings), capacity)
			copy(next, out.Findings)
			out.Findings = next
		}
		if out.Err = c.SectionCheckpoint(ctx, 1, 1); out.Err != nil {
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: f.Content, Message: "Paragraph contains excessive bold or italic text."})
	}
	return out
}
