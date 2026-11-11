// Package md018 checks MD018, which requires a space after the opening hashes of an ATX heading.
package md018

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
)

// Registration returns the MD018 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD018", Check: check}
}

func check(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	points, err := c.OpeningHashCandidates(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	out := engine.RuleOutcome{}
	for _, point := range points {
		if err := c.Checkpoint(ctx, 1); err != nil {
			out.Err = err
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: point, Message: "Opening hashes must be followed by a space."})
	}
	return out
}
