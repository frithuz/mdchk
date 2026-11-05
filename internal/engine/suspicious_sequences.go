package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// SuspiciousSequences returns suspicious sequence occurrence spans under both
// caller and originating controls. Option copying uses the original finite allowance.
func (c RuleContext) SuspiciousSequences(ctx context.Context) (out []parser.Span, err error) {
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
	values, ok := c.options["sequences"].([]string)
	if !ok || len(c.options) != 1 {
		return nil, parser.ErrModel
	}
	if err = c.SectionCheckpoint(ctx, int64(len(values))+1, int64(len(values))); err != nil {
		return nil, err
	}
	sequences := make([]string, len(values))
	for i, value := range values {
		if err = c.SectionCheckpoint(ctx, 1, 0); err != nil {
			return nil, err
		}
		sequences[i] = value
	}
	return c.document.SuspiciousSequences(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), sequences)
}
