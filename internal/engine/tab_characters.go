package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// TabCharacters borrows admitted scalar options and returns detached
// original TAB violations under both caller and originating controls.
func (c RuleContext) TabCharacters(ctx context.Context) (out []parser.Span, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	spaces, ok := c.options["spaces_per_tab"].(int)
	code, typed := c.options["code_blocks"].(bool)
	if !ok || !typed || spaces < 1 {
		return nil, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, 0, 0); err != nil {
		return nil, err
	}
	return c.document.TabCharacters(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), code)
}
