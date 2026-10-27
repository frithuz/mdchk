package parser

import (
	"context"
	"math/bits"
	"sort"
)

// TabCharacters borrows completed original syntax and its operation. Each
// returned detached span owns exactly one original TAB byte. CodeSpan and HTML
// remain literal with either code option; only actual code block contributions
// and intrinsic indentation are admitted by code=true.
func (d *Document) TabCharacters(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, code bool) ([]Span, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	type tab struct {
		offset        int
		literal, code bool
	}
	var tabs []tab
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
	// Scan original bytes, including source syntax omitted from final Text.
	for offset := 0; offset < index.source.Len(); offset++ {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		if index.source.content[offset] != '\t' {
			continue
		}
		capacity, err := grow(len(tabs), cap(tabs))
		if err != nil {
			return nil, err
		}
		if capacity != cap(tabs) {
			next := make([]tab, len(tabs), capacity)
			copy(next, tabs)
			tabs = next
		}
		if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
			return nil, err
		}
		tabs = append(tabs, tab{offset: offset})
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
	mark := func(span Span, literal, raw bool) error {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return err
		}
		if !span.Present() || span.source.Token() != index.source.Token() {
			return ErrModel
		}
		if err := headingFactCharge(ctx, budget, int64(bits.Len(uint(len(tabs))))+1, 0); err != nil {
			return err
		}
		first := sort.Search(len(tabs), func(i int) bool { return tabs[i].offset >= span.Start() })
		for i := first; i < len(tabs); i++ {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return err
			}
			if tabs[i].offset >= span.End() {
				break
			}
			if raw {
				line, err := locate(tabs[i].offset)
				if err != nil {
					return err
				}
				if tabs[i].offset < cursors[line].offset {
					continue
				}
			}
			if literal {
				tabs[i].literal = true
			} else {
				tabs[i].code = true
			}
		}
		return nil
	}
	type frame struct {
		node *Node
		alt  bool
	}
	// Reserve both initial capacity and the logical root frame before construction.
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
				raw, rawStart = n.rawTag, n.spec.Envelope.Start()
			} else if raw == n.rawTag && n.rawClosing {
				if err := mark(Span{source: index.source, start: rawStart, end: n.spec.Envelope.End(), present: true}, true, true); err != nil {
					return nil, err
				}
				raw = ""
			}
		}
		literal := n.Kind() == CodeSpan || n.Kind() == InlineHTML || n.Kind() == HTMLBlock
		block := n.Kind() == CodeBlock
		if literal || block {
			for _, segment := range n.spec.Content {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return nil, err
				}
				if err := mark(segment.Source, literal, false); err != nil {
					return nil, err
				}
			}
			for _, m := range n.spec.Markers {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return nil, err
				}
				if literal || m.Role == "indent-prefix" {
					if err := mark(m.Source, literal, false); err != nil {
						return nil, err
					}
				}
			}
		}
		// Push reverse source order, including actual HTMLBlock comment-tail facts.
		// Image descriptions retain local literal bytes but cannot change outer raw.
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
				stack = append(stack, frame{node: children[i], alt: f.alt || n.Kind() == Image})
			}
		}
	}
	if raw != "" {
		if err := mark(Span{source: index.source, start: rawStart, end: index.source.Len(), present: true}, true, true); err != nil {
			return nil, err
		}
	}
	if d.frontmatter.boundary.region.Present() {
		if err := mark(d.frontmatter.boundary.region, true, false); err != nil {
			return nil, err
		}
	}
	var out []Span
	for _, t := range tabs {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		if t.literal || t.code && !code {
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
		out = append(out, Span{source: index.source, start: t.offset, end: t.offset + 1, present: true})
	}
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
