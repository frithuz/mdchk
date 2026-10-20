package parser

import "strings"

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
// Nested matches are detected, not constructed: #25 owns nested publication.
func (s *inlineScanner) emphasisPairs() []emphasisPair {
	d := s.delimiters
	for i := range d {
		if interruption(s.ctx) != "" {
			return nil
		}
		d[i].previous, d[i].next = i-1, i+1
	}
	var floors [2][3][2]int
	for m := range floors {
		for r := range floors[m] {
			for o := range floors[m][r] {
				floors[m][r][o] = -1
			}
		}
	}
	remove := func(i int) {
		if d[i].previous >= 0 {
			d[d[i].previous].next = d[i].next
		}
		if d[i].next < len(d) {
			d[d[i].next].previous = d[i].previous
		}
	}
	var pairs []emphasisPair
	for current := 0; current < len(d); {
		if interruption(s.ctx) != "" {
			return nil
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
				return nil
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
		if len(pairs) != 0 && pair.opening < pairs[len(pairs)-1].closing+pairs[len(pairs)-1].width {
			s.pending = true
			return nil // retain the original provisional flat children
		}
		pairs = append(pairs, pair)
		for i := open.next; i != current; {
			if interruption(s.ctx) != "" {
				return nil
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
	return pairs
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

func (s *inlineScanner) emphasis(parts []Segment, flat []*Node) ([]*Node, error) {
	pairs := s.emphasisPairs()
	if len(pairs) == 0 || interruption(s.ctx) != "" {
		return flat, nil
	}
	raw := inlineContent{ctx: s.ctx, parts: parts}
	nodes := inlineNodes{scanner: s, nodes: flat}
	var out []*Node
	for _, pair := range pairs {
		if interruption(s.ctx) != "" {
			return nil, nil
		}
		if _, err := raw.take(pair.opening); err != nil {
			return nil, err
		}
		opening, err := raw.take(pair.opening + pair.width)
		if err != nil || interruption(s.ctx) != "" {
			return nil, err
		}
		payload, err := raw.take(pair.closing)
		if err != nil || interruption(s.ctx) != "" {
			return nil, err
		}
		closing, err := raw.take(pair.closing + pair.width)
		if err != nil || interruption(s.ctx) != "" {
			return nil, err
		}
		op, cl := contentEnvelope(opening), contentEnvelope(closing)
		before, err := nodes.take(op.start)
		if err != nil {
			return nil, err
		}
		out = append(out, before...)
		if _, err := nodes.take(op.end); err != nil {
			return nil, err
		}
		children, err := nodes.take(cl.start)
		if err != nil {
			return nil, err
		}
		if _, err := nodes.take(cl.end); err != nil {
			return nil, err
		}
		if interruption(s.ctx) != "" {
			return nil, nil
		}
		kind := Emphasis
		if pair.width == 2 {
			kind = Strong
		}
		envelope := op
		envelope.end = cl.end
		n, err := NewNode(NodeSpec{Kind: kind, Envelope: envelope, Content: payload, Children: children,
			Markers:    []Marker{{Role: "opening", Source: op}, {Role: "closing", Source: cl}},
			Properties: Properties{Delimiter: &DelimiterProperties{Marker: pair.marker, Width: pair.width}}})
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	remaining, err := nodes.take(contentEnvelope(parts).end)
	return append(out, remaining...), err
}
