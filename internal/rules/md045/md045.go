// Package md045 checks MD045, which reports images without alternative text.
package md045

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules/internal/findings"
)

// Registration returns the MD045 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD045", Check: imageAlternatives}
}

func imageAlternatives(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out = engine.RuleOutcome{Err: out.Err}
		}
	}()
	inventory, err := c.ImageAlternatives(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	if inventory.Availability != parser.Available && inventory.Availability != parser.Unavailable {
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
	for _, span := range inventory.Spans {
		if out.Err = c.SectionCheckpoint(ctx, 2, 1); out.Err != nil {
			return out
		}
		out.Findings, out.Err = findings.Append(ctx, c, out.Findings, engine.Finding{Span: span, Message: "Image alternative text is missing or blank."})
		if out.Err != nil {
			return out
		}
	}
	for _, span := range inventory.Uncertainty {
		if out.Err = c.SectionCheckpoint(ctx, 2, 1); out.Err != nil {
			return out
		}
		out.ParsingProblems, out.Err = findings.Append(ctx, c, out.ParsingProblems, engine.Finding{Span: span, Message: "Image alternative text or decorative attributes cannot be interpreted reliably."})
		if out.Err != nil {
			return out
		}
	}
	if inventory.Availability == parser.Unavailable && len(inventory.Uncertainty) == 0 {
		out.IncompleteReason = "Image alternative context is unavailable."
	}
	return out
}
