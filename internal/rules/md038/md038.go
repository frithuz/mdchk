// Package md038 checks and corrects MD038, which reports whitespace inside inline code delimiters.
package md038

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// Registration returns the MD038 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD038", Check: codeSpanWhitespace}
}

func codeSpanWhitespace(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, err := c.CodeSpanSpaces(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return compareCodeSpanSpaces(ctx, c, items)
}

func compareCodeSpanSpaces(ctx context.Context, c engine.RuleContext, items []parser.Span) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	eligible, err := eligiblePairs(ctx, c, len(items))
	if err != nil {
		out.Err = err
		return out
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
		message := "Whitespace within inline code delimiters."
		out.Findings = append(out.Findings, engine.Finding{Span: item, Message: message, FixEligible: eligible[item.Start()] == item})
	}
	return out
}

// eligiblePairs indexes by start the edge runs a correction may delete: both
// runs of a span whose normalized value survives their deletion, when the
// interior edit query admits each of them. A span is never half-corrected, so
// one inadmissible run withholds its partner too.
func eligiblePairs(ctx context.Context, c engine.RuleContext, findings int) (map[int]parser.Span, error) {
	if findings == 0 {
		return nil, nil
	}
	pairs, err := c.DeletableCodeSpanSpaces(ctx)
	if err != nil {
		return nil, err
	}
	if len(pairs)%2 != 0 {
		return nil, parser.ErrModel
	}
	if err := c.SectionCheckpoint(ctx, int64(len(pairs))+1, int64(len(pairs))+1); err != nil {
		return nil, err
	}
	edits := c.InteriorEdits()
	out := make(map[int]parser.Span, len(pairs))
	for i := 0; i < len(pairs); i += 2 {
		opening, closing := pairs[i], pairs[i+1]
		ok, err := edits.Eligible(ctx, "MD038", opening, opening)
		if err != nil {
			return nil, err
		}
		if ok {
			ok, err = edits.Eligible(ctx, "MD038", closing, closing)
			if err != nil {
				return nil, err
			}
		}
		if ok {
			out[opening.Start()], out[closing.Start()] = opening, closing
		}
	}
	return out, nil
}
