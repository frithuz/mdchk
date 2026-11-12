package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// BoldMarkerCorrections returns MD050's rewritable Strong facts and the expected
// bold marker for a completed parse from this operation under a resolved MD050
// plan. The correction producer and checker use it on each round's own parse,
// independently of any analysis fix flag.
func BoldMarkerCorrections(ctx context.Context, parsed parser.ParseResult, index parser.LineIndex, plan *ExecutionPlan, budget *parser.OperationBudget) ([]parser.DelimiterFact, byte, error) {
	if ctx == nil || plan == nil || budget == nil || parsed.Document == nil {
		return nil, 0, errors.New("nil bold marker context")
	}
	rule, ok := plan.rules["MD050"]
	if !ok {
		return nil, 0, parser.ErrModel
	}
	style, ok := rule.Options["style"].(string)
	if !ok {
		return nil, 0, parser.ErrModel
	}
	return parsed.Document.BoldMarkerCorrections(ctx, budget, index, plan.Context().ParseOptions(), style)
}

// BoldMarkerCorrections returns the Strong facts whose markers can both be
// rewritten to the expected style, and that marker. Serves MD050.
func (c RuleContext) BoldMarkerCorrections(ctx context.Context) (facts []parser.DelimiterFact, expected byte, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, 0, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			facts, expected = nil, 0
		}
	}()
	style, err := c.DelimiterStyleOption(ctx)
	if err != nil {
		return nil, 0, err
	}
	return c.document.BoldMarkerCorrections(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), style)
}
