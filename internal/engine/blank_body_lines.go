package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// BlankBodyLines returns detached first-excess physical body spans under the
// admitted maximum and both caller and original operation controls.
func (c RuleContext) BlankBodyLines(ctx context.Context) (out []parser.Span, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	maximum, ok := c.options["maximum"].(int)
	if !ok || maximum < 0 {
		return nil, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, 0, 0); err != nil {
		return nil, err
	}
	return c.document.BlankBodyLines(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), maximum)
}
