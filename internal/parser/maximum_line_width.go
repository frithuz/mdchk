package parser

import (
	"context"
	"math/bits"
	"sort"
	"unicode"
	"unicode/utf8"
)

// MaximumLineWidth returns detached first eligible overflowing scalar spans.
// Original physical bytes determine width; syntax only determines eligibility.
// It borrows immutable completed original facts and their originating allowance.
func (d *Document) MaximumLineWidth(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, length int, code, tables, headings bool) ([]Span, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	if length < 1 {
		return nil, ErrModel
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
	if err := headingFactCharge(ctx, budget, int64(len(index.lines))+1, int64(len(index.lines))); err != nil {
		return nil, err
	}
	cursors := make([]listContinuationCursor, len(index.lines))
	for i, l := range index.lines {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		cursors[i] = listContinuationCursor{offset: l.start, end: l.end}
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

	// Literal and raw intervals stay separate: only raw ownership subtracts
	// wholly consumed enclosing prefix scalars. No source-sized byte mask is built.
	var literal, rawSpans []Span
	mark := func(span Span, raw bool) error {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return err
		}
		if !span.Present() || span.source.Token() != index.source.Token() {
			return ErrModel
		}
		if span.start == span.end {
			return nil
		}
		target := &literal
		if raw {
			target = &rawSpans
		}
		capacity, err := grow(len(*target), cap(*target))
		if err != nil {
			return err
		}
		if capacity != cap(*target) {
			next := make([]Span, len(*target), capacity)
			copy(next, *target)
			*target = next
		}
		if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
			return err
		}
		*target = append(*target, span)
		return nil
	}
	wholeLines := func(span Span) error {
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
		return mark(Span{source: index.source, start: index.lines[first].start, end: index.lines[last].next, present: true}, false)
	}
	// Actual destinations alone supply atoms. Synthetic prefixes have no range;
	// source segments must otherwise form one contiguous physical-line spelling.
	atom := func(value Value, markers []Marker) error {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return err
		}
		if !value.Semantic.Present || value.Semantic.Value == "" {
			return nil
		}
		valid := func(text string) (bool, error) {
			for offset := 0; offset < len(text); {
				// Reserve a complete UTF-8 probe and both Unicode classification tests.
				if err := headingFactCharge(ctx, budget, 7, 0); err != nil {
					return false, err
				}
				r, size := utf8.DecodeRuneInString(text[offset:])
				if unicode.IsSpace(r) || unicode.IsControl(r) {
					return false, nil
				}
				offset += size
			}
			return true, nil
		}
		start, end := -1, -1
		for _, segment := range value.Segments {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return err
			}
			if !segment.Source.Present() {
				if segment.Transformation == Synthetic {
					continue
				}
				return nil
			}
			s := segment.Source
			if s.source.Token() != index.source.Token() {
				return ErrModel
			}
			if start < 0 {
				start, end = s.start, s.end
			} else {
				if s.start != end {
					return nil
				}
				end = s.end
			}
		}
		if start < 0 || end <= start {
			return nil
		}
		line, err := locate(start)
		if err != nil {
			return err
		}
		if end > index.lines[line].end {
			return nil
		}
		ok, err := valid(value.Semantic.Value)
		if err != nil || !ok {
			return err
		}
		ok, err = valid(index.source.content[start:end])
		if err != nil || !ok {
			return err
		}
		// Angle wrappers expand first. Link wrappers may then join only at exact
		// byte adjacency; titles/whitespace are never jumped.
		for pass := 0; pass < 2; pass++ {
			for _, m := range markers {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return err
				}
				opening, closing := "destination-open", "destination-close"
				if pass == 1 {
					opening, closing = "link-open", "link-close"
				}
				if m.Role == opening && m.Source.end == start {
					start = m.Source.start
				}
				if m.Role == closing && m.Source.start == end {
					end = m.Source.end
				}
			}
		}
		return mark(Span{source: index.source, start: start, end: end, present: true}, false)
	}
	type frame struct {
		node *Node
		alt  bool
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
			c := cursors[i]
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
				c.offset++
				c.column++
				if err := consume(&c, 1); err != nil {
					return nil, err
				}
			} else {
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
			cursors[i] = c
		}

		if !f.alt && n.rawTag != "" {
			if raw == "" && !n.rawClosing {
				raw, rawStart = n.rawTag, n.spec.Envelope.start
			} else if raw == n.rawTag && n.rawClosing {
				if err := mark(Span{source: index.source, start: rawStart, end: n.spec.Envelope.end, present: true}, true); err != nil {
					return nil, err
				}
				raw = ""
			}
		}
		isLiteral := n.Kind() == CodeSpan || n.Kind() == InlineHTML || n.Kind() == HTMLBlock
		isCode := n.Kind() == CodeBlock && !code
		if isLiteral || isCode {
			for _, segment := range n.spec.Content {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return nil, err
				}
				if err := mark(segment.Source, false); err != nil {
					return nil, err
				}
			}
			for _, m := range n.spec.Markers {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return nil, err
				}
				if isLiteral || m.Role == "indent-prefix" {
					if err := mark(m.Source, false); err != nil {
						return nil, err
					}
				}
			}
		}
		if n.Kind() == Table && !tables || n.Kind() == Heading && !headings {
			if err := wholeLines(n.spec.Envelope); err != nil {
				return nil, err
			}
		}
		if p := n.spec.Properties.Link; p != nil {
			if err := atom(p.Destination, n.spec.Markers); err != nil {
				return nil, err
			}
		}
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
				stack = append(stack, frame{children[i], f.alt || n.Kind() == Image})
			}
		}
	}
	if raw != "" {
		if err := mark(Span{source: index.source, start: rawStart, end: index.source.Len(), present: true}, true); err != nil {
			return nil, err
		}
	}
	if d.frontmatter.boundary.region.Present() {
		if err := mark(d.frontmatter.boundary.region, false); err != nil {
			return nil, err
		}
	}
	for _, definition := range d.references.Definitions {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		if err := atom(definition.spec.Destination, definition.spec.Markers); err != nil {
			return nil, err
		}
	}
	// Reserve conservative sort comparison/swap work before each in-place sort.
	for _, spans := range [][]Span{literal, rawSpans} {
		if err := headingFactCharge(ctx, budget, 8*int64(len(spans))*int64(bits.Len(uint(len(spans)))+1)+1, 0); err != nil {
			return nil, err
		}
		sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	}
	var out []Span
	li, ri, literalEnd, rawEnd := 0, 0, 0, 0
	for i, line := range index.lines {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		width := 0
		for offset := line.start; offset < line.end; {
			if err := headingFactCharge(ctx, budget, 8, 0); err != nil {
				return nil, err
			}
			r, size := utf8.DecodeRuneInString(index.source.content[offset:line.end])
			increment := 1
			if r == '\t' {
				increment = 4 - width%4
			}
			width += increment
			for li < len(literal) {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return nil, err
				}
				if literal[li].start >= offset+size {
					break
				}
				literalEnd = max(literalEnd, literal[li].end)
				li++
			}
			for ri < len(rawSpans) {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return nil, err
				}
				if rawSpans[ri].start >= offset+size {
					break
				}
				rawEnd = max(rawEnd, rawSpans[ri].end)
				ri++
			}
			protected := literalEnd > offset || rawEnd > offset && offset >= cursors[i].offset
			if width > length && !protected {
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
				out = append(out, Span{source: index.source, start: offset, end: offset + size, present: true})
				break
			}
			offset += size
		}
	}
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
