package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// BlockquoteSpaces returns detached original excess-space spans under the
// admitted completed syntax and both caller and originating controls.
func (c RuleContext) BlockquoteSpaces(ctx context.Context) (out []parser.Span, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	if len(c.options) != 0 {
		return nil, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, 0, 0); err != nil {
		return nil, err
	}
	return c.document.BlockquoteSpaces(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
