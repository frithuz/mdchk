package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// ParagraphIndentationFacts returns the surplus continuation indentation spans of
// a completed parse from this operation for a resolved MD085 plan. The correction
// producer and checker use it on each round's own parse, independently of any
// analysis fix flag.
func ParagraphIndentationFacts(ctx context.Context, parsed parser.ParseResult, index parser.LineIndex, plan *ExecutionPlan, budget *parser.OperationBudget) ([]parser.Span, error) {
	if ctx == nil || plan == nil || budget == nil || parsed.Document == nil {
		return nil, errors.New("nil paragraph indentation context")
	}
	rule, ok := plan.rules["MD085"]
	if !ok || len(rule.Options) != 0 {
		return nil, parser.ErrModel
	}
	return parsed.Document.ParagraphIndentation(ctx, budget, index, plan.Context().ParseOptions())
}

// ParagraphIndentationFixEligible resolves one MD085 span from retained facts and
// directive intervals, then requires that deleting the span keeps its line a
// paragraph continuation. Hidden and observational findings are never eligible,
// and neither is any span in a document opening with an unrecognized block.
func (c RuleContext) ParagraphIndentationFixEligible(ctx context.Context, span parser.Span) (bool, error) {
	if c.observational || !c.syntaxReliable || !c.directives.complete || c.document.UnrecognizedBlock() {
		return false, nil
	}
	if err := c.auditCharge(ctx, directiveLookupWork(len(c.directives.intervals["MD085"]))+1, 0); err != nil {
		return false, err
	}
	state, err := c.directives.anchorState("MD085", span.Start())
	if err != nil || !state.Enabled {
		return false, err
	}
	return c.document.ParagraphIndentationPreserved(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), span)
}
