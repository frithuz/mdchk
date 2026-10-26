package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// ListItems exposes detached original list facts under caller and origin control.
func (c RuleContext) ListItems(ctx context.Context) (out []parser.ListItemFact, err error) {
	if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	return c.document.ListItems(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// BulletMarker returns the admitted explicit marker; zero means document-first.
// The option is borrowed, never exposed as mutable configuration.
func (c RuleContext) BulletMarker(ctx context.Context) (byte, error) {
	if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return 0, err
	}
	value, ok := c.options["marker"].(string)
	if !ok {
		return 0, parser.ErrModel
	}
	if err := c.SectionCheckpoint(ctx, int64(len(value))+1, 0); err != nil {
		return 0, err
	}
	switch value {
	case "consistent":
		return 0, nil
	case "asterisk":
		return '*', nil
	case "plus":
		return '+', nil
	case "dash":
		return '-', nil
	default:
		return 0, parser.ErrModel
	}
}

// BulletIndentation borrows admitted immutable MD007 scalars under the original
// operation controls, without cloning an option map or creating an allowance.
func (c RuleContext) BulletIndentation(ctx context.Context) (int, bool, error) {
	if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return 0, false, err
	}
	indent, ok := c.options["indent"].(int)
	start, valid := c.options["start_indented"].(bool)
	if !ok || !valid || indent <= 0 {
		return 0, false, parser.ErrModel
	}
	if err := c.SectionCheckpoint(ctx, 0, 0); err != nil {
		return 0, false, err
	}
	return indent, start, nil
}
