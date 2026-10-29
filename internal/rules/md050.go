package rules

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

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
	return compareBoldDelimiterStyle(ctx, c, style, items.Valid)
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
