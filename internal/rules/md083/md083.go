// Package md083 checks MD083, which reports character sequences that suggest an
// encoding mismatch.
package md083

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/rules/internal/spanreport"
)

// Registration returns the MD083 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD083", Check: suspiciousSequences}
}

func suspiciousSequences(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, err := c.SuspiciousSequences(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return spanreport.Report(ctx, c, items, "Text may be corrupted by an encoding mismatch.")
}
