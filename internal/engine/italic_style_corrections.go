package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// ItalicStyleCorrections returns MD049's mismatched italic facts and the
// expected marker for a completed parse from this operation under a resolved
// MD049 plan: the configured style, or in consistent mode the marker of the
// document's first italic. The correction producer and checker use it on each
// round's own parse, independently of any analysis fix flag.
func ItalicStyleCorrections(ctx context.Context, parsed parser.ParseResult, index parser.LineIndex, plan *ExecutionPlan, budget *parser.OperationBudget) ([]parser.DelimiterFact, byte, error) {
	if ctx == nil || plan == nil || budget == nil || parsed.Document == nil {
		return nil, 0, errors.New("nil italic style context")
	}
	rule, ok := plan.rules["MD049"]
	if !ok {
		return nil, 0, parser.ErrModel
	}
	style, ok := rule.Options["style"].(string)
	if !ok {
		return nil, 0, parser.ErrModel
	}
	items, err := parsed.Document.DelimiterStyle(ctx, budget, index, plan.Context().ParseOptions())
	if err != nil {
		return nil, 0, err
	}
	if err := budget.ChargeWork(int64(len(items.Valid))*2 + 1); err != nil {
		return nil, 0, err
	}
	var expected byte
	switch style {
	case "asterisk":
		expected = '*'
	case "underscore":
		expected = '_'
	case "consistent":
		first := -1
		for _, f := range items.Valid {
			if f.Kind == parser.Emphasis && (first < 0 || f.Opening.Start() < first) {
				first, expected = f.Opening.Start(), f.Marker
			}
		}
	default:
		return nil, 0, parser.ErrModel
	}
	var out []parser.DelimiterFact
	for _, f := range items.Valid {
		if f.Kind == parser.Emphasis && f.Width == 1 && f.Marker != expected {
			out = append(out, f)
		}
	}
	return out, expected, nil
}

// ItalicSwapsPreserved reports, for each mismatched italic fact, whether
// rewriting both of its markers to marker keeps the parse, under both caller
// and originating controls. Serves MD049.
func (c RuleContext) ItalicSwapsPreserved(ctx context.Context, swaps []parser.DelimiterFact, marker byte) (out []bool, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	if _, err = c.DelimiterStyleOption(ctx); err != nil {
		return nil, err
	}
	return c.document.ItalicSwapsPreserved(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), swaps, marker)
}
