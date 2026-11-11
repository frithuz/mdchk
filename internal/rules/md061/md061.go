// Package md061 checks MD061.
package md061

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/rules/internal/spanreport"
)

// Registration returns the MD061 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD061", Check: prohibitedVocabulary}
}

func prohibitedVocabulary(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, err := c.ProhibitedTerms(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return spanreport.Report(ctx, c, items, "Configured prohibited term.")
}
