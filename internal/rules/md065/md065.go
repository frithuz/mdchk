// Package md065 checks MD065, which requires blank lines around horizontal rules.
package md065

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules/internal/blankpoints"
)

// Registration returns the MD065 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD065", Check: horizontalRuleBlankLines}
}

func horizontalRuleBlankLines(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	items, err := c.ThematicSpacing(ctx)
	if err != nil {
		out.Err = err
		return out
	}
	return compareHorizontalRuleBlankLines(ctx, c, items)
}

func compareHorizontalRuleBlankLines(ctx context.Context, c engine.RuleContext, items []parser.ThematicSpacingFact) engine.RuleOutcome {
	return blankpoints.Compare(ctx, c, items, "Horizontal rule")
}
