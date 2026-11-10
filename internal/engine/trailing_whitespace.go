package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// TrailingWhitespaceItem adds per-finding fix availability to one suffix fact.
type TrailingWhitespaceItem struct {
	parser.TrailingSuffix
	FixEligible bool
}

// TrailingWhitespaceSuffixes returns the suffix facts of a completed parse from
// this operation for a resolved MD009 plan. The correction producer and checker
// use it on each round's own parse, independently of any analysis fix flag.
func TrailingWhitespaceSuffixes(ctx context.Context, parsed parser.ParseResult, index parser.LineIndex, plan *ExecutionPlan, budget *parser.OperationBudget) ([]parser.TrailingSuffix, int, error) {
	if ctx == nil || plan == nil || budget == nil || parsed.Document == nil {
		return nil, 0, errors.New("nil trailing whitespace context")
	}
	rule, ok := plan.rules["MD009"]
	if !ok {
		return nil, 0, parser.ErrModel
	}
	spaces, strict, code, err := trailingWhitespaceOptions(rule.Options)
	if err != nil {
		return nil, 0, err
	}
	items, err := parsed.Document.TrailingWhitespace(ctx, budget, index, plan.Context().ParseOptions(), spaces, strict, code)
	return items, spaces, err
}

func trailingWhitespaceOptions(options Options) (int, bool, bool, error) {
	spaces, ok := options["hard_break_spaces"].(int)
	strict, valid := options["strict"].(bool)
	code, typed := options["code_blocks"].(bool)
	if !ok || !valid || !typed || spaces < 2 {
		return 0, false, false, parser.ErrModel
	}
	return spaces, strict, code, nil
}

// TrailingWhitespace borrows admitted scalar options and returns detached
// original suffix violations under both caller and originating controls.
func (c RuleContext) TrailingWhitespace(ctx context.Context) (out []TrailingWhitespaceItem, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	spaces, strict, code, err := trailingWhitespaceOptions(c.options)
	if err != nil {
		return nil, err
	}
	if err = c.SectionCheckpoint(ctx, 0, 0); err != nil {
		return nil, err
	}
	items, err := c.document.TrailingWhitespace(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), spaces, strict, code)
	if err != nil {
		return nil, err
	}
	if err = c.SectionCheckpoint(ctx, int64(len(items)), int64(len(items))); err != nil {
		return nil, err
	}
	out = make([]TrailingWhitespaceItem, len(items))
	for i, item := range items {
		eligible, err := c.trailingFixEligible(ctx, item)
		if err != nil {
			return nil, err
		}
		out[i] = TrailingWhitespaceItem{item, eligible}
	}
	return out, nil
}

// trailingFixEligible resolves one suffix from retained facts and directive
// intervals only. Hidden and observational findings are never eligible, and
// neither is any suffix in a document opening with an unrecognized block. A suffix
// at physical EOF wins its conflict with MD047, whose final newline follows later.
func (c RuleContext) trailingFixEligible(ctx context.Context, item parser.TrailingSuffix) (bool, error) {
	if c.observational || !c.syntaxReliable || !c.directives.complete || c.document.UnrecognizedBlock() || item.Literal || item.Withheld || item.Oversized {
		return false, nil
	}
	if err := c.auditCharge(ctx, directiveLookupWork(len(c.directives.intervals["MD009"]))+1, 0); err != nil {
		return false, err
	}
	state, err := c.directives.anchorState("MD009", item.Start())
	if err != nil {
		return false, err
	}
	return state.Enabled, nil
}
