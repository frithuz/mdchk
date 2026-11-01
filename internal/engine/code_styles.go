package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// CodeStyles uses original syntax and the same operation, including caller AND
// originating controls. The immutable style value is borrowed synchronously.
func (c RuleContext) CodeStyles(ctx context.Context) (out parser.CodeStyleInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.CodeStyleInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 16, 0); err != nil {
		return out, err
	}
	style, ok := c.options["style"].(string)
	if !ok || len(c.options) != 1 {
		return out, parser.ErrModel
	}
	return c.document.CodeStyles(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), style)
}
