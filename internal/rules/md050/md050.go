// Package md050 checks and corrects MD050, which requires one bold delimiter style.
package md050

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// Registration returns the MD050 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD050", Check: boldDelimiterStyle}
}

func boldDelimiterStyle(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	style, err := c.DelimiterStyleOption(ctx)
	if err != nil {
		out.Err = err
		return out
	}
	items, err := c.DelimiterStyle(ctx)
	if err != nil {
		out.Err = err
		return out
	}
	out = compareBoldDelimiterStyle(ctx, c, style, items.Valid)
	if out.Err == nil && len(out.Findings) != 0 {
		out.Err = markFixEligible(ctx, c, items.Valid, out.Findings)
	}
	return out
}

// markFixEligible marks each finding whose Strong both markers can be rewritten
// to the expected marker, as BoldMarkerCorrections decides on this parse, when
// the shared interior edit query allows both marker edits under the opening.
func markFixEligible(ctx context.Context, c engine.RuleContext, items []parser.DelimiterFact, findings []engine.Finding) error {
	facts, _, err := c.BoldMarkerCorrections(ctx)
	if err != nil {
		return err
	}
	if err := c.SectionCheckpoint(ctx, int64(len(facts)+len(items))+1, int64(len(facts))+1); err != nil {
		return err
	}
	correctable := make(map[int]parser.DelimiterFact, len(facts))
	for _, f := range facts {
		correctable[f.Opening.Start()] = f
	}
	edits := c.InteriorEdits()
	for i := range findings {
		if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
			return err
		}
		f, ok := correctable[findings[i].Span.Start()]
		if !ok || f.Opening != findings[i].Span {
			continue
		}
		for _, span := range [2]parser.Span{f.Opening, f.Closing} {
			eligible, err := edits.Eligible(ctx, "MD050", f.Opening, span)
			if err != nil {
				return err
			}
			findings[i].FixEligible = eligible
			if !eligible {
				break
			}
		}
	}
	return nil
}

// The inventory need not be in opener order. Select the minimum original opener
// once per document, including disabled intervals; the engine filters findings
// at their opening START after this callback completes.
func compareBoldDelimiterStyle(ctx context.Context, c engine.RuleContext, style string, items []parser.DelimiterFact) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	if err := c.SectionCheckpoint(ctx, 34, 0); err != nil {
		out.Err = err
		return out
	}
	var expected byte
	switch style {
	case "asterisk":
		expected = '*'
	case "underscore":
		expected = '_'
	case "consistent":
	default:
		out.Err = parser.ErrModel
		return out
	}
	first := c.Source().Len()
	for i := 0; ; i++ {
		// Terminal probe, fact kind/width/marker, all three role owners/bounds,
		// containment and minimum selection are bounded before inspection.
		if err := c.SectionCheckpoint(ctx, 70, 0); err != nil {
			out.Err = err
			return out
		}
		if i == len(items) {
			break
		}
		f := items[i]
		if (f.Kind != parser.Emphasis || f.Width != 1) && (f.Kind != parser.Strong || f.Width != 2) || (f.Marker != '*' && f.Marker != '_') {
			out.Err = parser.ErrModel
			return out
		}
		for _, span := range [3]parser.Span{f.Envelope, f.Opening, f.Closing} {
			if !span.Present() || span.SourceToken() != c.Source().Token() || span.Start() < 0 || span.End() < span.Start() || span.End() > c.Source().Len() {
				out.Err = parser.ErrModel
				return out
			}
		}
		if f.Opening.End()-f.Opening.Start() != f.Width || f.Closing.End()-f.Closing.Start() != f.Width || f.Envelope.Start() > f.Opening.Start() || f.Opening.End() > f.Closing.Start() || f.Closing.End() > f.Envelope.End() {
			out.Err = parser.ErrModel
			return out
		}
		// At most four original marker bytes; never examine transformed children.
		for _, span := range [2]parser.Span{f.Opening, f.Closing} {
			text, err := c.Source().Text(span)
			if err != nil {
				out.Err = err
				return out
			}
			for j := range text {
				if text[j] != f.Marker {
					out.Err = parser.ErrModel
					return out
				}
			}
		}
		if style == "consistent" && f.Kind == parser.Strong && f.Opening.Start() < first {
			first, expected = f.Opening.Start(), f.Marker
		}
	}
	for i := 0; ; i++ {
		if err := c.SectionCheckpoint(ctx, 5, 0); err != nil {
			out.Err = err
			return out
		}
		if i == len(items) {
			break
		}
		f := items[i]
		if f.Kind != parser.Strong || f.Marker == expected {
			continue
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
		out.Findings = append(out.Findings, engine.Finding{Span: f.Opening, Message: "Bold delimiter style does not match the expected style."})
	}
	return out
}
