package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// BulletIndentationItems returns the list item facts of a completed parse from
// this operation with the resolved MD007 indent and start_indented options. The
// correction producer and checker use it on each round's own parse.
func BulletIndentationItems(ctx context.Context, parsed parser.ParseResult, index parser.LineIndex, plan *ExecutionPlan, budget *parser.OperationBudget) ([]parser.ListItemFact, int, bool, error) {
	if ctx == nil || plan == nil || budget == nil || parsed.Document == nil {
		return nil, 0, false, errors.New("nil bullet indentation context")
	}
	rule, ok := plan.rules["MD007"]
	if !ok {
		return nil, 0, false, parser.ErrModel
	}
	indent, ok := rule.Options["indent"].(int)
	start, valid := rule.Options["start_indented"].(bool)
	if !ok || !valid || indent <= 0 {
		return nil, 0, false, parser.ErrModel
	}
	items, err := parsed.Document.ListItems(ctx, budget, index, plan.Context().ParseOptions())
	return items, indent, start, err
}
