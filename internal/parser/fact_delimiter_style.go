package parser

import (
	"context"
	"unicode"
	"unicode/utf8"
)

// DelimiterFact describes actual recognized syntax, independently of malformed
// whitespace candidates. Spans retain the immutable original source owner.
type DelimiterFact struct {
	Kind                       Kind
	Marker                     byte
	Width                      int
	Envelope, Opening, Closing Span
}

// DelimiterInventory owns its slices. Mutating a returned inventory cannot
// affect the tree, a repeated call, or another operation.
type DelimiterInventory struct {
	Valid      []DelimiterFact
	Whitespace []Span
}

type delimiterScalar struct {
	start, end      int
	marker          byte
	white, nonblank bool
}

// DelimiterStyle borrows the completed original production parse and allowance.
// It projects valid delimiters separately from one conservative malformed pair
// per source-contiguous ordinary Text island. It never decodes source again.
func (d *Document) DelimiterStyle(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (*DelimiterInventory, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	charge := func(w, n int64) error { return headingFactCharge(ctx, budget, w, n) }
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
	out := &DelimiterInventory{}
	var island []delimiterScalar
	appendScalar := func(s delimiterScalar) error {
		capacity, err := grow(len(island), cap(island))
		if err != nil {
			return err
		}
		if capacity != cap(island) {
			next := make([]delimiterScalar, len(island), capacity)
			copy(next, island)
			island = next
		}
		if err := charge(1, 1); err != nil {
			return err
		}
		island = append(island, s)
		return nil
	}
	appendWhite := func(start, end int) error {
		capacity, err := grow(len(out.Whitespace), cap(out.Whitespace))
		if err != nil {
			return err
		}
		if capacity != cap(out.Whitespace) {
			next := make([]Span, len(out.Whitespace), capacity)
			copy(next, out.Whitespace)
			out.Whitespace = next
		}
		if err := charge(1, 1); err != nil {
			return err
		}
		out.Whitespace = append(out.Whitespace, Span{source: index.source, start: start, end: end, present: true})
		return nil
	}
	// Raw original neighbors, bounded by the island rather than decoded payload.
	word := func(at int, before bool) (bool, error) {
		// UTF-8 (four bytes) and three bounded Unicode category searches, including
		// absent/unsuccessful probes, are reserved before examining source.
		if err := charge(256, 0); err != nil {
			return false, err
		}
		if len(island) == 0 {
			return false, nil
		}
		var r rune
		if before {
			if at == island[0].start {
				return false, nil
			}
			r, _ = utf8.DecodeLastRuneInString(index.source.content[island[0].start:at])
		} else {
			if at == island[len(island)-1].end {
				return false, nil
			}
			r, _ = utf8.DecodeRuneInString(index.source.content[at:island[len(island)-1].end])
		}
		return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r), nil
	}
	boundary := func() error {
		if err := charge(1, 0); err != nil {
			return err
		}
		defer func() { island = island[:0] }()
		type run struct {
			first, last int
			marker      byte
		}
		var runs [2]run
		count := 0
		poison := false
		for i := 0; ; {
			if err := charge(1, 0); err != nil {
				return err
			}
			if i == len(island) {
				break
			}
			if island[i].marker == 0 {
				i++
				continue
			}
			first := i
			marker := island[i].marker
			i++
			for { // Include failed comparison and terminal probe.
				if err := charge(3, 0); err != nil {
					return err
				}
				if i == len(island) || island[i].marker != marker {
					break
				}
				i++
			}
			if i-first > 2 {
				poison = true
				continue
			}
			before, err := word(island[first].start, true)
			if err != nil {
				return err
			}
			after, err := word(island[i-1].end, false)
			if err != nil {
				return err
			}
			if before && after {
				continue
			}
			if count < 2 {
				runs[count] = run{first, i, marker}
			}
			count++
		}
		if poison || count != 2 {
			return nil
		}
		a, b := runs[0], runs[1]
		if err := charge(4, 0); err != nil {
			return err
		}
		if a.marker != b.marker || a.last-a.first != b.last-b.first {
			return nil
		}
		before, err := word(island[a.first].start, true)
		if err != nil {
			return err
		}
		after, err := word(island[b.last-1].end, false)
		if err != nil {
			return err
		}
		if before || after {
			return nil
		}
		nonblank := false
		for i := a.last; ; i++ {
			if err := charge(1, 0); err != nil {
				return err
			}
			if i == b.first {
				break
			}
			nonblank = nonblank || island[i].nonblank
		}
		if !nonblank {
			return nil
		}
		left, right := a.last, b.first
		for {
			if err := charge(2, 0); err != nil {
				return err
			}
			if left == b.first || !island[left].white {
				break
			}
			left++
		}
		for {
			if err := charge(2, 0); err != nil {
				return err
			}
			if right == a.last || !island[right-1].white {
				break
			}
			right--
		}
		if left > a.last {
			if err := appendWhite(island[a.last].start, island[left-1].end); err != nil {
				return err
			}
		}
		if right < b.first {
			if err := appendWhite(island[right].start, island[b.first-1].end); err != nil {
				return err
			}
		}
		return nil
	}
	text := func(n *Node) error {
		for _, part := range n.spec.Content {
			if err := charge(4, 0); err != nil {
				return err
			}
			if !part.Semantic.Present || !part.Source.Present() || part.Source.SourceToken() != index.source.Token() {
				return ErrModel
			}
			if len(island) > 0 && island[len(island)-1].end != part.Source.start {
				if err := boundary(); err != nil {
					return err
				}
			}
			switch part.Transformation {
			case Normalized, PartialTab:
				if err := boundary(); err != nil {
					return err
				}
				continue
			case Exact, Escape, Entity:
			default:
				return ErrModel
			}
			exact := part.Transformation == Exact
			if exact {
				if err := charge(int64(len(part.Semantic.Value))+1, 0); err != nil {
					return err
				}
				if index.source.content[part.Source.start:part.Source.end] != part.Semantic.Value {
					return ErrModel
				}
			}
			nonblank := false
			for offset := 0; ; {
				// Decode, source association, whitespace/Cc classification and terminal
				// probe. Unicode predicates have at most 32 binary comparisons each.
				if err := charge(256, 0); err != nil {
					return err
				}
				if offset == len(part.Semantic.Value) {
					break
				}
				r, width := utf8.DecodeRuneInString(part.Semantic.Value[offset:])
				if r == utf8.RuneError && width == 1 {
					return ErrModel
				}
				white := IsUnicodeWhitespace(r)
				barrier := r == '\r' || r == '\n' || unicode.Is(unicode.Cc, r) && r != '\t' && r != '\f'
				if exact {
					if barrier {
						if err := boundary(); err != nil {
							return err
						}
					} else {
						s := delimiterScalar{start: part.Source.start + offset, end: part.Source.start + offset + width, white: white, nonblank: !white}
						if r == '*' || r == '_' {
							s.marker = byte(r)
						}
						if err := appendScalar(s); err != nil {
							return err
						}
					}
				} else {
					nonblank = nonblank || !white && !barrier
				}
				offset += width
			}
			if !exact {
				if err := appendScalar(delimiterScalar{start: part.Source.start, end: part.Source.end, nonblank: nonblank}); err != nil {
					return err
				}
			}
		}
		return charge(1, 0)
	}
	type frame struct {
		node                 *Node
		prose, exit, restore bool
		raw                  string
	}
	if err := charge(1, 2); err != nil {
		return nil, err
	}
	stack := make([]frame, 1)
	stack[0].node = d.root
	raw := ""
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
	for {
		if err := charge(8, 0); err != nil {
			return nil, err
		}
		if len(stack) == 0 {
			break
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := f.node
		if f.exit {
			if f.restore {
				raw = f.raw
			}
			switch n.Kind() {
			case Paragraph, Heading, TableCell, Emphasis, Strong, Strike, Link, Image:
				if err := boundary(); err != nil {
					return nil, err
				}
			default:
				// Leaves and non-inline containers cannot continue a local island.
			}
			continue
		}
		prose := f.prose
		switch n.Kind() {
		case Paragraph, Heading, TableCell:
			if err := boundary(); err != nil {
				return nil, err
			}
			prose = true
			if n.Kind() == TableCell {
				if err := charge(1, 0); err != nil {
					return nil, err
				}
				prose = n.spec.Properties.Cell != nil && !n.spec.Properties.Cell.Alignment.Present
			}
		case DocumentKind, BlockQuote, List, ListItem, Table, TableRow, CodeBlock, HTMLBlock:
			if err := boundary(); err != nil {
				return nil, err
			}
			prose = false
		case Text:
			if prose && raw == "" {
				if err := text(n); err != nil {
					return nil, err
				}
			} else {
				if err := boundary(); err != nil {
					return nil, err
				}
			}
		case Emphasis, Strong, Strike, Link, Image:
			if err := boundary(); err != nil {
				return nil, err
			}
			if n.Kind() == Link {
				if err := charge(18, 0); err != nil {
					return nil, err
				}
				if n.spec.Properties.Link == nil {
					return nil, ErrModel
				}
				origin := n.spec.Properties.Link.Origin
				if origin == "autolink" || origin == "bare-gfm" {
					prose = false
				}
			}
			if prose && raw == "" && (n.Kind() == Emphasis || n.Kind() == Strong) {
				if err := charge(int64(len(n.spec.Markers))+4, 0); err != nil {
					return nil, err
				}
				p := n.spec.Properties.Delimiter
				if p == nil {
					return nil, ErrModel
				}
				fact := DelimiterFact{Kind: n.Kind(), Marker: p.Marker, Width: p.Width, Envelope: n.Span()}
				for _, m := range n.spec.Markers {
					// Both role comparisons reserve original string bytes before use.
					if err := charge(2*int64(len(m.Role))+1, 0); err != nil {
						return nil, err
					}
					switch m.Role {
					case "opening":
						fact.Opening = m.Source
					case "closing":
						fact.Closing = m.Source
					}
				}
				capacity, err := grow(len(out.Valid), cap(out.Valid))
				if err != nil {
					return nil, err
				}
				if capacity != cap(out.Valid) {
					next := make([]DelimiterFact, len(out.Valid), capacity)
					copy(next, out.Valid)
					out.Valid = next
				}
				if err := charge(1, 1); err != nil {
					return nil, err
				}
				out.Valid = append(out.Valid, fact)
			}
		case InlineHTML, CodeSpan, SoftBreak, HardBreak, ThematicBreak:
			if err := boundary(); err != nil {
				return nil, err
			}
		}
		if n.rawTag != "" {
			if err := charge(2*int64(len(n.rawTag))+int64(len(raw))+1, 0); err != nil {
				return nil, err
			}
			if raw == "" && !n.rawClosing {
				raw = n.rawTag
			} else if raw == n.rawTag && n.rawClosing {
				raw = ""
			}
		}
		exit := frame{node: n, exit: true}
		if n.Kind() == Image {
			exit.restore = true
			exit.raw = raw
			prose = prose && raw == ""
			raw = ""
		}
		if err := push(exit); err != nil {
			return nil, err
		}
		for group := 0; group < 2; group++ {
			children := n.htmlTail
			if group == 1 {
				children = n.spec.Children
			}
			if err := charge(int64(len(children))+3, 0); err != nil {
				return nil, err
			}
			for i := len(children) - 1; i >= 0; i-- {
				if err := push(frame{node: children[i], prose: prose}); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := boundary(); err != nil {
		return nil, err
	}
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
