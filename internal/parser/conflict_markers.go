package parser

import (
	"context"
	"errors"
)

// ConflictMarkerInventory is an atomic original-source projection. Unavailable
// context never publishes a provisional prefix or proves absence of markers.
type ConflictMarkerInventory struct {
	Markers      []Span
	Availability Availability
}

// ConflictMarkers recognizes physical-column-one conflict runs using completed
// private production ownership. It borrows the original source, index, options,
// profile and cumulative allowance; it grants no decoding or correction access.
func (d *Document) ConflictMarkers(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out ConflictMarkerInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = ConflictMarkerInventory{}
		}
	}()
	charge := func(work, nodes int64) error { return headingFactCharge(ctx, budget, work, nodes) }
	type candidate struct {
		end       int
		kind      byte
		protected bool
	}
	if err = charge(int64(len(index.lines))+1, int64(len(index.lines))); err != nil {
		return out, err
	}
	lines := make([]candidate, len(index.lines))
	// Recognition inspects only the maximal homogeneous run. Suffix bytes have
	// no grammatical role and are never decoded, normalized or copied.
	for i := 0; ; i++ {
		if err = charge(1, 0); err != nil {
			return out, err
		}
		if i == len(index.lines) {
			break
		}
		l := index.lines[i]
		if err = charge(1, 0); err != nil {
			return out, err
		}
		if l.start == l.end {
			continue
		}
		b := index.source.content[l.start]
		if b != '<' && b != '>' && b != '|' && b != '=' {
			continue
		}
		end := l.start
		for {
			if err = charge(1, 0); err != nil {
				return out, err
			}
			if end == l.end {
				break
			}
			if err = charge(1, 0); err != nil {
				return out, err
			}
			if index.source.content[end] != b {
				break
			}
			end++
		}
		if end-l.start >= 7 {
			lines[i] = candidate{end: end, kind: b}
		}
	}
	// This binary lookup charges each reached comparison and failed terminal.
	locate := func(offset int) (int, error) {
		lo, hi := 0, len(index.lines)
		for {
			if e := charge(1, 0); e != nil {
				return 0, e
			}
			if lo == hi {
				break
			}
			mid := lo + (hi-lo)/2
			if index.lines[mid].start <= offset {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo == 0 || offset < 0 || offset >= index.source.Len() {
			return 0, ErrModel
		}
		return lo - 1, nil
	}
	mark := func(s Span, physical bool) error {
		if e := charge(1, 0); e != nil {
			return e
		}
		if !s.present || s.source != index.source || s.start < 0 || s.end < s.start || s.end > index.source.Len() {
			return ErrModel
		}
		if s.start == s.end {
			return nil
		}
		first, e := locate(s.start)
		if e != nil {
			return e
		}
		last, e := locate(s.end - 1)
		if e != nil {
			return e
		}
		for i := first; ; i++ {
			if e := charge(1, 0); e != nil {
				return e
			}
			if i > last {
				break
			}
			c := &lines[i]
			// CodeBlock ownership includes prefixes on its physical lines.
			// Other inline ownership protects only overlapping marker bytes.
			if c.kind != 0 && (physical || s.start < c.end && s.end > index.lines[i].start) {
				c.protected = true
			}
		}
		return nil
	}
	if d.frontmatter.boundary.region.present {
		if err = mark(d.frontmatter.boundary.region, false); err != nil {
			return out, err
		}
	}
	var stack []*Node
	stack, err = conflictMarkerGrow(ctx, budget, stack)
	if err != nil {
		return out, err
	}
	if err = charge(1, 1); err != nil {
		return out, err
	}
	stack = append(stack, d.root)
	raw := conflictRawState{}
	// One adapter across all owned HTML scans. Its document-nodes is an
	// additional scratch guard; original operation totals never reset.
	var htmlWork *parserContext
	for {
		if err = charge(1, 0); err != nil {
			return out, err
		}
		if len(stack) == 0 {
			break
		}
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if err = charge(16, 0); err != nil {
			return out, err
		}
		if n == nil || !contains(d.root.spec.Envelope, n.spec.Envelope) || n.spec.Envelope.end < n.spec.Envelope.start {
			return out, ErrModel
		}
		// Whole Images are opaque; their local markup cannot change outer raw
		// lifetime. Borrow private raw facts, never reconstruct HTML from Text.
		if n.spec.Kind == Image {
			if err = mark(n.spec.Envelope, false); err != nil {
				return out, err
			}
			continue
		}
		if n.spec.Kind == HTMLBlock {
			if htmlWork == nil {
				if err = charge(2, 2); err != nil {
					return out, err
				}
				htmlWork = &parserContext{Context: ctx, budget: &parseBudget{operation: budget}}
			}
			var closed []Span
			var available bool
			raw, closed, available, err = conflictHTMLRaw(htmlWork, index.source, n, raw)
			if err != nil {
				return out, err
			}
			if !available {
				return ConflictMarkerInventory{Availability: Unavailable}, nil
			}
			for i := 0; ; i++ {
				if err = charge(1, 0); err != nil {
					return out, err
				}
				if i == len(closed) {
					break
				}
				if err = mark(closed[i], false); err != nil {
					return out, err
				}
			}
			if err = mark(n.spec.Envelope, false); err != nil {
				return out, err
			}
			continue // Owned Content scan includes all original closing tails once.
		}
		if err = charge(int64(len(raw.name))+int64(len(n.rawTag))+1, 0); err != nil {
			return out, err
		}
		if n.rawTag != "" {
			if raw.name == "" && !n.rawClosing {
				raw = conflictRawState{name: n.rawTag, start: n.spec.Envelope.start}
			} else if raw.name == n.rawTag && n.rawClosing {
				if err = mark(Span{source: index.source, start: raw.start, end: n.spec.Envelope.end, present: true}, false); err != nil {
					return out, err
				}
				raw = conflictRawState{}
			}
		}
		switch n.spec.Kind {
		case CodeBlock:
			if n.spec.Properties.Code == nil {
				return ConflictMarkerInventory{Availability: Unavailable}, nil
			}
			if err = mark(n.spec.Envelope, true); err != nil {
				return out, err
			}
		case CodeSpan, InlineHTML:
			if n.spec.Kind == InlineHTML && n.spec.Properties.HTML == nil {
				return ConflictMarkerInventory{Availability: Unavailable}, nil
			}
			if err = mark(n.spec.Envelope, false); err != nil {
				return out, err
			}
		default:
			// Other kinds add no protection here; continue existing child traversal.
		}
		// LIFO pushes tails first, so actual children precede each owned tail.
		for group := 0; ; group++ {
			if err = charge(1, 0); err != nil {
				return out, err
			}
			if group == 2 {
				break
			}
			children := n.htmlTail
			if group == 1 {
				children = n.spec.Children
			}
			for i := len(children); ; {
				if err = charge(1, 0); err != nil {
					return out, err
				}
				if i == 0 {
					break
				}
				i--
				stack, err = conflictMarkerGrow(ctx, budget, stack)
				if err != nil {
					return out, err
				}
				if err = charge(1, 1); err != nil {
					return out, err
				}
				stack = append(stack, children[i])
			}
		}
	}
	if raw.name != "" {
		if err = mark(Span{source: index.source, start: raw.start, end: index.source.Len(), present: true}, false); err != nil {
			return out, err
		}
	}
	active := false
	for i := 0; ; i++ {
		if err = charge(1, 0); err != nil {
			return out, err
		}
		if i == len(lines) {
			break
		}
		c := lines[i]
		if c.kind == 0 || c.protected {
			continue
		}
		switch c.kind {
		case '<':
			active = true
		case '>':
			active = false
		case '=':
			if !active {
				continue
			}
		}
		out.Markers, err = conflictMarkerGrow(ctx, budget, out.Markers)
		if err != nil {
			return out, err
		}
		if err = charge(1, 1); err != nil {
			return out, err
		}
		out.Markers = append(out.Markers, Span{source: index.source, start: index.lines[i].start, end: c.end, present: true})
	}
	out.Availability = Available
	return out, nil
}

// Growth reserves new capacity and the copied prefix before either operation.
func conflictMarkerGrow[T any](ctx context.Context, budget *OperationBudget, dst []T) ([]T, error) {
	if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
		return nil, err
	}
	if len(dst) < cap(dst) {
		return dst, nil
	}
	if cap(dst) > int(^uint(0)>>1)/2 {
		return nil, ErrModel
	}
	capacity := max(1, cap(dst)*2)
	if err := headingFactCharge(ctx, budget, int64(len(dst))+int64(capacity)+1, int64(capacity)); err != nil {
		return nil, err
	}
	next := make([]T, len(dst), capacity)
	copy(next, dst)
	return next, nil
}
