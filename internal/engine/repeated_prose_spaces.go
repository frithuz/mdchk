package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// RepeatedProseSpaceSpans returns the excess prose SPACE spans of a completed
// parse from this operation for a resolved MD064 plan. The correction producer
// and checker use it on each round's own parse, independently of any analysis
// fix flag.
func RepeatedProseSpaceSpans(ctx context.Context, parsed parser.ParseResult, index parser.LineIndex, plan *ExecutionPlan, budget *parser.OperationBudget) ([]parser.Span, error) {
	if ctx == nil || plan == nil || budget == nil || parsed.Document == nil {
		return nil, errors.New("nil repeated prose space context")
	}
	rule, ok := plan.rules["MD064"]
	if !ok {
		return nil, parser.ErrModel
	}
	maximum, ok := rule.Options["maximum"].(int)
	if !ok || maximum < 1 {
		return nil, parser.ErrModel
	}
	return parsed.Document.RepeatedProseSpaces(ctx, budget, index, plan.Context().ParseOptions(), maximum)
}
