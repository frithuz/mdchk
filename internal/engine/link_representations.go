package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// LinkRepresentations retains both caller and originating controls and returns only
// detached link and image style observations. It grants no alternate parse or correction authority.
func (c RuleContext) LinkRepresentations(ctx context.Context) (out parser.LinkRepresentationsInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.LinkRepresentationsInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if _, ok := c.options["allowed_styles"].([]string); !ok || len(c.options) != 1 {
		return out, parser.ErrModel
	}
	return c.document.LinkRepresentations(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
