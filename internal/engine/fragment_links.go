package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// FragmentLinks retains both caller and originating controls and returns only
// detached link destination observations. It grants no alternate parse or correction authority.
func (c RuleContext) FragmentLinks(ctx context.Context) (out parser.FragmentLinksInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.FragmentLinksInventory{}
		}
	}()
	if _, ok := c.options["cross_file"].(bool); !ok || len(c.options) != 1 {
		return out, parser.ErrModel
	}
	return c.document.FragmentLinks(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// CrossFile reports the validated rule option; it does not grant resource access.
func (c RuleContext) CrossFile() bool {
	v, _ := c.options["cross_file"].(bool)
	return v
}
