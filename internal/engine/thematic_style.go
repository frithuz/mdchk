package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// ThematicStyleEdits returns the mismatched marker envelopes of a completed
// parse from this operation for a resolved MD035 plan, with the replacement
// each one takes. The correction producer and checker use it on each round's
// own parse.
func ThematicStyleEdits(ctx context.Context, parsed parser.ParseResult, index parser.LineIndex, plan *ExecutionPlan, budget *parser.OperationBudget) ([]parser.Span, []byte, error) {
	if ctx == nil || plan == nil || budget == nil || parsed.Document == nil {
		return nil, nil, errors.New("nil thematic style context")
	}
	rule, ok := plan.rules["MD035"]
	if !ok {
		return nil, nil, parser.ErrModel
	}
	style, ok := rule.Options["style"].(string)
	if !ok {
		return nil, nil, parser.ErrModel
	}
	return parsed.Document.ThematicStyleCorrection(ctx, budget, index, plan.Context().ParseOptions(), style)
}
