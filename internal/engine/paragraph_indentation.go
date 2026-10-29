package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// ParagraphIndentation borrows completed original Paragraph ownership and returns
// detached original surplus SPACE/TAB spans under both caller and originating controls.
func (c RuleContext) ParagraphIndentation(ctx context.Context) (out []parser.Span, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	if len(c.options) != 0 {
		return nil, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, 0, 0); err != nil {
		return nil, err
	}
	return c.document.ParagraphIndentation(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
