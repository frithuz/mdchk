package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/rules/internal/spanreport"
)

func prohibitedVocabulary(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, err := c.ProhibitedTerms(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return spanreport.Report(ctx, c, items, "Configured prohibited term.")
}
