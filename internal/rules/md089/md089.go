// Package md089 checks MD089, which reports a missing space between CJK and Latin text.
package md089

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// Registration returns the MD089 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD089", Check: check}
}

func check(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, err := c.CJKSpacing(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return compareCJKSpacing(ctx, c, items)
}

func compareCJKSpacing(ctx context.Context, c engine.RuleContext, items []parser.Span) (out engine.RuleOutcome) {
	edits := c.InteriorEdits()
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	// Heading and reference-label points name an anchor or a definition, so an
	// inserted space would break what links to them; they stay manual.
	manual := map[int]bool{}
	if len(items) > 0 {
		points, err := c.CJKSpacingManual(ctx)
		if err != nil {
			out.Err = err
			return out
		}
		for _, p := range points {
			manual[p.Start()] = true
		}
	}
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
		if err := c.SectionCheckpoint(ctx, 1, 1); err != nil {
			out.Err = err
			return out
		}
		message := "Missing space between CJK and Latin text."
		finding := engine.Finding{Span: item, Message: message}
		// The shared query refuses a zero-width span, so eligibility covers the
		// pair's two scalars; the producer rechecks them.
		pair, err := pairSpan(c.Source(), item)
		if err != nil {
			out.Err = err
			return out
		}
		if !manual[item.Start()] {
			finding.FixEligible, err = edits.Eligible(ctx, "MD089", item, pair)
			if err != nil {
				out.Err = err
				return out
			}
		}
		out.Findings = append(out.Findings, finding)
	}
	return out
}

// pairSpan returns the span of the scalar before point through the scalar after it.
func pairSpan(src parser.Source, point parser.Span) (parser.Span, error) {
	around, err := src.Span(max(0, point.Start()-utf8.UTFMax), min(src.Len(), point.End()+utf8.UTFMax))
	if err != nil {
		return parser.Span{}, err
	}
	text, err := src.Text(around)
	if err != nil {
		return parser.Span{}, err
	}
	split := point.Start() - around.Start()
	_, left := utf8.DecodeLastRuneInString(text[:split])
	_, right := utf8.DecodeRuneInString(text[split:])
	return src.Span(point.Start()-left, point.End()+right)
}
