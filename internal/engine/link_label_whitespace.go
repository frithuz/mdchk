package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// LinkLabelWhitespace retains both caller and originating controls and returns only
// detached label-edge observations. It grants no alternate parse or correction authority.
func (c RuleContext) LinkLabelWhitespace(ctx context.Context) (out parser.LinkLabelWhitespaceInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.LinkLabelWhitespaceInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if len(c.options) != 0 {
		return out, parser.ErrModel
	}
	return c.document.LinkLabelWhitespace(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
