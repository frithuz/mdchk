package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// LinkLabels retains both caller and originating controls and returns only
// detached visible link label observations. It grants no alternate parse or correction authority.
func (c RuleContext) LinkLabels(ctx context.Context) (out parser.LinkLabelsInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.LinkLabelsInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if _, ok := c.options["phrases"].([]string); !ok || len(c.options) != 1 {
		return out, parser.ErrModel
	}
	return c.document.LinkLabels(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
