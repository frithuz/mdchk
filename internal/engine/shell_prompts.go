package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// ShellPrompts validates the resolved option shape and synchronously borrows the
// immutable admitted list. Neither the parser nor this facade exposes or mutates
// option storage. Original production authority and caller AND origin apply.
func (c RuleContext) ShellPrompts(ctx context.Context) (out parser.ShellPromptInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.ShellPromptInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 16, 0); err != nil {
		return out, err
	}
	languages, ok := c.options["languages"].([]string)
	if !ok || len(c.options) != 1 {
		return out, parser.ErrModel
	}
	return c.document.ShellPrompts(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), languages)
}
