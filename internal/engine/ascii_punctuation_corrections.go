package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// ASCIIPunctuationCorrections returns MD088's rewritable scalar facts for a
// completed parse from this operation under a resolved MD088 plan. The
// correction producer and checker use it on each round's own parse,
// independently of any analysis fix flag.
func ASCIIPunctuationCorrections(ctx context.Context, parsed parser.ParseResult, index parser.LineIndex, plan *ExecutionPlan, budget *parser.OperationBudget) ([]parser.ASCIIPunctuationFact, error) {
	if ctx == nil || plan == nil || budget == nil || parsed.Document == nil {
		return nil, errors.New("nil ASCII punctuation context")
	}
	rule, ok := plan.rules["MD088"]
	if !ok {
		return nil, parser.ErrModel
	}
	if style, ok := rule.Options["style"].(string); !ok || style != "ascii" {
		return nil, parser.ErrModel
	}
	return parsed.Document.ASCIIPunctuationCorrections(ctx, budget, index, plan.Context().ParseOptions())
}

// ASCIIPunctuationCorrections returns the ASCIIPunctuation facts whose scalar
// can be rewritten to its ASCII spelling. Serves MD088.
func (c RuleContext) ASCIIPunctuationCorrections(ctx context.Context) (out []parser.ASCIIPunctuationFact, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	if _, err = c.ASCIIPunctuationStyle(ctx); err != nil {
		return nil, err
	}
	return c.document.ASCIIPunctuationCorrections(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
