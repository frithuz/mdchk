package parser

import (
	"context"
	"math/bits"
	"sort"
)

// TrailingWhitespace returns one full original physical ASCII-space/tab suffix
// per violation. It borrows completed production ownership and the original
// index/options/operation, exposing no syntax handles or correction authority.
// Failure, including the final checkpoint, withholds the entire inventory.
func (d *Document) TrailingWhitespace(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, spaces int, strict, code bool) ([]Span, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	if spaces < 2 {
		return nil, ErrModel
	}
	type lineState struct {
		suffix, prefix              int
		tabs, protected, code, hard bool
	}
	if err := headingFactCharge(ctx, budget, int64(len(index.lines))+1, int64(len(index.lines))); err != nil {
		return nil, err
	}
	lines := make([]lineState, len(index.lines))
	// Discover the complete suffix once, independently of retained Text nodes.
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
			s.tabs = s.tabs || b == '\t'
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
	// Associate only suffix intersections, or complete physical code contributions.
	mark := func(span Span, literal, physical bool) error {
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
			if physical || (lines[i].suffix < span.End() && index.lines[i].end > span.Start()) {
				if literal {
					lines[i].code = true
				} else {
					lines[i].protected = true
				}
			}
		}
		return nil
	}
	if d.frontmatter.boundary.region.Present() {
		if err := mark(d.frontmatter.boundary.region, false, false); err != nil {
			return nil, err
		}
	}
	type frame struct {
		node *Node
		alt  bool
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
			if raw == "" && !n.rawClosing {
				raw, rawStart = n.rawTag, n.spec.Envelope.Start()
			} else if raw == n.rawTag && n.rawClosing {
				span := Span{source: index.source, start: rawStart, end: n.spec.Envelope.End(), present: true}
				if err := mark(span, false, false); err != nil {
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
			case "hard-break-marker":
				if n.Kind() == HardBreak && n.spec.Properties.Break != nil && n.spec.Properties.Break.Origin == "spaces" {
					i, err := locate(m.Source.Start())
					if err != nil {
						return nil, err
					}
					lines[i].hard = m.Source.Start() == lines[i].suffix && m.Source.End() == index.lines[i].end
				}
			}
		}
		switch n.Kind() {
		case HTMLBlock, InlineHTML:
			if err := mark(n.spec.Envelope, false, false); err != nil {
				return nil, err
			}
		case CodeSpan:
			if err := mark(n.spec.Envelope, true, false); err != nil {
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
			if err := mark(span, true, true); err != nil {
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
					if err := mark(n.spec.Envelope, false, false); err != nil {
						return nil, err
					}
					break
				}
				stack = append(stack, frame{children[i], alt})
			}
		}
	}
	if raw != "" {
		if err := mark(Span{source: index.source, start: rawStart, end: index.source.Len(), present: true}, false, false); err != nil {
			return nil, err
		}
	}
	for i, s := range lines {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		end := index.lines[i].end
		if s.suffix == end || s.protected || (s.code && !code) {
			continue
		}
		blank := s.suffix <= s.prefix
		if !s.code && !blank && !s.tabs && end-s.suffix == spaces && (!strict || s.hard) {
			continue
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
		out = append(out, Span{source: index.source, start: s.suffix, end: end, present: true})
	}
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
