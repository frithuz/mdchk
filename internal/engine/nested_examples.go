package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// NestedExamples borrows original syntax under caller AND originating operation
// controls. Its detached observations carry neither fixes nor derived tokens.
func (c RuleContext) NestedExamples(ctx context.Context) (out parser.NestedExampleInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.NestedExampleInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if len(c.options) != 0 {
		return out, parser.ErrModel
	}
	return c.document.NestedExamples(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
