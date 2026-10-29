package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// DelimiterStyle returns detached original delimiter facts without granting
// parse, metadata or correction authority. Both caller and origin remain binding.
func (c RuleContext) DelimiterStyle(ctx context.Context) (out *parser.DelimiterInventory, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	if len(c.options) != 0 {
		if _, err = c.DelimiterStyleOption(ctx); err != nil {
			return nil, err
		}
	}
	return c.document.DelimiterStyle(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// DelimiterStyleOption reads the exact resolved italic/bold style schema under
// the original allowance. Empty options remain valid only for the MD037 facade;
// a style consumer must receive its default from configuration preflight.
func (c RuleContext) DelimiterStyleOption(ctx context.Context) (style string, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return "", err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			style = ""
		}
	}()
	// Bound the map lookup, shape/type tests and all three string comparisons
	// before reading options. Length checks bound every compared string to ten
	// bytes, including invalid inputs; no option copy or alternate context.
	if err = c.SectionCheckpoint(ctx, 40, 0); err != nil {
		return "", err
	}
	value, ok := c.options["style"].(string)
	if !ok || len(c.options) != 1 || len(value) > 10 {
		return "", parser.ErrModel
	}
	if value != "consistent" && value != "asterisk" && value != "underscore" {
		return "", parser.ErrModel
	}
	return value, nil
}
