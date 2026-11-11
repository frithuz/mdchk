// Package md084 checks MD084, which reports hidden or discouraged Unicode
// characters.
package md084

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// Registration returns the MD084 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD084", Check: hiddenCharacters}
}

func hiddenCharacters(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, err := c.HiddenCharacters(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return reportHiddenCharacters(ctx, c, items)
}

func reportHiddenCharacters(ctx context.Context, c engine.RuleContext, items []parser.Span) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	for _, item := range items {
		if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
			out.Err = err
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
		// Decoding the scalar and formatting its fixed-width message.
		if err := c.SectionCheckpoint(ctx, 16, 2); err != nil {
			out.Err = err
			return out
		}
		text, err := c.Source().Text(item)
		if err != nil {
			out.Err = err
			return out
		}
		r, width := utf8.DecodeRuneInString(text)
		if width != len(text) || r == utf8.RuneError {
			out.Err = parser.ErrModel
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: item, Message: fmt.Sprintf("Hidden or discouraged Unicode character U+%04X.", r)})
	}
	return out
}
