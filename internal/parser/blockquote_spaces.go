package parser

import (
	"context"
	"math/bits"
	"sort"
)

// BlockquoteSpaces returns detached excess original ASCII SPACE spans after
// actual prose-facing quote markers. It borrows completed production ownership
// and the original operation; it grants no correction authority.
func (d *Document) BlockquoteSpaces(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) ([]Span, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	grow := func(length, capacity int) (int, error) {
		if length < capacity {
			return capacity, nil
		}
		if capacity > int(^uint(0)>>1)/2 {
			return 0, ErrModel
		}
		capacity = max(1, capacity*2)
		if err := headingFactCharge(ctx, budget, int64(length)+1, int64(capacity)); err != nil {
			return 0, err
		}
		return capacity, nil
	}
	type lineState struct {
		cursor                         listContinuationCursor
		marker                         int
		paragraph, prose, continuation bool
	}
	if err := headingFactCharge(ctx, budget, int64(len(index.lines))+1, int64(len(index.lines))); err != nil {
		return nil, err
	}
	lines := make([]lineState, len(index.lines))
	for i, l := range index.lines {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		lines[i].cursor = listContinuationCursor{offset: l.start, end: l.end}
		lines[i].marker = -1
	}
	locate := func(offset int) (int, error) {
		if err := headingFactCharge(ctx, budget, int64(bits.Len(uint(len(index.lines))))+1, 0); err != nil {
			return 0, err
		}
		i := sort.Search(len(index.lines), func(i int) bool { return index.lines[i].start > offset }) - 1
		if i < 0 || offset > index.source.Len() {
			return 0, ErrModel
		}
		return i, nil
	}
	// Replay only final confirmed prefixes. Offset stays on a partially consumed
	// TAB, preserving original physical phase and residual body ownership.
	consume := func(c *listContinuationCursor, columns int) error {
		for columns > 0 && c.offset < c.end {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return err
			}
			b := index.source.content[c.offset]
			if b != ' ' && b != '\t' {
				break
			}
			n := 1
			if b == '\t' {
				if c.tabRemaining == 0 {
					c.tabRemaining = 4 - c.column%4
				}
				n = min(columns, c.tabRemaining)
			}
			if c.column > int(^uint(0)>>1)-n {
				return ErrModel
			}
			c.column += n
			columns -= n
			if b == '\t' {
				c.tabRemaining -= n
				if c.tabRemaining != 0 {
					continue
				}
			}
			c.offset++
		}
		return nil
	}

	// Associate only actual contributions; a Paragraph envelope can bridge prefix
	// gaps or removed syntax and is never itself evidence of eligible prose.
	associate := func(span Span, paragraph, prose, continuation bool) error {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return err
		}
		if !span.Present() || span.source.Token() != index.source.Token() {
			return ErrModel
		}
		if span.start == span.end {
			return nil
		}
		first, err := locate(span.start)
		if err != nil {
			return err
		}
		last, err := locate(span.end - 1)
		if err != nil {
			return err
		}
		for i := first; i <= last; i++ {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return err
			}
			s := &lines[i]
			if continuation {
				if i > first {
					s.continuation = true
				}
				continue
			}
			a, z := max(span.start, index.lines[i].start), min(span.end, index.lines[i].end)
			if paragraph && a < z {
				s.paragraph = true
			}
			if prose {
				for offset := a; offset < z; offset++ {
					if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
						return err
					}
					b := index.source.content[offset]
					if b != ' ' && b != '\t' {
						s.prose = true
						break
					}
				}
			}
		}
		return nil
	}
	type frame struct {
		node           *Node
		paragraph, alt bool
	}
	if err := headingFactCharge(ctx, budget, 1, 2); err != nil {
		return nil, err
	}
	stack := make([]frame, 1)
	stack[0].node = d.root
	raw, rawStart := "", 0
	for len(stack) > 0 {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := f.node
		for _, m := range n.spec.Markers {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return nil, err
			}
			if m.Role != "item-prefix" && m.Role != "item-indent" && m.Role != "quote-prefix" {
				continue
			}
			i, err := locate(m.Source.Start())
			if err != nil {
				return nil, err
			}
			c := lines[i].cursor
			if c.offset != m.Source.Start() {
				return nil, ErrModel
			}
			if m.Role == "quote-prefix" {
				if err := consume(&c, 3); err != nil {
					return nil, err
				}
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return nil, err
				}
				if c.tabRemaining != 0 || c.offset >= c.end || index.source.content[c.offset] != '>' || c.column == int(^uint(0)>>1) {
					return nil, ErrModel
				}
				lines[i].marker = c.offset
				c.offset++
				c.column++
				if err := consume(&c, 1); err != nil {
					return nil, err
				}
			} else {
				// An actual intervening item owns the quote separator.
				lines[i].marker = -1
				p := n.spec.Properties.List
				if p == nil || !p.ContentIndent.Present || p.ContentIndent.Value <= 0 {
					return nil, ErrModel
				}
				columns := p.ContentIndent.Value
				if m.Role == "item-prefix" {
					if !p.MarkerWidth.Present || !p.Padding.Present {
						return nil, ErrModel
					}
					leading := columns - p.MarkerWidth.Value - p.Padding.Value
					if leading < 0 {
						return nil, ErrModel
					}
					if err := consume(&c, leading); err != nil {
						return nil, err
					}
					for j := 0; j < p.MarkerWidth.Value; j++ {
						if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
							return nil, err
						}
						if c.tabRemaining != 0 || c.offset >= c.end || c.column == int(^uint(0)>>1) {
							return nil, ErrModel
						}
						c.offset++
						c.column++
					}
					if p.Prefix == nil {
						return nil, ErrModel
					}
					// Empty items consume all actual trailing prefix columns;
					// effective Padding alone cannot describe that ownership.
					total := 0
					for _, segment := range p.Prefix.Segments {
						if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
							return nil, err
						}
						if !segment.Semantic.Present || total > int(^uint(0)>>1)-len(segment.Semantic.Value) {
							return nil, ErrModel
						}
						total += len(segment.Semantic.Value)
					}
					columns = total - leading - p.MarkerWidth.Value
					if columns < 0 {
						return nil, ErrModel
					}
				}
				if err := consume(&c, columns); err != nil {
					return nil, err
				}
			}
			end := c.offset
			if c.tabRemaining > 0 {
				end++
			}
			if end != m.Source.End() {
				return nil, ErrModel
			}
			lines[i].cursor = c
		}

		if !f.alt && n.rawTag != "" {
			// Tags are parser-confirmed bounded ASCII identities; reserve comparisons
			// before changing the semantic raw state. No spelling is guessed.
			if err := headingFactCharge(ctx, budget, 2*int64(len(n.rawTag))+int64(len(raw))+1, 0); err != nil {
				return nil, err
			}
			if raw == "" && !n.rawClosing {
				raw, rawStart = n.rawTag, n.spec.Envelope.start
			} else if raw == n.rawTag && n.rawClosing {
				if err := associate(Span{source: index.source, start: rawStart, end: n.spec.Envelope.end, present: true}, false, false, true); err != nil {
					return nil, err
				}
				raw = ""
			}
		}
		if n.Kind() == Paragraph {
			for _, part := range n.spec.Content {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return nil, err
				}
				if err := associate(part.Source, true, false, false); err != nil {
					return nil, err
				}
			}
		}
		if n.Kind() == CodeSpan || n.Kind() == InlineHTML {
			if err := associate(n.spec.Envelope, false, false, true); err != nil {
				return nil, err
			}
		}
		if f.paragraph && raw == "" {
			switch n.Kind() {
			case Text:
				for _, part := range n.spec.Content {
					if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
						return nil, err
					}
					if err := associate(part.Source, false, true, false); err != nil {
						return nil, err
					}
				}
			case DocumentKind, Paragraph, Heading, ThematicBreak, BlockQuote, List, ListItem, CodeBlock, HTMLBlock, InlineHTML, SoftBreak, HardBreak, Table, TableRow, TableCell:
				// These kinds supply no ordinary inline prose evidence.
			case CodeSpan, Link, Image, Emphasis, Strong, Strike:
				// Real ordinary inline syntax itself supplies a boundary, even when its
				// literal payload or image description has no ordinary Text child.
				span := n.spec.Envelope
				span.end = min(span.end, span.start+1)
				if err := associate(span, false, true, false); err != nil {
					return nil, err
				}
			}
		}
		// Reverse pushes preserve actual source order, including HTML comment tails.
		// Image descriptions cannot change an enclosing semantic raw body's state.
		for group := 0; group < 2; group++ {
			children := n.htmlTail
			if group == 1 {
				children = n.spec.Children
			}
			for i := len(children) - 1; i >= 0; i-- {
				if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
					return nil, err
				}
				capacity, err := grow(len(stack), cap(stack))
				if err != nil {
					return nil, err
				}
				if capacity != cap(stack) {
					next := make([]frame, len(stack), capacity)
					copy(next, stack)
					stack = next
				}
				paragraph := f.paragraph || n.Kind() == Paragraph
				if n.Kind() == Heading || n.Kind() == Table || n.Kind() == TableCell || n.Kind() == HTMLBlock || n.Kind() == CodeBlock {
					paragraph = false
				}
				stack = append(stack, frame{children[i], paragraph, f.alt || n.Kind() == Image})
			}
		}
	}
	if raw != "" {
		if err := associate(Span{source: index.source, start: rawStart, end: index.source.Len(), present: true}, false, false, true); err != nil {
			return nil, err
		}
	}
	var out []Span
	for i, s := range lines {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		if s.marker < 0 || !s.paragraph || !s.prose || s.continuation {
			continue
		}
		end := s.marker + 1
		tab := false
		for end < index.lines[i].end {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return nil, err
			}
			b := index.source.content[end]
			if b != ' ' && b != '\t' {
				break
			}
			tab = tab || b == '\t'
			end++
		}
		if tab || end-s.marker <= 2 || end == index.lines[i].end {
			continue
		}
		capacity, err := grow(len(out), cap(out))
		if err != nil {
			return nil, err
		}
		if capacity != cap(out) {
			next := make([]Span, len(out), capacity)
			copy(next, out)
			out = next
		}
		if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
			return nil, err
		}
		out = append(out, Span{source: index.source, start: s.marker + 2, end: end, present: true})
	}
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
