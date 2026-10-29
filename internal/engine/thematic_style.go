package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// ThematicStyle borrows one admitted scalar style and projects detached original
// marker envelopes under both caller and originating operation controls.
func (c RuleContext) ThematicStyle(ctx context.Context) (out []parser.Span, err error) {
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
	style, ok := c.options["style"].(string)
	if !ok || len(c.options) != 1 {
		return nil, parser.ErrModel
	}
	return c.document.ThematicStyle(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), style)
}
