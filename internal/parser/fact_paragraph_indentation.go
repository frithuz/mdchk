package parser

import (
	"context"
	"math/bits"
	"sort"
	"unicode"
	"unicode/utf8"
)

// ParagraphIndentation borrows completed original production syntax and its
// allowance. Only root prose Paragraph contributions establish a fixed baseline;
// detached surplus spans grant no edit authority.
func (d *Document) ParagraphIndentation(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) ([]Span, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	charge := func(work, nodes int64) error { return headingFactCharge(ctx, budget, work, nodes) }
	grow := func(length, capacity int) (int, error) {
		if length < capacity {
			return capacity, nil
		}
		if capacity > int(^uint(0)>>1)/2 {
			return 0, ErrModel
		}
		capacity = max(1, capacity*2)
		if err := charge(int64(length)+1, int64(capacity)); err != nil {
			return 0, err
		}
		return capacity, nil
	}
	type lineState struct {
		start, first, baseline          int
		owned, witness, protected, hard bool
	}
	if err := charge(int64(len(index.lines))+1, int64(len(index.lines))); err != nil {
		return nil, err
	}
	lines := make([]lineState, len(index.lines))
	locate := func(offset int) (int, error) {
		if err := charge(int64(bits.Len(uint(len(index.lines))))+1, 0); err != nil {
			return 0, err
		}
		i := sort.Search(len(index.lines), func(i int) bool { return index.lines[i].start > offset }) - 1
		if i < 0 || offset >= index.source.Len() {
			return 0, ErrModel
		}
		return i, nil
	}
	// Charge each attempted byte probe, including the terminating failed probe.
	indent := func(i, baseline int) (end, column, surplus int, err error) {
		end, surplus = index.lines[i].start, -1
		for {
			if err = charge(1, 0); err != nil {
				return
			}
			if end >= index.lines[i].end {
				return
			}
			b := index.source.content[end]
			if b != ' ' && b != '\t' {
				return
			}
			next := column + 1
			if b == '\t' {
				next = column + 4 - column%4
			}
			if surplus < 0 && next > baseline {
				surplus = end
			}
			column = next
			end++
		}
	}
	protect := func(span Span) error {
		if err := charge(1, 0); err != nil {
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
		for i := first + 1; i <= last; i++ {
			if err := charge(1, 0); err != nil {
				return err
			}
			lines[i].protected = true
		}
		return nil
	}
	paragraph := func(n *Node) error {
		first, baseline := -1, 0
		for _, part := range n.spec.Content {
			if err := charge(1, 0); err != nil {
				return err
			}
			span := part.Source
			if !span.Present() || span.source.Token() != index.source.Token() {
				return ErrModel
			}
			if span.start == span.end {
				continue
			}
			a, err := locate(span.start)
			if err != nil {
				return err
			}
			z, err := locate(span.end - 1)
			if err != nil {
				return err
			}
			for i := a; i <= z; i++ {
				if err := charge(1, 0); err != nil {
					return err
				}
				start, end := max(span.start, index.lines[i].start), min(span.end, index.lines[i].end)
				if start >= end {
					continue
				}
				if first < 0 {
					first = i
					prefix, col, _, err := indent(i, 0)
					if err != nil {
						return err
					}
					if prefix != start {
						return ErrModel
					}
					baseline = col
				}
				if !lines[i].owned || start < lines[i].start {
					lines[i].start = start
				}
				lines[i].owned, lines[i].first, lines[i].baseline = true, first, baseline
			}
		}
		return nil
	}
	text := func(n *Node) error {
		for _, part := range n.spec.Content {
			if err := charge(1, 0); err != nil {
				return err
			}
			if !part.Source.Present() || part.Source.source.Token() != index.source.Token() || !part.Semantic.Present {
				return ErrModel
			}
			switch part.Transformation {
			case Exact, Escape, Entity, Normalized, PartialTab:
			default:
				return ErrModel
			}
			if part.Transformation == Exact && len(part.Semantic.Value) != part.Source.end-part.Source.start {
				return ErrModel
			}
			for offset := 0; offset < len(part.Semantic.Value); {
				// Decode, provenance association, NUL and bounded Unicode classification.
				if err := charge(12, 0); err != nil {
					return err
				}
				r, width := utf8.DecodeRuneInString(part.Semantic.Value[offset:])
				at := part.Source.start
				if part.Transformation == Exact {
					at += offset
				}
				nul := part.Transformation == Normalized && part.Source.end-part.Source.start == 1 && index.source.content[part.Source.start] == 0
				offset += width
				i, err := locate(at)
				if err != nil {
					return err
				}
				if !nul && !unicode.IsSpace(r) && !unicode.Is(unicode.Cc, r) {
					lines[i].witness = true
				}
			}
		}
		return nil
	}
	type frame struct {
		node                           *Node
		prose, exit, restore, excluded bool
		raw                            string
		rawStart                       int
	}
	if err := charge(1, 2); err != nil {
		return nil, err
	}
	stack := make([]frame, 1)
	stack[0].node = d.root
	push := func(f frame) error {
		if err := charge(1, 1); err != nil {
			return err
		}
		capacity, err := grow(len(stack), cap(stack))
		if err != nil {
			return err
		}
		if capacity != cap(stack) {
			next := make([]frame, len(stack), capacity)
			copy(next, stack)
			stack = next
		}
		stack = append(stack, f)
		return nil
	}
	raw, rawStart := "", 0
	for len(stack) > 0 {
		if err := charge(1, 0); err != nil {
			return nil, err
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := f.node
		if f.exit {
			if f.restore {
				if raw != "" {
					if err := protect(Span{source: index.source, start: rawStart, end: n.spec.Envelope.end, present: true}); err != nil {
						return nil, err
					}
				}
				raw, rawStart = f.raw, f.rawStart
			}
			continue
		}
		prose := f.prose
		excluded := f.excluded
		switch n.Kind() {
		case BlockQuote, List, ListItem:
			excluded, prose = true, false
		case Heading, Table, TableRow, TableCell, CodeBlock, HTMLBlock:
			prose = false
		case Paragraph:
			if !excluded {
				if err := paragraph(n); err != nil {
					return nil, err
				}
			}
			prose = !excluded
		case Text:
			if prose && raw == "" {
				if err := text(n); err != nil {
					return nil, err
				}
			}
		case CodeSpan, InlineHTML:
			if err := protect(n.spec.Envelope); err != nil {
				return nil, err
			}
		case DocumentKind, ThematicBreak, SoftBreak, Emphasis, Strong, Strike, Link, Image:
			// Transparent ownership or non-witness syntax; children retain context.
		case HardBreak:
			if prose && raw == "" {
				for _, part := range n.spec.Content {
					if err := charge(1, 0); err != nil {
						return nil, err
					}
					if !part.Source.Present() || part.Source.start == part.Source.end {
						continue
					}
					i, err := locate(part.Source.start)
					if err != nil {
						return nil, err
					}
					if part.Source.start == index.lines[i].end && part.Source.end == index.lines[i].next && i+1 < len(lines) {
						lines[i+1].hard = true
					}
				}
			}
		}
		if n.rawTag != "" {
			if err := charge(2*int64(len(n.rawTag))+int64(len(raw))+1, 0); err != nil {
				return nil, err
			}
			if raw == "" && !n.rawClosing {
				raw, rawStart = n.rawTag, n.spec.Envelope.start
			} else if raw == n.rawTag && n.rawClosing {
				if err := protect(Span{source: index.source, start: rawStart, end: n.spec.Envelope.end, present: true}); err != nil {
					return nil, err
				}
				raw = ""
			}
		}
		exit := frame{node: n, exit: true}
		if n.Kind() == Image {
			exit.restore = true
			exit.raw, exit.rawStart = raw, rawStart
			prose = prose && raw == ""
			raw, rawStart = "", 0
		}
		if err := push(exit); err != nil {
			return nil, err
		}
		for group := 0; group < 2; group++ {
			children := n.htmlTail
			if group == 1 {
				children = n.spec.Children
			}
			for i := len(children) - 1; i >= 0; i-- {
				if err := push(frame{node: children[i], prose: prose, excluded: excluded}); err != nil {
					return nil, err
				}
			}
		}
	}
	if raw != "" {
		if err := protect(Span{source: index.source, start: rawStart, end: index.source.Len(), present: true}); err != nil {
			return nil, err
		}
	}
	var out []Span
	for i, s := range lines {
		if err := charge(1, 0); err != nil {
			return nil, err
		}
		if !s.owned || i == s.first || !s.witness || s.protected || s.hard {
			continue
		}
		end, col, start, err := indent(i, s.baseline)
		if err != nil {
			return nil, err
		}
		if col <= s.baseline || start < 0 || end != s.start || end >= index.lines[i].end {
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
		if err := charge(1, 1); err != nil {
			return nil, err
		}
		out = append(out, Span{source: index.source, start: start, end: end, present: true})
	}
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
