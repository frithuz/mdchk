package parser

import (
	"context"
	"errors"
)

// ClosingHashCandidates returns original zero-width points before maximal raw
// terminal hash runs in actual open ATX headings. Odd escape parity excludes the
// whole run conservatively; genuine closers and protected runs are never targets.
// Collection shares the completed parse's operation and publishes no partial list.
func (d *Document) ClosingHashCandidates(ctx context.Context, budget *OperationBudget) ([]Span, error) {
	if ctx == nil || budget == nil || d == nil || !d.parsedComplete || d.commentOperation != budget.identity {
		return nil, errors.New("closing hashes require a completed parse in this operation")
	}
	var runs []Span
	if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
		return nil, err
	}
	stack := []*Node{d.root}
	for len(stack) != 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if err := headingFactCharge(ctx, budget, int64(len(n.spec.Children))+1, int64(len(n.spec.Children))); err != nil {
			return nil, err
		}
		if n.Kind() == Heading {
			h := n.spec.Properties.Heading
			if h == nil {
				return nil, ErrModel
			}
			closed := false
			for _, marker := range n.spec.Markers {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return nil, err
				}
				closed = closed || marker.Role == "closing"
			}
			if h.Style != "atx" || closed || len(n.spec.Content) == 0 {
				continue
			}
			// Content may be split by NUL normalization. Its original first/last
			// contributions bound raw bytes independently of semantic lengths.
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return nil, err
			}
			start := n.spec.Content[0].Source.Start()
			end := n.spec.Content[len(n.spec.Content)-1].Source.End()
			text := d.Source().content
			if start < 0 || end < start || end > len(text) {
				return nil, ErrModel
			}
			first := end
			for first > start {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return nil, err
				}
				if text[first-1] != '#' {
					break
				}
				first--
			}
			if first == end {
				continue
			}
			slash := first
			for slash > start {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return nil, err
				}
				if text[slash-1] != '\\' {
					break
				}
				slash--
			}
			if (first-slash)%2 != 0 {
				continue
			}
			if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
				return nil, err
			}
			if err := openingHashGrowth(ctx, budget, len(runs), cap(runs)); err != nil {
				return nil, err
			}
			run, err := d.Source().Span(first, end)
			if err != nil {
				return nil, err
			}
			runs = append(runs, run)
			continue
		}
		for i := len(n.spec.Children) - 1; i >= 0; i-- {
			if err := openingHashGrowth(ctx, budget, len(stack), cap(stack)); err != nil {
				return nil, err
			}
			stack = append(stack, n.spec.Children[i])
		}
	}
	excluded, err := d.protectedHashRuns(ctx, budget, runs)
	if err != nil {
		return nil, err
	}
	var out []Span
	for i, run := range runs {
		if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
			return nil, err
		}
		if !excluded[i] {
			if err := openingHashGrowth(ctx, budget, len(out), cap(out)); err != nil {
				return nil, err
			}
			point, err := d.Source().Span(run.Start(), run.Start())
			if err != nil {
				return nil, err
			}
			out = append(out, point)
		}
	}
	if err := headingFactCharge(ctx, budget, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
