package parser

import (
	"sort"
	"strings"
)

// delimiterFlanking uses CommonMark's character classes, not unicode.IsSpace.
// Callers supply logical source neighbors, before ordinary text decoding.
func delimiterFlanking(marker byte, before, after rune) (open, close bool) {
	bw, aw := IsUnicodeWhitespace(before), IsUnicodeWhitespace(after)
	bp, ap := IsUnicodePunctuation(before), IsUnicodePunctuation(after)
	left := !aw && (!ap || bw || bp)
	right := !bw && (!bp || aw || ap)
	if marker == '_' {
		return left && (!right || bp), right && (!left || ap)
	}
	return left, right
}

func delimitersMatch(open, close inlineDelimiter) bool {
	return open.marker == close.marker && open.canOpen && close.canClose &&
		(!(open.canClose || close.canOpen) || (open.original+close.original)%3 != 0 ||
			open.original%3 == 0 && close.original%3 == 0)
}

type emphasisPair struct {
	opening, closing, width int // logical raw offsets; opening is the consumed suffix
	marker                  byte
}

// emphasisPairs owns the private delimiter records. Stable source-order indexes
// remain valid search floors even when records are removed from the linked stack.
// Each failed-search class retires its searched prefix. Successful searches remove
// intervening records, so repeated incompatible candidates cannot rescan a suffix.
// bottom is a live delimiter index, or -1 for the whole stack. Processing and
// cleanup affect only its suffix. Retired records are never resurrected on a
// subsequent scoped call over the same completed scan.
func (s *inlineScanner) emphasisPairs(bottom int) ([]emphasisPair, error) {
	d := s.delimiters
	if !s.delimitersLinked {
		for i := range d {
			if interruption(s.ctx) != "" {
				return nil, nil
			}
			d[i].previous, d[i].next = i-1, i+1
		}
		s.delimiterHead = 0
		s.delimiterTail = len(d) - 1
		s.delimitersLinked = true
	}
	if bottom < -1 || bottom >= len(d) || bottom >= 0 && d[bottom].retired {
		return nil, ErrModel
	}
	var floors [2][3][2]int
	for m := range floors {
		for r := range floors[m] {
			for o := range floors[m][r] {
				floors[m][r][o] = bottom
			}
		}
	}
	remove := func(i int) {
		d[i].retired = true
		if d[i].previous >= 0 {
			d[d[i].previous].next = d[i].next
		} else {
			s.delimiterHead = d[i].next
		}
		if d[i].next < len(d) {
			d[d[i].next].previous = d[i].previous
		} else {
			s.delimiterTail = d[i].previous
		}
	}
	start := func() int {
		if bottom >= 0 {
			return d[bottom].next
		}
		return s.delimiterHead
	}
	var pairs []emphasisPair
	for current := start(); current < len(d); {
		if interruption(s.ctx) != "" {
			return nil, nil
		}
		closer := &d[current]
		if !closer.canClose {
			current = closer.next
			continue
		}
		marker, dual := 0, 0
		if closer.marker == '_' {
			marker = 1
		}
		if closer.canOpen {
			dual = 1
		}
		floor := &floors[marker][closer.original%3][dual]
		opener := closer.previous
		for opener > *floor {
			if interruption(s.ctx) != "" {
				return nil, nil
			}
			if delimitersMatch(d[opener], *closer) {
				break
			}
			opener = d[opener].previous
		}
		if opener <= *floor {
			*floor = max(*floor, closer.previous)
			next := closer.next
			if !closer.canOpen {
				remove(current)
			}
			current = next
			continue
		}
		open := &d[opener]
		width := 1
		if open.width >= 2 && closer.width >= 2 {
			width = 2
		}
		pair := emphasisPair{opening: open.offset + open.width - width, closing: closer.offset, width: width, marker: open.marker}
		pairs = append(pairs, pair)
		for i := open.next; i != current; {
			if interruption(s.ctx) != "" {
				return nil, nil
			}
			next := d[i].next
			remove(i)
			i = next
		}
		open.width -= width
		closer.width -= width
		closer.offset += width
		if open.width == 0 {
			remove(opener)
		}
		if closer.width == 0 {
			next := closer.next
			remove(current)
			current = next
		}
	}
	for i := start(); i < len(d); {
		if interruption(s.ctx) != "" {
			return nil, nil
		}
		next := d[i].next
		remove(i)
		i = next
	}
	return pairs, nil
}

