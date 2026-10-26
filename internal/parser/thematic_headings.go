package parser

import "context"

// ThematicBreaksBeforeHeadings returns owned marker spans for direct adjacent
// block siblings. Each container retains its own sequence; blanks and removed
// references are absent, while actual comment and container blocks interrupt.
func (d *Document) ThematicBreaksBeforeHeadings(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) ([]Span, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	type frame struct {
		node *Node
		next int
	}
	stack := make([]frame, 1)
	stack[0].node = d.root
	var out []Span
	for len(stack) != 0 {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		f := &stack[len(stack)-1]
		if f.next == len(f.node.spec.Children) {
			stack = stack[:len(stack)-1]
			continue
		}
		i := f.next
		f.next++
		n := f.node.spec.Children[i]
		body := f.node.Kind() == DocumentKind || f.node.Kind() == BlockQuote || f.node.Kind() == ListItem
		if body && n.Kind() == ThematicBreak && i+1 < len(f.node.spec.Children) && f.node.spec.Children[i+1].Kind() == Heading {
			var first, last Span
			for _, marker := range n.spec.Markers {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return nil, err
				}
				if marker.Role == "thematic-marker" {
					if !first.Present() {
						first = marker.Source
					}
					last = marker.Source
				}
			}
			if !first.Present() || !last.Present() {
				return nil, ErrModel
			}
			if err := openingHashGrowth(ctx, budget, len(out), cap(out)); err != nil {
				return nil, err
			}
			if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
				return nil, err
			}
			span, err := d.Source().Span(first.Start(), last.End())
			if err != nil {
				return nil, err
			}
			out = append(out, span)
		}
		if n.Kind() == BlockQuote || n.Kind() == List || n.Kind() == ListItem {
			if len(stack) == cap(stack) {
				if cap(stack) > int(^uint(0)>>1)/2 {
					return nil, ErrModel
				}
				capacity := 2 * cap(stack)
				if err := headingFactCharge(ctx, budget, int64(len(stack))+1, int64(capacity)); err != nil {
					return nil, err
				}
				next := make([]frame, len(stack), capacity)
				copy(next, stack)
				stack = next
			}
			stack = append(stack, frame{node: n})
		}
	}
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
