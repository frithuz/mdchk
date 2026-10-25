package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
)

func closingHashSpacing(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
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
