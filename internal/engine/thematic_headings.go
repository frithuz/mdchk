package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// ThematicBreaksBeforeHeadings borrows only original production syntax and
// withholds dependent facts if either caller or originating controls stop.
func (c RuleContext) ThematicBreaksBeforeHeadings(ctx context.Context) (out []parser.Span, err error) {
	if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	return c.document.ThematicBreaksBeforeHeadings(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// BooleanOption reads one immutable admitted option without copying collections.
// Applicability may use it before syntax is available; it grants no syntax facts.
func (c RuleContext) BooleanOption(ctx context.Context, name string) (bool, error) {
	if ctx == nil || c.operation == nil || c.budget == nil {
		return false, parser.ErrModel
	}
	if err := errors.Join(ctx.Err(), c.operation.Err(), c.budget.ChargeWork(1)); err != nil {
		return false, err
	}
	value, ok := c.options[name].(bool)
	if !ok {
		return false, parser.ErrModel
	}
	return value, nil
}
