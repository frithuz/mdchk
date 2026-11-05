package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
)

func suspiciousSequences(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, err := c.SuspiciousSequences(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return reportSpans(ctx, c, items, "Text may be corrupted by an encoding mismatch.")
}
