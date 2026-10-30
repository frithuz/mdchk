package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// ThematicSpacing projects detached missing-side original points under both
// caller and originating operation controls. It accepts no rule options.
func (c RuleContext) ThematicSpacing(ctx context.Context) (out []parser.ThematicSpacingFact, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	if len(c.options) != 0 {
		return nil, parser.ErrModel
	}
	return c.document.ThematicSpacing(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