// inlineNodes consumes immutable flat nodes in source order. A split only copies
// the consumed Text prefix, never the accumulated output or remaining suffix.
// Decoded contributions are indivisible; syntax boundaries occur in Exact bytes.
type inlineNodes struct {
	scanner             *inlineScanner
	nodes               []*Node
	index, part, within int
}

func (c *inlineNodes) take(end int) ([]*Node, error) {
	var out []*Node
	for c.index < len(c.nodes) {
		if interruption(c.scanner.ctx) != "" {
			return nil, nil
		}
		n := c.nodes[c.index]
		if n.spec.Kind != Text {
			if n.Span().Start() >= end {
				break
			}
			if n.Span().End() > end {
				return nil, ErrModel
			}
			out = append(out, n)
			c.index++
			continue
		}
		var parts []Segment
		var literal strings.Builder
		for c.part < len(n.spec.Content) {
			if interruption(c.scanner.ctx) != "" {
				return nil, nil
			}
			part := n.spec.Content[c.part]
			start := part.Source.start + c.within
			if start >= end {
				break
			}
			stop := min(end, part.Source.end)
			if start != part.Source.start || stop != part.Source.end {
				if part.Transformation != Exact {
					return nil, ErrModel
				}
				part.Semantic = Some(part.Semantic.Value[c.within : c.within+stop-start])
				part.Source.start, part.Source.end = start, stop
			}
			parts = append(parts, part)
			literal.WriteString(part.Semantic.Value)
			c.within += stop - start
			if stop == n.spec.Content[c.part].Source.end {
				c.part++
				c.within = 0
			}
		}
		if len(parts) != 0 {
			text, err := NewNode(NodeSpec{Kind: Text, Envelope: contentEnvelope(parts), Content: parts, Properties: Properties{Literal: Some(literal.String())}})
			if err != nil {
				return nil, err
			}
			out = append(out, text)
		}
		if c.part != len(n.spec.Content) {
			break
		}
		c.index++
		c.part = 0
	}
	return out, nil
}

// emphasisContent indexes the borrowed raw logical segments once. Each payload
// copies only its own output segments, without splitting it at nested markers or
// rescanning earlier source. Overlapping parent/child payloads remain explicit.
type emphasisContent struct {
	scanner *inlineScanner
	parts   []Segment
	starts  []int
}

func (c emphasisContent) slice(start, end int) ([]Segment, error) {
	i := sort.Search(len(c.parts), func(i int) bool { return c.starts[i+1] > start })
	var out []Segment
	for start < end {
		if interruption(c.scanner.ctx) != "" {
			return nil, nil
		}
		if i >= len(c.parts) {
			return nil, ErrModel
		}
		part := c.parts[i]
		a, b := start-c.starts[i], min(end, c.starts[i+1])-c.starts[i]
		if part.Transformation == Exact {
			part.Source.start += a
			part.Source.end = part.Source.start + b - a
			part.Semantic = Some(part.Semantic.Value[a:b])
		} else if a != 0 || b != len(part.Semantic.Value) {
			return nil, ErrModel
		}
		out = append(out, part)
		start = c.starts[i] + b
		i++
	}
	return out, nil
}

func (s *inlineScanner) emphasis(parts []Segment, flat []*Node) ([]*Node, error) {
	return s.emphasisAbove(parts, flat, -1, 0)
}

