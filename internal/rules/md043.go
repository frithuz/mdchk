package rules

import (
	"context"
	"errors"
	"math"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

const (
	outlineDone byte = iota
	outlineExact
	outlineStarAdvance
	outlineStarConsume
	outlineSubstitute
	outlineMissing
	outlineUnexpected
)

func requiredHeadingOutline(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	pattern, err := c.HeadingOutline(ctx)
	if err != nil || len(pattern) == 0 {
		return engine.RuleOutcome{Err: err}
	}
	headings, err := c.Headings(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	span, err := compareHeadingOutline(ctx, c, pattern, headings)
	if err != nil || !span.Present() {
		return engine.RuleOutcome{Err: err}
	}
	if err := c.HeadingOutlineCheckpoint(ctx, 1, 1); err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return engine.RuleOutcome{Findings: []engine.Finding{{Span: span, Message: "Headings must match the configured outline."}}}
}

// outlineDimensions checks all product and reservation arithmetic before either
// workspace allocation. Ordinary quotas are applied separately before make.
func outlineDimensions(pattern, headings int) (int, int, bool) {
	maximum := int(^uint(0) >> 1)
	if pattern < 0 || headings < 0 || pattern == maximum || headings == maximum {
		return 0, 0, false
	}
	rows, columns := pattern+1, headings+1
	if rows > maximum/columns {
		return 0, 0, false
	}
	cells := rows * columns
	if int64(cells) > math.MaxInt64/3 || cells > maximum/8 {
		return 0, 0, false
	}
	return columns, cells, true
}

func compareHeadingOutline(ctx context.Context, c engine.RuleContext, pattern []engine.HeadingOutlineEntry, headings []parser.HeadingFact) (out parser.Span, err error) {
	defer func() {
		err = errors.Join(err, c.HeadingOutlineCheckpoint(ctx, 0, 0))
		if err != nil {
			out = parser.Span{}
		}
	}()
	columns, cells, ok := outlineDimensions(len(pattern), len(headings))
	if !ok {
		// An unrepresentable workspace exhausts the original supported allowance;
		// it does not introduce an option cap or attempt an overflowing allocation.
		return parser.Span{}, c.HeadingOutlineCheckpoint(ctx, math.MaxInt64, 0)
	}
	if err := c.HeadingOutlineCheckpoint(ctx, 3*int64(cells), 2*int64(cells)); err != nil {
		return parser.Span{}, err
	}
	cost := make([]int, cells)
	action := make([]byte, cells)
	for i := len(pattern); i >= 0; i-- {
		for j := len(headings); j >= 0; j-- {
			if err := c.HeadingOutlineCheckpoint(ctx, 8, 0); err != nil {
				return parser.Span{}, err
			}
			at := i*columns + j
			if i == len(pattern) {
				cost[at] = len(headings) - j
				if j != len(headings) {
					action[at] = outlineUnexpected
				}
				continue
			}
			if pattern[i].Star {
				cost[at], action[at] = cost[at+columns], outlineStarAdvance
				if j < len(headings) && cost[at+1] < cost[at] {
					cost[at], action[at] = cost[at+1], outlineStarConsume
				}
				continue
			}
			if j == len(headings) {
				cost[at], action[at] = 1+cost[at+columns], outlineMissing
				continue
			}
			equal := false
			if pattern[i].Level == headings[j].Level {
				equal, err = outlineTextEqual(ctx, c, pattern[i].Text, headings[j].Visible)
				if err != nil {
					return parser.Span{}, err
				}
			}
			cost[at], action[at] = 1+cost[at+columns+1], outlineSubstitute
			if equal {
				cost[at], action[at] = cost[at+columns+1], outlineExact
			}
			if missing := 1 + cost[at+columns]; missing < cost[at] {
				cost[at], action[at] = missing, outlineMissing
			}
			if unexpected := 1 + cost[at+1]; unexpected < cost[at] {
				cost[at], action[at] = unexpected, outlineUnexpected
			}
		}
	}
	if cost[0] == 0 {
		return parser.Span{}, nil
	}
	for i, j := 0, 0; ; {
		if err := c.HeadingOutlineCheckpoint(ctx, 4, 0); err != nil {
			return parser.Span{}, err
		}
		switch action[i*columns+j] {
		case outlineExact:
			i++
			j++
		case outlineStarAdvance:
			i++
		case outlineStarConsume:
			j++
		case outlineSubstitute, outlineUnexpected:
			return headings[j].Label, nil
		case outlineMissing:
			point := c.Source().Len()
			if j < len(headings) {
				point = headings[j].Envelope.Start()
			}
			return c.Source().Span(point, point)
		default:
			return parser.Span{}, parser.ErrModel
		}
	}
}

func outlineTextEqual(ctx context.Context, c engine.RuleContext, left, right string) (bool, error) {
	if err := c.HeadingOutlineCheckpoint(ctx, 1, 0); err != nil {
		return false, err
	}
	if len(left) != len(right) {
		return false, nil
	}
	for start := 0; start < len(left); start += 256 {
		end := start + min(256, len(left)-start)
		if err := c.HeadingOutlineCheckpoint(ctx, 2*int64(end-start)+1, 0); err != nil {
			return false, err
		}
		if left[start:end] != right[start:end] {
			return false, nil
		}
	}
	return true, nil
}
