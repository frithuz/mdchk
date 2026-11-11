package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// BlankRunLines returns the excess blank runs of a completed parse from this
// operation for a resolved MD012 plan, with the resolved maximum. The correction
// producer and checker use it on each round's own parse.
func BlankRunLines(ctx context.Context, parsed parser.ParseResult, index parser.LineIndex, plan *ExecutionPlan, budget *parser.OperationBudget) ([][]parser.BlankRunLine, int, error) {
	if ctx == nil || plan == nil || budget == nil || parsed.Document == nil {
		return nil, 0, errors.New("nil blank run context")
	}
	rule, ok := plan.rules["MD012"]
	if !ok {
		return nil, 0, parser.ErrModel
	}
	maximum, ok := rule.Options["maximum"].(int)
	if !ok || maximum < 0 {
		return nil, 0, parser.ErrModel
	}
	runs, err := parsed.Document.BlankRuns(ctx, budget, index, plan.Context().ParseOptions(), maximum)
	return runs, maximum, err
}
