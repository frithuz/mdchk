package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// ASCIIPunctuationStyle admits only MD088's resolved fixed scalar option.
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
