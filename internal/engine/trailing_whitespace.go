package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// TrailingWhitespace borrows admitted scalar options and returns detached
// original suffix violations under both caller and originating controls.
func (c RuleContext) TrailingWhitespace(ctx context.Context) (out []parser.Span, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	spaces, ok := c.options["hard_break_spaces"].(int)
	strict, valid := c.options["strict"].(bool)
	code, typed := c.options["code_blocks"].(bool)
	if !ok || !valid || !typed || spaces < 2 {
		return nil, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, 0, 0); err != nil {
		return nil, err
	}
	return c.document.TrailingWhitespace(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), spaces, strict, code)
}
