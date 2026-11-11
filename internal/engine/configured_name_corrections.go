package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// ConfiguredNameCorrections returns MD044's occurrence spans and their single
// configured replacements for a completed parse from this operation under a
// resolved MD044 plan. An empty replacement marks an ambiguous occurrence. The
// correction producer and checker use it on each round's own parse,
// independently of any analysis fix flag.
func ConfiguredNameCorrections(ctx context.Context, parsed parser.ParseResult, index parser.LineIndex, plan *ExecutionPlan, budget *parser.OperationBudget) ([]parser.Span, []string, error) {
	if ctx == nil || plan == nil || budget == nil || parsed.Document == nil {
		return nil, nil, errors.New("nil configured name context")
	}
	rule, ok := plan.rules["MD044"]
	if !ok {
		return nil, nil, parser.ErrModel
	}
	names, ok := rule.Options["names"].([]string)
	if !ok {
		return nil, nil, parser.ErrModel
	}
	code, ok := rule.Options["code_blocks"].(bool)
	if !ok {
		return nil, nil, parser.ErrModel
	}
	return parsed.Document.ConfiguredNameReplacements(ctx, budget, index, plan.Context().ParseOptions(), names, code)
}

// ConfiguredNameReplacements is ConfiguredNames with each occurrence's single
// configured replacement, empty when ambiguous. Serves MD044.
func (c RuleContext) ConfiguredNameReplacements(ctx context.Context) (spans []parser.Span, replacements []string, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			spans, replacements = nil, nil
		}
	}()
	values, ok := c.options["names"].([]string)
	if !ok || len(c.options) != 2 {
		return nil, nil, parser.ErrModel
	}
	code, ok := c.options["code_blocks"].(bool)
	if !ok {
		return nil, nil, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, int64(len(values))+1, int64(len(values))); err != nil {
		return nil, nil, err
	}
	names := make([]string, len(values))
	copy(names, values)
	return c.document.ConfiguredNameReplacements(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), names, code)
}
