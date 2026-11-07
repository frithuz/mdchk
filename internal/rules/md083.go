package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/rules/internal/spanreport"
)

func suspiciousSequences(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, err := c.SuspiciousSequences(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return spanreport.Report(ctx, c, items, "Text may be corrupted by an encoding mismatch.")
}
