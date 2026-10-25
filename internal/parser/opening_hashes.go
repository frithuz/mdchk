package parser

import (
	"context"
	"errors"
)

// retainOpeningHashRun uses the actual container cursor before normalization.
// It retains only bounded lexical candidates; final block and inline ownership
// are settled separately. A seventh hash rejects the whole maximal run.
func (p *blockParser) retainOpeningHashRun(runs []Span, line blockLine) []Span {
	meterWork(p.ctx, 1)
	if line.indent >= 4 || line.first != '#' {
		return runs
	}
	end := line.contentStart
	text := p.index.source.content
	for end < line.end && text[end] == '#' {
		meterWork(p.ctx, 1)
		end++
		if end-line.contentStart > 6 {
			return runs
		}
	}
	if end == line.end || text[end] == ' ' || text[end] == '\t' {
		return runs
	}
	return meteredAppend(p.ctx, runs, p.index.span(line.contentStart, end))
}

// OpeningHashCandidates returns zero-width missing-separator points after
// literal runs of one through six opening hashes. Only parser-retained physical
// lines finally owned by ordinary paragraphs, with fewer than four structural
// indentation columns, participate. Valid headings, tables and definitions have
// other ownership. Code, raw HTML/literal regions and link destination/title
// syntax exclude overlapping runs, even when the point itself is at a boundary.
// Collection requires the original completed parse and operation allowance;
// cancellation or a quota failure publishes no partial inventory.
func (d *Document) OpeningHashCandidates(ctx context.Context, budget *OperationBudget) ([]Span, error) {
	if ctx == nil || budget == nil || d == nil || !d.parsedComplete || d.commentOperation != budget.identity {
		return nil, errors.New("opening hashes require a completed parse in this operation")
	}
	runs := d.openingHashRuns
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
			point, _ := d.Source().Span(run.End(), run.End())
			if err := openingHashGrowth(ctx, budget, len(out), cap(out)); err != nil {
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

// Record reservations precede append; growth additionally copies the old prefix.
func openingHashGrowth(ctx context.Context, budget *OperationBudget, length, capacity int) error {
	if length == capacity {
		return headingFactCharge(ctx, budget, int64(length), 0)
	}
	return nil
}

// protectedHashRuns filters source-ordered, disjoint runs against actual opaque
// ownership in one tree traversal, including active raw-text regions.
func (d *Document) protectedHashRuns(ctx context.Context, budget *OperationBudget, runs []Span) ([]bool, error) {
	if err := headingFactCharge(ctx, budget, int64(len(runs))+1, int64(len(runs))); err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, nil
	}
	excluded := make([]bool, len(runs))
	protect := func(span Span) error {
		first, last := 0, len(runs)
		for first < last {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return err
			}
			middle := first + (last-first)/2
			if runs[middle].End() > span.Start() {
				last = middle
			} else {
				first = middle + 1
			}
		}
		for i := first; i < len(runs) && runs[i].Start() < span.End(); i++ {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return err
			}
			excluded[i] = true
		}
		return nil
	}
	if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
		return nil, err
	}
	stack := []*Node{d.root}
	raw, rawStart := "", 0
	for len(stack) != 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count := int64(len(n.spec.Children) + len(n.htmlTail) + 1)
		if err := headingFactCharge(ctx, budget, count, count); err != nil {
			return nil, err
		}
		if n.rawTag != "" {
			if raw == "" && !n.rawClosing {
				raw, rawStart = n.rawTag, n.Span().Start()
			} else if raw == n.rawTag && n.rawClosing {
				span, _ := d.Source().Span(rawStart, n.Span().End())
				if err := protect(span); err != nil {
					return nil, err
				}
				raw = ""
			}
		}
		switch n.Kind() {
		case CodeBlock, CodeSpan, HTMLBlock, InlineHTML:
			if err := protect(n.Span()); err != nil {
				return nil, err
			}
		default:
		}
		if link := n.spec.Properties.Link; link != nil {
			values := [2]*Value{&link.Destination, link.Title}
			for _, value := range values {
				if value == nil {
					continue
				}
				for _, part := range value.Segments {
					if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
						return nil, err
					}
					if part.Source.Present() {
						if err := protect(part.Source); err != nil {
							return nil, err
						}
					}
				}
			}
		}
		for i := len(n.htmlTail) - 1; i >= 0; i-- {
			if err := openingHashGrowth(ctx, budget, len(stack), cap(stack)); err != nil {
				return nil, err
			}
			stack = append(stack, n.htmlTail[i])
		}
		for i := len(n.spec.Children) - 1; i >= 0; i-- {
			if err := openingHashGrowth(ctx, budget, len(stack), cap(stack)); err != nil {
				return nil, err
			}
			stack = append(stack, n.spec.Children[i])
		}
	}
	if raw != "" {
		span, _ := d.Source().Span(rawStart, d.Source().Len())
		if err := protect(span); err != nil {
			return nil, err
		}
	}
	return excluded, nil
}
