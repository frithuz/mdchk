package rules

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"unicode/utf8"
)

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
	return compareASCIIPunctuation(ctx, c, items, style)
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
