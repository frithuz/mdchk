package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// HeadingCapitalizationOptions returns owned name-list storage after reserving
// it in the actual source operation. String bytes remain immutable borrows.
func (c RuleContext) HeadingCapitalizationOptions(ctx context.Context) (style string, names []string, err error) {
	if err := c.HeadingCapitalizationCheckpoint(ctx, 1, 0); err != nil {
		return "", nil, err
	}
	defer func() {
		err = errors.Join(err, c.HeadingCapitalizationCheckpoint(ctx, 0, 0))
		if err != nil {
			style = ""
			names = nil
		}
	}()
	style, ok := c.options["style"].(string)
	if !ok || (style != "sentence" && style != "title") {
		return "", nil, parser.ErrModel
	}
	values, ok := c.options["proper_names"].([]string)
	if !ok {
		return "", nil, parser.ErrModel
	}
	if err := c.HeadingCapitalizationCheckpoint(ctx, int64(len(values))+1, int64(len(values))); err != nil {
		return "", nil, err
	}
	names = make([]string, len(values))
	for i, value := range values {
		if err := c.HeadingCapitalizationCheckpoint(ctx, 1, 0); err != nil {
			return "", nil, err
		}
		names[i] = value
	}
	return style, names, nil
}

// HeadingScalars retains shared Visible semantics, code ownership and atomic
// source provenance under both caller and originating operation controls.
func (c RuleContext) HeadingScalars(ctx context.Context) (out [][]parser.HeadingScalar, err error) {
	if err := c.HeadingCapitalizationCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.HeadingCapitalizationCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	return c.document.HeadingScalars(ruleOperation{ctx, c.operation}, c.budget)
}

// HeadingCapitalizationCheckpoint reserves derived work/records without exposing
// an allowance or accepting a foreign parse, index or originating operation.
func (c RuleContext) HeadingCapitalizationCheckpoint(ctx context.Context, work, nodes int64) error {
	if ctx == nil || c.operation == nil || c.document == nil || c.budget == nil || !c.syntaxReliable || c.document.Source().Token() != c.Source().Token() {
		return parser.ErrModel
	}
	return c.document.HeadingCapitalizationCheckpoint(ruleOperation{ctx, c.operation}, c.budget, c.index, work, nodes)
}