func (s *inlineScanner) emphasisAbove(parts []Segment, flat []*Node, bottom, start int) ([]*Node, error) {
	pairs, err := s.emphasisPairs(bottom)
	if err != nil || len(pairs) == 0 || interruption(s.ctx) != "" {
		return flat, err
	}
	// Pairing emits children before their parents, in closing-source order.
	// Each new parent takes a suffix of the current forest exactly once. Copy
	// those direct child indexes before reusing the forest's mutable storage.
	type group struct {
		pair     emphasisPair
		children []int
	}
	groups := make([]group, 0, len(pairs))
	var forest []int
	for _, pair := range pairs {
		if interruption(s.ctx) != "" {
			return nil, nil
		}
		first := len(forest)
		for first > 0 && groups[forest[first-1]].pair.opening > pair.opening {
			if interruption(s.ctx) != "" {
				return nil, nil
			}
			first--
		}
		groups = append(groups, group{pair: pair, children: append([]int(nil), forest[first:]...)})
		forest = append(forest[:first], len(groups)-1)
	}
	content := emphasisContent{scanner: s, parts: parts, starts: make([]int, len(parts)+1)}
	content.starts[0] = start
	for i, part := range parts {
		if interruption(s.ctx) != "" {
			return nil, nil
		}
		content.starts[i+1] = content.starts[i] + len(part.Semantic.Value)
	}
	// Build privately with explicit frames, so nesting does not consume the Go
	// call stack. The flat-node cursor advances once over original source order;
	// completed children are appended once, never copied with an accumulated prefix.
	type frame struct {
		index, next int
		children    []*Node
		opening     Span
	}
	stack := []frame{{index: -1}}
	nodes := inlineNodes{scanner: s, nodes: flat}
	for len(stack) != 0 {
		if interruption(s.ctx) != "" {
			return nil, nil
		}
		top := &stack[len(stack)-1]
		children := forest
		if top.index >= 0 {
			children = groups[top.index].children
		}
		if top.next < len(children) {
			index := children[top.next]
			top.next++
			pair := groups[index].pair
			opening, err := content.slice(pair.opening, pair.opening+pair.width)
			if err != nil || interruption(s.ctx) != "" {
				return nil, err
			}
			op := contentEnvelope(opening)
			before, err := nodes.take(op.start)
			if err != nil || interruption(s.ctx) != "" {
				return nil, err
			}
			top.children = append(top.children, before...)
			if _, err := nodes.take(op.end); err != nil || interruption(s.ctx) != "" {
				return nil, err
			}
			stack = append(stack, frame{index: index, opening: op})
			continue
		}
		if top.index < 0 {
			rest, err := nodes.take(contentEnvelope(parts).end)
			if err != nil || interruption(s.ctx) != "" {
				return nil, err
			}
			return append(top.children, rest...), nil
		}
		pair := groups[top.index].pair
		closing, err := content.slice(pair.closing, pair.closing+pair.width)
		if err != nil || interruption(s.ctx) != "" {
			return nil, err
		}
		cl := contentEnvelope(closing)
		rest, err := nodes.take(cl.start)
		if err != nil || interruption(s.ctx) != "" {
			return nil, err
		}
		top.children = append(top.children, rest...)
		if _, err := nodes.take(cl.end); err != nil || interruption(s.ctx) != "" {
			return nil, err
		}
		payload, err := content.slice(pair.opening+pair.width, pair.closing)
		if err != nil || interruption(s.ctx) != "" {
			return nil, err
		}
		kind := Emphasis
		if pair.width == 2 {
			kind = Strong
		}
		envelope := top.opening
		envelope.end = cl.end
		n, err := NewNode(NodeSpec{Kind: kind, Envelope: envelope, Content: payload, Children: top.children,
			Markers:    []Marker{{Role: "opening", Source: top.opening}, {Role: "closing", Source: cl}},
			Properties: Properties{Delimiter: &DelimiterProperties{Marker: pair.marker, Width: pair.width}}})
		if err != nil {
			return nil, err
		}
		stack = stack[:len(stack)-1]
		parent := &stack[len(stack)-1]
		parent.children = append(parent.children, n)
	}
	return nil, ErrModel
}
