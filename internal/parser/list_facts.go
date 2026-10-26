package parser

import "context"

// ListItemFact retains actual group/item ancestry and first-line source facts.
// Values contain no mutable syntax handles. ParentItem is absent at the root.
// Ordered items retain their own ordinal and delimiter, not the group's start.
type ListItemFact struct {
	Group, Item, ParentItem, Marker, Prefix Span
	Ordered                                 bool
	Bullet                                  Optional[byte]
	Start                                   Optional[int]
	Delimiter                               Optional[byte]
	MarkerWidth, Padding, ContentIndent     int
}

// ListItems borrows complete production syntax and its original allowance.
// Items are in original source preorder, across groups and container nesting.
// Failure withholds the complete dependent inventory.
func (d *Document) ListItems(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) ([]ListItemFact, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	type frame struct {
		node          *Node
		next          int
		group, parent Span
	}
	stack := make([]frame, 1)
	stack[0].node = d.root
	var out []ListItemFact
	for len(stack) != 0 {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		f := &stack[len(stack)-1]
		if f.next == len(f.node.spec.Children) {
			stack = stack[:len(stack)-1]
			continue
		}
		n := f.node.spec.Children[f.next]
		f.next++
		group, parent := f.group, f.parent
		if n.Kind() == List {
			group = n.Span()
		}
		if n.Kind() == ListItem {
			p := n.spec.Properties.List
			if p == nil || !group.Present() || !p.MarkerWidth.Present || !p.Padding.Present || !p.ContentIndent.Present {
				return nil, ErrModel
			}
			var marker, prefix Span
			for _, m := range n.spec.Markers {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return nil, err
				}
				switch m.Role {
				case "item-marker":
					marker = m.Source
				case "item-prefix":
					prefix = m.Source
				default:
				}
			}
			if !marker.Present() || !prefix.Present() {
				return nil, ErrModel
			}
			if err := openingHashGrowth(ctx, budget, len(out), cap(out)); err != nil {
				return nil, err
			}
			if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
				return nil, err
			}
			out = append(out, ListItemFact{Group: group, Item: n.Span(), ParentItem: parent, Marker: marker, Prefix: prefix, Ordered: p.Ordered, Bullet: p.Bullet, Start: p.Start, Delimiter: p.Delimiter, MarkerWidth: p.MarkerWidth.Value, Padding: p.Padding.Value, ContentIndent: p.ContentIndent.Value})
			parent = n.Span()
		}
		if len(n.spec.Children) != 0 && (n.Kind() == BlockQuote || n.Kind() == List || n.Kind() == ListItem) {
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
			stack = append(stack, frame{node: n, group: group, parent: parent})
		}
	}
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
