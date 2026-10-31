package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// FencedSpacing borrows completed original syntax under caller AND originating
// operation controls. Returned facts are detached; no rule options are accepted.
func (c RuleContext) FencedSpacing(ctx context.Context) (out parser.FencedSpacingInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.FencedSpacingInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if len(c.options) != 0 {
		return out, parser.ErrModel
	}
	return c.document.FencedSpacing(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
