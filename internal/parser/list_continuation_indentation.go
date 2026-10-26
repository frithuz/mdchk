package parser

import (
	"context"
	"math/bits"
	"sort"
)

// ListContinuationFact is detached original indentation evidence. Actual and
// Expected count item columns; consumed ancestor quote columns are neutral.
// A tab is indivisible evidence, including when shared with a required prefix.
type ListContinuationFact struct {
	Span             Span
	Actual, Expected int
}

// listContinuationCursor is the scalar portion of an original Indentation view.
// The projection borrows one source/index for all cursors; per-line replay does
// not retain redundant LineIndex descriptors or allocate semantic segments.
type listContinuationCursor struct {
	offset, end, column, tabRemaining int
}

// ListContinuationIndentation projects final nearest-item ownership from the
// completed original tree and removed definitions. Only confirmed container
// markers are replayed, using original physical tab phase. No grammar is rerun,
// and no provisional paragraph or container ownership is retained.
func (d *Document) ListContinuationIndentation(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) ([]ListContinuationFact, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	type facing struct {
		owner          *Node
		boundary, tail listContinuationCursor
		quotes         int
	}
	type lineState struct {
		cursor, tail             listContinuationCursor
		quotes                   int
		facing                   facing
		exempt, eligible, opaque bool
		owner                    *Node
		expected                 int
	}
	if err := headingFactCharge(ctx, budget, int64(len(index.lines))+1, int64(len(index.lines))); err != nil {
		return nil, err
	}
	lines := make([]lineState, len(index.lines))
	for i, line := range index.lines {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		c := listContinuationCursor{offset: line.start, end: line.end}
		lines[i].cursor, lines[i].tail = c, c
	}
	locate := func(offset int) (int, error) {
		if err := headingFactCharge(ctx, budget, int64(bits.Len(uint(len(index.lines))))+1, 0); err != nil {
			return 0, err
		}
		i := sort.Search(len(index.lines), func(i int) bool { return index.lines[i].start > offset }) - 1
		if i < 0 || (offset >= index.lines[i].next && offset != index.lines[i].end) {
			return 0, ErrModel
		}
		return i, nil
	}
	// These cursors borrow source/index; unlike Consume, no semantic segments or
	// expanded strings are needed. Every original byte visit is reserved first.
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
	type frame struct {
		node, owner    *Node
		expected, next int
		entered        bool
	}
	stack := make([]frame, 1)
	stack[0].node = d.root
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
	mark := func(span Span, owner *Node, expected int, all bool) error {
		first, err := locate(span.Start())
		if err != nil {
			return err
		}
		last := first
		if all && span.End() > span.Start() {
			last, err = locate(span.End() - 1)
			if err != nil {
				return err
			}
		}
		for i := first; i <= last; i++ {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return err
			}
			if owner != nil {
				lines[i].eligible = true
				lines[i].owner = owner
				lines[i].expected = expected
			}
		}
		return nil
	}
	reference := 0
	for len(stack) > 0 {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		f := &stack[len(stack)-1]
		if !f.entered {
			f.entered = true
			if f.node.Kind() == ListItem {
				p := f.node.spec.Properties.List
				if p == nil || !p.ContentIndent.Present || p.ContentIndent.Value <= 0 || f.expected > int(^uint(0)>>1)-p.ContentIndent.Value {
					return nil, ErrModel
				}
				f.expected += p.ContentIndent.Value
				f.owner = f.node
			}
			for _, m := range f.node.spec.Markers {
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
				s := &lines[i]
				if m.Role == "item-prefix" {
					s.exempt = true
					continue
				}
				if s.exempt {
					continue
				}
				c := s.cursor
				if c.offset != m.Source.Start() {
					return nil, ErrModel
				}
				if m.Role == "item-indent" {
					if err := consume(&c, f.node.spec.Properties.List.ContentIndent.Value); err != nil {
						return nil, err
					}
				} else {
					before := c
					if err := consume(&c, 3); err != nil {
						return nil, err
					}
					if c.tabRemaining != 0 || c.offset >= c.end || index.source.content[c.offset] != '>' {
						return nil, ErrModel
					}
					if s.facing.owner != f.owner || f.owner == nil {
						s.facing = facing{owner: f.owner, boundary: c, tail: s.tail, quotes: s.quotes}
					}
					if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
						return nil, err
					}
					if c.column == int(^uint(0)>>1) {
						return nil, ErrModel
					}
					c.offset++
					c.column++
					if err := consume(&c, 1); err != nil {
						return nil, err
					}
					s.quotes += c.column - before.column
					s.tail = c
				}
				end := c.offset
				if c.tabRemaining > 0 {
					end++
				}
				if end != m.Source.End() {
					return nil, ErrModel
				}
				s.cursor = c
			}
			switch f.node.Kind() {
			case Paragraph:
				if err := mark(f.node.spec.Envelope, f.owner, f.expected, true); err != nil {
					return nil, err
				}
			case Heading, ThematicBreak, TableRow:
				if err := mark(f.node.spec.Envelope, f.owner, f.expected, false); err != nil {
					return nil, err
				}
			case CodeSpan, InlineHTML:
				// Final inline classification protects subsequent physical
				// payload/closing lines, while the opening paragraph line can
				// still own indentation preceding its inline token.
				span := f.node.spec.Envelope
				first, err := locate(span.Start())
				if err != nil {
					return nil, err
				}
				last, err := locate(span.End() - 1)
				if err != nil {
					return nil, err
				}
				for i := first + 1; i <= last; i++ {
					if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
						return nil, err
					}
					lines[i].opaque = true
				}
			case BlockQuote:
				// Only the actual quote opening can target an otherwise empty body.
				i, err := locate(f.node.spec.Envelope.Start())
				if err != nil {
					return nil, err
				}
				c := lines[i].cursor
				if err := consume(&c, int(^uint(0)>>1)); err != nil {
					return nil, err
				}
				if c.offset == c.end {
					if err := mark(f.node.spec.Envelope, f.owner, f.expected, false); err != nil {
						return nil, err
					}
				}
			case DocumentKind, List, ListItem, HTMLBlock, Text, SoftBreak, HardBreak, Emphasis, Strong, Strike, Link, Image, Table, TableCell:
				// These nodes supply structure or inline syntax, never a new target.
			case CodeBlock:
				if p := f.node.spec.Properties.Code; p != nil && p.Style == "fenced" {
					for _, m := range f.node.spec.Markers {
						if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
							return nil, err
						}
						if m.Role == "opening" || m.Role == "closing" {
							if err := mark(m.Source, f.owner, f.expected, false); err != nil {
								return nil, err
							}
						}
					}
				}
			}
		}
		end := f.node.spec.Envelope.End()
		var child *Node
		if f.next < len(f.node.spec.Children) {
			child = f.node.spec.Children[f.next]
			end = child.spec.Envelope.Start()
		}
		// Definitions are removed from children, but their final container envelopes
		// still establish ownership. Merge once in source order, as ListSpacing does.
		if reference < len(d.references.Definitions) {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return nil, err
			}
			span := d.references.Definitions[reference].spec.Envelope
			if span.Start() < end {
				if (f.node.Kind() != DocumentKind && f.node.Kind() != BlockQuote && f.node.Kind() != ListItem) || span.Start() < f.node.spec.Envelope.Start() || span.End() > f.node.spec.Envelope.End() {
					return nil, ErrModel
				}
				if err := mark(span, f.owner, f.expected, false); err != nil {
					return nil, err
				}
				reference++
				continue
			}
		}
		if child != nil {
			f.next++
			// Inline children supply only final literal/raw protection, never owners.
			// Table cells cannot introduce multiline physical contributions.
			if f.node.Kind() == DocumentKind || f.node.Kind() == BlockQuote || f.node.Kind() == List || f.node.Kind() == ListItem || f.node.Kind() == Table || f.node.Kind() == Paragraph || f.node.Kind() == Heading || f.node.Kind() == Emphasis || f.node.Kind() == Strong || f.node.Kind() == Strike || f.node.Kind() == Link || f.node.Kind() == Image {
				next := frame{node: child, owner: f.owner, expected: f.expected}
				capacity, err := grow(len(stack), cap(stack))
				if err != nil {
					return nil, err
				}
				if capacity != cap(stack) {
					n := make([]frame, len(stack), capacity)
					copy(n, stack)
					stack = n
				}
				if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
					return nil, err
				}
				stack = append(stack, next)
			}
			continue
		}
		stack = stack[:len(stack)-1]
	}
	if reference != len(d.references.Definitions) {
		return nil, ErrModel
	}
	var out []ListContinuationFact
	for _, s := range lines {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		if !s.eligible || s.exempt || s.opaque {
			continue
		}
		c, tail, quotes := s.cursor, s.tail, s.quotes
		if s.facing.owner == s.owner {
			c, tail, quotes = s.facing.boundary, s.facing.tail, s.facing.quotes
		} else {
			if err := consume(&c, int(^uint(0)>>1)); err != nil {
				return nil, err
			}
			if c.offset == c.end {
				continue
			}
		}
		actual := c.column - quotes
		if actual == s.expected {
			continue
		}
		start := c.offset
		if actual > s.expected {
			if s.expected > int(^uint(0)>>1)-quotes {
				return nil, ErrModel
			}
			target := s.expected + quotes
			if tail.column > target {
				return nil, ErrModel
			}
			if err := consume(&tail, target-tail.column); err != nil {
				return nil, err
			}
			if tail.column != target {
				return nil, ErrModel
			}
			start = tail.offset
		}
		capacity, err := grow(len(out), cap(out))
		if err != nil {
			return nil, err
		}
		if capacity != cap(out) {
			n := make([]ListContinuationFact, len(out), capacity)
			copy(n, out)
			out = n
		}
		if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
			return nil, err
		}
		out = append(out, ListContinuationFact{Span: Span{source: index.source, start: start, end: c.offset, present: true}, Actual: actual, Expected: s.expected})
	}
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
