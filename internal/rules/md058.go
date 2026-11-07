package rules

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules/internal/blankpoints"
)

func tableBlankLines(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	items, err := c.TableSpacing(ctx)
	if err != nil {
		out.Err = err
		return out
	}
	return compareTableBlankLines(ctx, c, items)
}

func compareTableBlankLines(ctx context.Context, c engine.RuleContext, items []parser.TableSpacingFact) engine.RuleOutcome {
	return blankpoints.Compare(ctx, c, items, "Table")
}
