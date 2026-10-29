package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// RepeatedProseSpaces borrows admitted scalar options and returns detached
// original surplus prose SPACE spans under both caller and originating controls.
func (c RuleContext) RepeatedProseSpaces(ctx context.Context) (out []parser.Span, err error) {
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
	if !ok || len(c.options) != 1 || maximum < 1 {
		return nil, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, 0, 0); err != nil {
		return nil, err
	}
	return c.document.RepeatedProseSpaces(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), maximum)
}
