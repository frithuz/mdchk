package parser

import "sort"

// These summaries are private parse-time values, not model properties. A blank
// at a list's trailing boundary can affect its enclosing item; arbitrary blanks
// inside its descendants cannot. Leaves and quotes never propagate that fact.
type parsedBlock struct {
	node          *Node
	loose         bool // an item has blank-separated direct blocks
	trailingBlank bool
	partial       bool
}

func blockNodes(blocks []parsedBlock) []*Node {
	if len(blocks) == 0 {
		return nil
	}
	nodes := make([]*Node, len(blocks))
	for i := range blocks {
		nodes[i] = blocks[i].node
	}
	return nodes
}

// groupLists consumes private child storage after its parent has closed. Each
// run is frozen once, so a growing list never repeatedly copies its prefix.
// Descendants already carry their own local boundary summaries.
func (p *blockParser) groupLists(children []parsedBlock) ([]parsedBlock, error) {
	grouped := children[:0]
	for i := 0; i < len(children); {
		first := children[i]
		if first.node.Kind() != ListItem {
			grouped = append(grouped, first)
			i++
			continue
		}
		props := first.node.spec.Properties.List
		end := i + 1
		for end < len(children) && compatibleItems(first.node, children[end].node) {
			end++
		}
		loose, partial := false, false
		for j := i; j < end; j++ {
			item := children[j]
			partial = partial || item.partial
			loose = loose || item.loose
			if j+1 < end {
				loose = loose || item.trailingBlank || p.blankItemGap(item.node.Span().End(), children[j+1].node.Span().Start())
			}
		}
		listProps := &ListProperties{Ordered: props.Ordered, Start: props.Start, Bullet: props.Bullet, Delimiter: props.Delimiter}
		if !partial {
			listProps.Tight = Some(!loose)
		}
		last := children[end-1]
		list, err := NewNode(NodeSpec{Kind: List,
			Envelope: p.index.span(first.node.Span().Start(), last.node.Span().End()),
			Children: blockNodes(children[i:end]), Properties: Properties{List: listProps}})
		if err != nil {
			return nil, err
		}
		grouped = append(grouped, parsedBlock{node: list, trailingBlank: last.trailingBlank, partial: partial})
		i = end
	}
	return grouped, nil
}

func compatibleItems(a, b *Node) bool {
	if b.Kind() != ListItem {
		return false
	}
	x, y := a.spec.Properties.List, b.spec.Properties.List
	return x.Ordered == y.Ordered && x.Bullet == y.Bullet && x.Delimiter == y.Delimiter
}

// Empty items close before a second blank and cannot own its marker. Between
// established siblings at the same parent, a whole otherwise unowned physical
// line is a separating blank. Parent quote/item prefixes on the next item's
// own line do not count. No source bytes or descendant subtrees are rescanned.
func (p *blockParser) blankItemGap(end, start int) bool {
	i := sort.Search(len(p.index.lines), func(i int) bool { return p.index.lines[i].next > end })
	return i < len(p.index.lines) && p.index.lines[i].next <= start
}

// Match item-level blank evidence to direct children in source order. A blank
// enclosed by code/HTML/quote is internal. A nested list contributes only its
// final boundary, never its loose status. Leading/trailing blanks alone do not
// separate direct blocks; the trailing summary is consulted only if another
// sibling item (or a following direct block) actually exists.
func itemBlankBoundaries(item openBlock, children []parsedBlock) (loose, trailing bool) {
	child := 0
	for _, marker := range item.markers {
		if marker.Role != "item-blank" {
			continue
		}
		blank := marker.Source
		for child < len(children) && children[child].node.Span().End() <= blank.Start() {
			child++
		}
		before, after := child > 0, child < len(children)
		if after && children[child].node.Span().Start() <= blank.Start() {
			block := children[child]
			if block.node.Kind() != List || !block.trailingBlank || blank.End() != block.node.Span().End() {
				continue
			}
			before, after = true, child+1 < len(children)
		}
		loose = loose || before && after
		trailing = trailing || blank.End() == item.end
	}
	return loose, trailing
}
