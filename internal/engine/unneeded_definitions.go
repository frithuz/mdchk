package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// UnneededDefinitions retains both caller and originating controls and returns
// only detached observations. ignored_labels is read from admitted immutable
// options; it grants no alternate parse or correction authority.
func (c RuleContext) UnneededDefinitions(ctx context.Context) (out parser.UnneededDefinitionInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.UnneededDefinitionInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 16, 0); err != nil {
		return out, err
	}
	list, ok := c.options["ignored_labels"].([]string)
	if !ok || len(c.options) != 1 {
		return out, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, int64(len(list))+1, int64(len(list))); err != nil {
		return out, err
	}
	ignored := make([]string, len(list))
	copy(ignored, list)
	return c.document.UnneededDefinitions(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), ignored)
}
