// Package md020 checks MD020, which requires a space before the closing hashes of a heading.
package md020

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
)

// Registration returns the MD020 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD020", Check: check}
}

func check(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	points, err := c.ClosingHashCandidates(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	out := engine.RuleOutcome{}
	for _, point := range points {
		if err := c.Checkpoint(ctx, 1); err != nil {
			out.Err = err
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: point, Message: "Terminal heading hashes must be preceded by a space."})
	}
	return out
}
