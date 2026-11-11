// Rule fact facades for the formatting rules.

package engine

import (
	"context"
	"errors"
	"math"

	"github.com/frithuz/mdchk/internal/parser"
)

// ThematicStyle borrows one admitted scalar style and projects detached original
// marker envelopes under both caller and originating operation controls.
// Serves MD035.
func (c RuleContext) ThematicStyle(ctx context.Context) (out []parser.Span, err error) {
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
	style, ok := c.options["style"].(string)
	if !ok || len(c.options) != 1 {
		return nil, parser.ErrModel
	}
	return c.document.ThematicStyle(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), style)
}

// DelimiterStyle returns detached original delimiter facts without granting
// parse, metadata or correction authority. Both caller and origin remain binding.
// Serves MD037, MD049 and MD050.
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

// CodeSpanSpaces returns detached original code-span SPACE ranges without granting
// parse, metadata or correction authority. Both caller and origin remain binding.
// Serves MD038.
func (c RuleContext) CodeSpanSpaces(ctx context.Context) (out []parser.Span, err error) {
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
		return nil, parser.ErrModel
	}
	return c.document.CodeSpanSpaces(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// ConfiguredNames returns manual original occurrence spans under both caller
// and originating controls. Option copying uses the original finite allowance.
// Serves MD044, MD061, MD083 and MD084.
func (c RuleContext) ConfiguredNames(ctx context.Context) (out []parser.Span, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	if err = c.SectionCheckpoint(ctx, 3, 0); err != nil {
		return nil, err
	}
	values, ok := c.options["names"].([]string)
	if !ok || len(c.options) != 2 {
		return nil, parser.ErrModel
	}
	code, ok := c.options["code_blocks"].(bool)
	if !ok {
		return nil, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, int64(len(values))+1, int64(len(values))); err != nil {
		return nil, err
	}
	names := make([]string, len(values))
	for i, value := range values {
		if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
			return nil, err
		}
		names[i] = value
	}
	return c.document.ConfiguredNames(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), names, code)
}

// DelimiterStyleOption reads the exact resolved italic/bold style schema under
// the original allowance. Empty options remain valid only for the MD037 facade;
// a style consumer must receive its default from configuration preflight.
// Serves MD049 and MD050.
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

// ParagraphEmphasisOptions reads only the admitted MD081 scalar domain, without
// copying options or widening any other rule's option contract.
// Serves MD081.
func (c RuleContext) ParagraphEmphasisOptions(ctx context.Context) (ratio float64, minimum int, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return 0, 0, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			ratio = 0
			minimum = 0
		}
	}()
	if err = c.SectionCheckpoint(ctx, 16, 0); err != nil {
		return 0, 0, err
	}
	if len(c.options) != 2 {
		return 0, 0, parser.ErrModel
	}
	switch v := c.options["maximum_ratio"].(type) {
	case int:
		ratio = float64(v)
	case float64:
		ratio = v
	default:
		return 0, 0, parser.ErrModel
	}
	minimum, ok := c.options["minimum_characters"].(int)
	if !ok || minimum < 1 || math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 || ratio > 1 {
		return 0, 0, parser.ErrModel
	}
	return ratio, minimum, nil
}

// ParagraphEmphasis returns detached completed-original paragraph counts. The
// original options/profile/index/operation and both cancellation owners bind it.
// Serves MD081.
func (c RuleContext) ParagraphEmphasis(ctx context.Context) (out []parser.ParagraphEmphasisFact, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = nil
		}
	}()
	if _, _, err = c.ParagraphEmphasisOptions(ctx); err != nil {
		return nil, err
	}
	return c.document.ParagraphEmphasis(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}

// ASCIIPunctuationStyle admits only MD088's resolved fixed scalar option.
// Serves MD088.
func (c RuleContext) ASCIIPunctuationStyle(ctx context.Context) (style string, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return "", err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			style = ""
		}
	}()
	if err = c.SectionCheckpoint(ctx, 16, 0); err != nil {
		return "", err
	}
	style, ok := c.options["style"].(string)
	if !ok || len(c.options) != 1 || style != "ascii" {
		return "", parser.ErrModel
	}
	return style, nil
}

// ASCIIPunctuation retains original source/profile/options/index/budget authority
// and both caller and originating operation controls, including final publication.
// Serves MD088.
func (c RuleContext) ASCIIPunctuation(ctx context.Context) (out []parser.ASCIIPunctuationFact, err error) {
	if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 1, 0))
		if err != nil {
			out = nil
		}
	}()
	if _, err = c.ASCIIPunctuationStyle(ctx); err != nil {
		return nil, err
	}
	return c.document.ASCIIPunctuation(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
}
