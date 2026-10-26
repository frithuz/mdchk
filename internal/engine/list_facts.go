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

// OrderedListStyle borrows the admitted immutable MD029 style under the original
// operation controls; true selects numeric one for every actual ordered item.
func (c RuleContext) OrderedListStyle(ctx context.Context) (bool, error) {
	if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return false, err
	}
	value, ok := c.options["style"].(string)
	if !ok || (value != "ordered" && value != "one") {
		return false, parser.ErrModel
	}
	if err := c.SectionCheckpoint(ctx, int64(len(value))+1, 0); err != nil {
		return false, err
	}
	return value == "one", nil
}

// ListSeparators exposes detached original separator facts under both controls.
func (c RuleContext) ListSeparators(ctx context.Context) (out []parser.ListSeparatorFact, err error) {
	if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	return c.document.ListSeparators(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// ListMarkerSpaces borrows the admitted positive integer without an option-map
// clone or work proportional to the configured value.
func (c RuleContext) ListMarkerSpaces(ctx context.Context) (int, error) {
	if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return 0, err
	}
	spaces, ok := c.options["spaces"].(int)
	if !ok || spaces <= 0 {
		return 0, parser.ErrModel
	}
	if err := c.SectionCheckpoint(ctx, 0, 0); err != nil {
		return 0, err
	}
	return spaces, nil
}

// ListSpacing exposes detached source-body boundaries under both controls.
func (c RuleContext) ListSpacing(ctx context.Context) (out []parser.ListSpacingFact, err error) {
	if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	return c.document.ListSpacing(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
