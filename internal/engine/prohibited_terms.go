package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// ProhibitedTerms returns prohibited term occurrence spans under both caller
// and originating controls. Option copying uses the original finite allowance.
func (c RuleContext) ProhibitedTerms(ctx context.Context) (out []parser.Span, err error) {
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
	values, ok := c.options["terms"].([]string)
	if !ok || len(c.options) != 2 {
		return nil, parser.ErrModel
	}
	caseSensitive, ok := c.options["case_sensitive"].(bool)
	if !ok {
		return nil, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, int64(len(values))+1, int64(len(values))); err != nil {
		return nil, err
	}
	terms := make([]string, len(values))
	for i, value := range values {
		if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
			return nil, err
		}
		terms[i] = value
	}
	return c.document.ProhibitedTerms(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), terms, caseSensitive)
}
