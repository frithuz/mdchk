// Facts for MD004, MD005, MD007, MD029 and MD030.

package parser

import (
	"context"
	"math/bits"
	"sort"
)

// ListItemFact retains actual group/item ancestry and first-line source facts.
// Values contain no mutable syntax handles. ParentItem is absent at the root.
// Ordered items retain their own ordinal and delimiter, not the group's start.
// Content runs from the first child block's start to the last one's end and is
// absent for an item without children.
type ListItemFact struct {
	Group, Item, ParentItem, Marker, Prefix, Content Span
	Ordered                                          bool
	Bullet                                           Optional[byte]
	Start                                            Optional[int]
	Delimiter                                        Optional[byte]
	MarkerWidth, Padding, ContentIndent              int
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
			var content Span
			if children := n.spec.Children; len(children) != 0 {
				first, last := children[0].Span(), children[len(children)-1].Span()
				if !first.Present() || !last.Present() || first.Start() > last.End() {
					return nil, ErrModel
				}
				content = Span{source: first.source, start: first.Start(), end: last.End(), present: true}
			}
			if err := openingHashGrowth(ctx, budget, len(out), cap(out)); err != nil {
				return nil, err
			}
			if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
				return nil, err
			}
			out = append(out, ListItemFact{Group: group, Item: n.Span(), ParentItem: parent, Marker: marker, Prefix: prefix, Content: content, Ordered: p.Ordered, Bullet: p.Bullet, Start: p.Start, Delimiter: p.Delimiter, MarkerWidth: p.MarkerWidth.Value, Padding: p.Padding.Value, ContentIndent: p.ContentIndent.Value})
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

// ListSeparatorFact describes the original owned first-line separator. Nonempty
// refers to source syntax on that physical line, independently of descendants.
// Values are detached from mutable syntax and never confer edit ownership.
//
// OneLine reports that every child block ends on the marker's physical line and
// that no list item nests inside this one, not even on that line. CodeFallback
// reports whitespace left after the separator: the content starts as indented
// code. Column is the tab-expanded column of the separator start. Following is the
// column of the first non-whitespace byte on the first nonblank physical line
// after the item, or -1 when there is none. Quoted reports a '>' before the item
// prefix on the marker line, where Following can't see container prefixes.
type ListSeparatorFact struct {
	Marker, Separator                                Span
	Padding, Column, Following                       int
	FirstLineNonempty, OneLine, CodeFallback, Quoted bool
}

// ListSeparators derives facts from the actual ListItems inventory exactly once,
// under its completed original production witness and allowance. Physical line
// lookup uses the retained index, without decoding or copying source prefixes.
func (d *Document) ListSeparators(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) ([]ListSeparatorFact, error) {
	items, err := d.ListItems(ctx, budget, index, options)
	if err != nil {
		return nil, err
	}
	// Reserve storage and work before construction, even for an empty inventory.
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, int64(len(items))+1, int64(len(items))); err != nil {
		return nil, err
	}
	out := make([]ListSeparatorFact, 0, len(items))
	previous := -1
	text := index.source.content
	for i, item := range items {
		// Search performs at most bits.Len(len(lines)) comparisons. Reserve them
		// before touching the original index; no Unicode prefix scan is necessary.
		if err := headingFactCharge(ctx, budget, int64(bits.Len(uint(len(index.lines))))+1, 0); err != nil {
			return nil, err
		}
		marker, prefix := item.Marker, item.Prefix
		if !marker.Present() || !prefix.Present() || marker.SourceToken() != index.source.Token() || prefix.SourceToken() != index.source.Token() || marker.Start() < previous || prefix.Start() > marker.Start() || marker.Start() >= marker.End() || marker.End() > prefix.End() || item.Padding <= 0 {
			return nil, ErrModel
		}
		line := sort.Search(len(index.lines), func(i int) bool { return index.lines[i].end >= marker.Start() })
		if line == len(index.lines) || prefix.Start() < index.lines[line].start || prefix.End() > index.lines[line].end {
			return nil, ErrModel
		}
		nonempty := prefix.End() < index.lines[line].end
		if nonempty && marker.End() == prefix.End() {
			return nil, ErrModel
		}
		// Logical records are distinct from their reserved backing storage.
		if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
			return nil, err
		}
		separator := Span{source: index.source, start: marker.End(), end: prefix.End(), present: true}
		fact := ListSeparatorFact{Marker: marker, Separator: separator, Padding: item.Padding, FirstLineNonempty: nonempty, Following: -1}
		if nonempty {
			// The marker line, the item's later lines and the first line after it
			// are each scanned once; reserve them before reading.
			if err := headingFactCharge(ctx, budget, int64(item.Item.End()-index.lines[line].start)+2, 0); err != nil {
				return nil, err
			}
			child := i+1 < len(items) && items[i+1].ParentItem == item.Item
			fact.OneLine = !child && (!item.Content.Present() || item.Content.End() <= index.lines[line].next)
			fact.CodeFallback = text[prefix.End()] == ' ' || text[prefix.End()] == '\t'
			fact.Column = sourceColumn(text, index.lines[line].start, marker.End())
			for at := index.lines[line].start; at < prefix.Start(); at++ {
				fact.Quoted = fact.Quoted || text[at] == '>'
			}
			for next := line + 1; next < len(index.lines); next++ {
				l := index.lines[next]
				if l.start < item.Item.End() {
					continue
				}
				if err := headingFactCharge(ctx, budget, int64(l.end-l.start)+1, 0); err != nil {
					return nil, err
				}
				at := l.start
				for at < l.end && (text[at] == ' ' || text[at] == '\t') {
					at++
				}
				if at < l.end {
					fact.Following = sourceColumn(text, l.start, at)
					break
				}
			}
		}
		out = append(out, fact)
		previous = marker.End()
	}
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}

// sourceColumn expands tabs to four-column stops from the physical line start.
func sourceColumn(text string, start, at int) int {
	column := 0
	for i := start; i < at; i++ {
		if text[i] == '\t' {
			column += 4 - column%4
		} else {
			column++
		}
	}
	return column
}
