package parser

import (
	"context"
	"math/bits"
	"sort"
)

// BlockquoteGaps returns one detached original body span per physical blank
// separating consecutive actual quote siblings of the same final parent.
// Whole gaps must be ordinary ASCII blanks after retained ancestor prefixes.
// It borrows completed syntax and the original operation, granting no edits.
func (d *Document) BlockquoteGaps(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) ([]Span, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	type lineState struct {
		suffix, prefix int
		protected      bool
	}
	if err := headingFactCharge(ctx, budget, int64(len(index.lines))+1, int64(len(index.lines))); err != nil {
		return nil, err
	}
	lines := make([]lineState, len(index.lines))
	// Discover original ASCII whitespace, including source omitted from Text.
	for i, l := range index.lines {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		s := &lines[i]
		s.suffix, s.prefix = l.end, l.start
		for s.suffix > l.start {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return nil, err
			}
			b := index.source.content[s.suffix-1]
			if b != ' ' && b != '\t' {
				break
			}
			s.suffix--
		}
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
	// Associate actual physical ownership. End-1 preserves exclusive boundaries;
	// using the physical ending also protects zero-width Content lines.
	mark := func(span Span) error {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return err
		}
		if !span.Present() || span.source.Token() != index.source.Token() {
			return ErrModel
		}
		if span.Start() == span.End() {
			return nil
		}
		first, err := locate(span.Start())
		if err != nil {
			return err
		}
		last, err := locate(span.End() - 1)
		if err != nil {
			return err
		}
		for i := first; i <= last; i++ {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return err
			}
			lines[i].protected = true
		}
		return nil
	}
	if d.frontmatter.boundary.region.Present() {
		if err := mark(d.frontmatter.boundary.region); err != nil {
			return nil, err
		}
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
	var out []Span
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
	for len(stack) > 0 {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := f.node
		if !f.alt && n.rawTag != "" {
			if err := headingFactCharge(ctx, budget, 2*int64(len(n.rawTag))+int64(len(raw))+1, 0); err != nil {
				return nil, err
			}
			if raw == "" && !n.rawClosing {
				raw, rawStart = n.rawTag, n.spec.Envelope.Start()
			} else if raw == n.rawTag && n.rawClosing {
				span := Span{source: index.source, start: rawStart, end: n.spec.Envelope.End(), present: true}
				if err := mark(span); err != nil {
					return nil, err
				}
				raw = ""
			}
		}
		for _, m := range n.spec.Markers {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return nil, err
			}
			switch m.Role {
			case "quote-prefix", "item-prefix", "item-indent":
				i, err := locate(m.Source.Start())
				if err != nil {
					return nil, err
				}
				if m.Source.End() > index.lines[i].end {
					return nil, ErrModel
				}
				lines[i].prefix = max(lines[i].prefix, m.Source.End())

			}
		}
		switch n.Kind() {
		case HTMLBlock, InlineHTML:
			if err := mark(n.spec.Envelope); err != nil {
				return nil, err
			}
		case CodeSpan:
			if err := mark(n.spec.Envelope); err != nil {
				return nil, err
			}
		case CodeBlock:
			span := n.spec.Envelope
			if n.spec.Properties.Code == nil {
				return nil, ErrModel
			}
			if n.spec.Properties.Code.Style == "fenced" {
				// Only actual retained delimiter lines are outside the payload.
				for _, m := range n.spec.Markers {
					if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
						return nil, err
					}
					if m.Role == "opening" {
						i, e := locate(m.Source.Start())
						if e != nil {
							return nil, e
						}
						span.start = index.lines[i].next
					}
					if m.Role == "closing" {
						i, e := locate(m.Source.Start())
						if e != nil {
							return nil, e
						}
						span.end = index.lines[i].start
					}
				}
			}
			if span.end < span.start {
				return nil, ErrModel
			}
			if err := mark(span); err != nil {
				return nil, err
			}
		default:
			// Ordinary syntax and structural owners add no payload protection.
		}
		// Source-ordered children precede actual HTML comment-tail metadata. Image
		// descriptions cannot open or close an enclosing semantic raw region.
		for group := 0; group < 2; group++ {
			children := n.htmlTail
			if group == 1 {
				children = n.spec.Children
			}
			for i := len(children) - 1; i >= 0; i-- {
				if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
					return nil, err
				}
				capacity, e := grow(len(stack), cap(stack))
				if e != nil {
					return nil, e
				}
				if capacity != cap(stack) {
					next := make([]frame, len(stack), capacity)
					copy(next, stack)
					stack = next
				}
				alt := f.alt || n.Kind() == Image
				if n.Kind() == Image && raw != "" && !f.alt {
					if err := mark(n.spec.Envelope); err != nil {
						return nil, err
					}
					break
				}
				stack = append(stack, frame{children[i], alt})
			}
		}
	}
	if raw != "" {
		if err := mark(Span{source: index.source, start: rawStart, end: index.source.Len(), present: true}); err != nil {
			return nil, err
		}
	}
	// Reuse the admitted traversal storage after all raw/literal ownership is
	// known. Direct child identity supplies ancestry, never lexical quote depth.
	if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
		return nil, err
	}
	stack = append(stack, frame{node: d.root})
	for len(stack) > 0 {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		n := stack[len(stack)-1].node
		stack = stack[:len(stack)-1]
		body := n.Kind() == DocumentKind || n.Kind() == BlockQuote || n.Kind() == ListItem
		for j, child := range n.spec.Children {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return nil, err
			}
			if body && j > 0 && child.Kind() == BlockQuote && n.spec.Children[j-1].Kind() == BlockQuote {
				previous := n.spec.Children[j-1]
				last, err := locate(previous.spec.Envelope.end - 1)
				if err != nil {
					return nil, err
				}
				first, err := locate(child.spec.Envelope.start)
				if err != nil {
					return nil, err
				}
				ordinary := true
				for i := last + 1; i < first; i++ {
					if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
						return nil, err
					}
					if lines[i].protected || lines[i].suffix > lines[i].prefix {
						ordinary = false
						break
					}
				}
				// Original nonblank definitions veto adjacency even after reference
				// extraction removed them from the final child sequence.
				if ordinary {
					for i := last + 1; i < first; i++ {
						if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
							return nil, err
						}
						capacity, e := grow(len(out), cap(out))
						if e != nil {
							return nil, e
						}
						if capacity != cap(out) {
							next := make([]Span, len(out), capacity)
							copy(next, out)
							out = next
						}
						if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
							return nil, err
						}
						out = append(out, Span{source: index.source, start: lines[i].prefix, end: index.lines[i].end, present: true})
					}
				}
			}
			if child.Kind() != BlockQuote && child.Kind() != List && child.Kind() != ListItem {
				continue
			}
			capacity, e := grow(len(stack), cap(stack))
			if e != nil {
				return nil, e
			}
			if capacity != cap(stack) {
				next := make([]frame, len(stack), capacity)
				copy(next, stack)
				stack = next
			}
			if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
				return nil, err
			}
			stack = append(stack, frame{node: child})
		}
	}
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
