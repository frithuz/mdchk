// Rule fact facades for the whitespace rules.

package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// BlankBodyLines returns detached first-excess physical body spans under the
// admitted maximum and both caller and original operation controls.
// Serves MD012.
func (c RuleContext) BlankBodyLines(ctx context.Context) (out []parser.Span, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	maximum, ok := c.options["maximum"].(int)
	if !ok || maximum < 0 {
		return nil, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, 0, 0); err != nil {
		return nil, err
	}
	return c.document.BlankBodyLines(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), maximum)
}

// MaximumLineWidth borrows admitted scalar options and returns detached
// original overflowing scalar violations under both caller and originating controls.
// Serves MD013.
func (c RuleContext) MaximumLineWidth(ctx context.Context) (out []parser.Span, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	length, ok := c.options["length"].(int)
	code, typed := c.options["code_blocks"].(bool)
	tables, tableTyped := c.options["tables"].(bool)
	headings, headingTyped := c.options["headings"].(bool)
	if !ok || !typed || !tableTyped || !headingTyped || length < 1 {
		return nil, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, 0, 0); err != nil {
		return nil, err
	}
	return c.document.MaximumLineWidth(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), length, code, tables, headings)
}

// BlockquoteSpaces returns detached original excess-space spans under the
// admitted completed syntax and both caller and originating controls.
// Serves MD027.
func (c RuleContext) BlockquoteSpaces(ctx context.Context) (out []parser.Span, err error) {
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
	return c.document.BlockquoteSpaces(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// BlockquoteGaps returns detached original blank-gap spans under the
// admitted completed syntax and both caller and originating controls.
// Serves MD028.
func (c RuleContext) BlockquoteGaps(ctx context.Context) (out []parser.Span, err error) {
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
	return c.document.BlockquoteGaps(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// RepeatedProseSpaces borrows admitted scalar options and returns detached
// original surplus prose SPACE spans under both caller and originating controls.
// Serves MD064.
func (c RuleContext) RepeatedProseSpaces(ctx context.Context) (out []parser.Span, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	maximum, ok := c.options["maximum"].(int)
	if !ok || len(c.options) != 1 || maximum < 1 {
		return nil, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, 0, 0); err != nil {
		return nil, err
	}
	return c.document.RepeatedProseSpaces(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), maximum)
}

// ParagraphIndentation borrows completed original Paragraph ownership and returns
// detached original surplus SPACE/TAB spans under both caller and originating controls.
// Serves MD085.
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

// CJKSpacing returns detached original CJK/Latin insertion points under both caller and originating controls.
// Serves MD089.
func (c RuleContext) CJKSpacing(ctx context.Context) (out []parser.Span, err error) {
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
	return c.document.CJKSpacing(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
