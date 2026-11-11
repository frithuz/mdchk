// Rule fact facades for the code-blocks rules.

package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// ShellPrompts validates the resolved option shape and synchronously borrows the
// immutable admitted list. Neither the parser nor this facade exposes or mutates
// option storage. Original production authority and caller AND origin apply.
// Serves MD014.
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

// FencedSpacing borrows completed original syntax under caller AND originating
// operation controls. Returned facts are detached; no rule options are accepted.
// Serves MD031.
func (c RuleContext) FencedSpacing(ctx context.Context) (out parser.FencedSpacingInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.FencedSpacingInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if len(c.options) != 0 {
		return out, parser.ErrModel
	}
	return c.document.FencedSpacing(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// FencedLanguages borrows the admitted immutable allowlist synchronously and
// preserves original syntax authority, allowance, and caller AND origin controls.
// Serves MD040.
func (c RuleContext) FencedLanguages(ctx context.Context) (out parser.FencedLanguageInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.FencedLanguageInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 16, 0); err != nil {
		return out, err
	}
	allowed, ok := c.options["allowed_languages"].([]string)
	if !ok || len(c.options) != 1 {
		return out, parser.ErrModel
	}
	return c.document.FencedLanguages(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), allowed)
}

// CodeStyles uses original syntax and the same operation, including caller AND
// originating controls. The immutable style value is borrowed synchronously.
// Serves MD046.
func (c RuleContext) CodeStyles(ctx context.Context) (out parser.CodeStyleInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.CodeStyleInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 16, 0); err != nil {
		return out, err
	}
	style, ok := c.options["style"].(string)
	if !ok || len(c.options) != 1 {
		return out, parser.ErrModel
	}
	return c.document.CodeStyles(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), style)
}

// FenceStyles uses original syntax and the same operation, including caller AND
// originating controls. The immutable style value is borrowed synchronously.
// Serves MD048.
func (c RuleContext) FenceStyles(ctx context.Context) (out parser.FenceStyleInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.FenceStyleInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 16, 0); err != nil {
		return out, err
	}
	style, ok := c.options["style"].(string)
	if !ok || len(c.options) != 1 {
		return out, parser.ErrModel
	}
	return c.document.FenceStyles(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), style)
}

// NestedExamples borrows original syntax under caller AND originating operation
// controls. Its detached observations carry neither fixes nor derived tokens.
// Serves MD070.
func (c RuleContext) NestedExamples(ctx context.Context) (out parser.NestedExampleInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = parser.NestedExampleInventory{}
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return out, err
	}
	if len(c.options) != 0 {
		return out, parser.ErrModel
	}
	return c.document.NestedExamples(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
