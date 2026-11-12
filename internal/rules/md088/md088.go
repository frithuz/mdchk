// Package md088 checks and corrects MD088, which requires ASCII quotes and
// dashes in prose.
package md088

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"unicode/utf8"
)

// Registration returns the MD088 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD088", Check: asciiPunctuation}
}

func asciiPunctuation(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	items, err := c.ASCIIPunctuation(ctx)
	if err != nil {
		out.Err = err
		return out
	}
	style, err := c.ASCIIPunctuationStyle(ctx)
	if err != nil {
		out.Err = err
		return out
	}
	out = compareASCIIPunctuation(ctx, c, items, style)
	if out.Err == nil && len(out.Findings) != 0 {
		out.Err = markFixEligible(ctx, c, out.Findings)
	}
	return out
}

// markFixEligible marks each finding whose scalar can be rewritten to its ASCII
// spelling, as ASCIIPunctuationCorrections decides on this parse, when the
// shared interior edit query allows the edit.
func markFixEligible(ctx context.Context, c engine.RuleContext, findings []engine.Finding) error {
	facts, err := c.ASCIIPunctuationCorrections(ctx)
	if err != nil {
		return err
	}
	if err := c.SectionCheckpoint(ctx, int64(len(facts))+1, int64(len(facts))+1); err != nil {
		return err
	}
	correctable := make(map[int]parser.Span, len(facts))
	for _, f := range facts {
		correctable[f.Span.Start()] = f.Span
	}
	edits := c.InteriorEdits()
	for i := range findings {
		if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
			return err
		}
		s, ok := correctable[findings[i].Span.Start()]
		if !ok || s != findings[i].Span {
			continue
		}
		eligible, err := edits.Eligible(ctx, "MD088", s, s)
		if err != nil {
			return err
		}
		findings[i].FixEligible = eligible
	}
	return nil
}

func compareASCIIPunctuation(ctx context.Context, c engine.RuleContext, items []parser.ASCIIPunctuationFact, style string) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	if out.Err = c.SectionCheckpoint(ctx, 8, 0); out.Err != nil {
		return out
	}
	if style != "ascii" {
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
		// Reserve 64 units for source/range validation and bounded UTF-8 decode,
		// plus 32 for ordering, six identities and caller bookkeeping.
		if out.Err = c.SectionCheckpoint(ctx, 96, 0); out.Err != nil {
			return out
		}
		f := items[i]
		s := f.Span
		if !s.Present() || s.SourceToken() != c.Source().Token() || s.Start() < previous || s.Start() < 0 || s.End()-s.Start() != 3 || s.End() > c.Source().Len() {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		text, e := c.Source().Text(s)
		if e != nil {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		r, width := utf8.DecodeRuneInString(text)
		if width != 3 || r != f.Scalar || r != '‘' && r != '’' && r != '“' && r != '”' && r != '–' && r != '—' {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		previous = s.End()
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
		out.Findings = append(out.Findings, engine.Finding{Span: s, Message: "Use ASCII quotes and dashes in prose."})
	}
	return out
}
