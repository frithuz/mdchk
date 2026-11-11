package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// DeletableCodeSpanSpaceSpans returns the deletable code-span edge SPACE pairs
// of a completed parse from this operation for a resolved MD038 plan. The
// correction producer and checker use it on each round's own parse,
// independently of any analysis fix flag.
func DeletableCodeSpanSpaceSpans(ctx context.Context, parsed parser.ParseResult, index parser.LineIndex, plan *ExecutionPlan, budget *parser.OperationBudget) ([]parser.Span, error) {
	if ctx == nil || plan == nil || budget == nil || parsed.Document == nil {
		return nil, errors.New("nil code span space context")
	}
	if _, ok := plan.rules["MD038"]; !ok {
		return nil, parser.ErrModel
	}
	return parsed.Document.DeletableCodeSpanSpaces(ctx, budget, index, plan.Context().ParseOptions())
}
