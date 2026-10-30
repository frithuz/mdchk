package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
	"math"
)

// ParagraphEmphasisOptions reads only the admitted MD081 scalar domain, without
// copying options or widening any other rule's option contract.
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
