package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// ImageAlternatives retains both caller and originating controls and returns only
// detached image alternative observations. It grants no alternate parse or correction authority.
func (c RuleContext) ImageAlternatives(ctx context.Context) (out parser.ImageAlternativesInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.ImageAlternativesInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	decorative, ok := c.options["allow_decorative"].(bool)
	if !ok || len(c.options) != 1 {
		return out, parser.ErrModel
	}
	return c.document.ImageAlternatives(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), decorative)
}
