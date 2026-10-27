package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// MaximumLineWidth borrows admitted scalar options and returns detached
// original overflowing scalar violations under both caller and originating controls.
func (c RuleContext) MaximumLineWidth(ctx context.Context) (out []parser.Span, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	length, ok := c.options["length"].(int)
	code, typed := c.options["code_blocks"].(bool)
	tables, tableTyped := c.options["tables"].(bool)
	headings, headingTyped := c.options["headings"].(bool)
	if !ok || !typed || !tableTyped || !headingTyped || length < 1 {
		return nil, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, 0, 0); err != nil {
		return nil, err
	}
	return c.document.MaximumLineWidth(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), length, code, tables, headings)
}
