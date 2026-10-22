package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// TerminalEditEligible conservatively proves the context of a suffix-only edit.
// A complete parse from this same operation is required. EOF touching an opaque
// envelope is withheld: unchanged payload alone cannot prove unchanged binding.
// The common combined-candidate checker remains authoritative after this query.
func TerminalEditEligible(ctx context.Context, parsed parser.ParseResult, state DirectiveResult, id string, span parser.Span, budget *parser.OperationBudget) (bool, error) {
	if ctx == nil || budget == nil {
		return false, errors.New("nil terminal edit context or budget")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := budget.ChargeWork(1); err != nil {
		return false, err
	}
	if parsed.Status != parser.Complete || parsed.AST != parser.Available || len(parsed.Uncertainty) != 0 || parsed.Document == nil || parsed.Document.Source() != parsed.Source || state.Source() != parsed.Source || !state.Complete() || !state.InOperation(budget) {
		return false, nil
	}
	if _, err := parsed.Source.Text(span); err != nil || span.End() != parsed.Source.Len() {
		return false, nil
	}
	permission, err := state.AllowsEdit(ctx, budget, id, span, span)
	if err != nil || !permission.Allowed {
		return false, err
	}
	protected, err := parsed.Document.ProtectedNodes(ctx, budget)
	if err != nil {
		return false, err
	}
	for _, node := range protected {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if err := budget.ChargeWork(1); err != nil {
			return false, err
		}
		if node.Span().End() > span.Start() || (span.Start() == span.End() && node.Span().End() == span.Start()) {
			return false, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return true, budget.ChargeWork(0)
}
