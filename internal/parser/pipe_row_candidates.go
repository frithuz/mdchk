package parser

import (
	"context"
	"errors"
	"math/bits"
	"sort"
)

// PipeRowCandidate is one detached physical Paragraph line with at least one
// structural pipe. Start and End are the line's trimmed content bounds: the
// container prefix, indentation, trailing spaces and tabs, and the line ending
// are excluded. Paragraph numbers the owning Paragraph in document order and
// Line is the one-based physical line. Cells, LeadingPipe and TrailingPipe are
// the table splitter's source cell count and outer pipes. Delimiter reports
// that every cell is a valid delimiter cell and the line starts with |, : or
// -, as a GFM delimiter row must. NextToTable reports that the Paragraph
// directly precedes or follows a sibling Table on the adjacent physical line.
type PipeRowCandidate struct {
	Start, End                                        int
	Paragraph, Line, Cells                            int
	LeadingPipe, TrailingPipe, Delimiter, NextToTable bool
}

// PipeRowCandidatesInventory contains every pipe-row candidate line of every
// Paragraph in document order. Unknown Paragraph provenance withholds all lines.
type PipeRowCandidatesInventory struct {
	Rows         []PipeRowCandidate
	Availability Availability
}

// PipeRowCandidates classifies each physical Paragraph line, including
// Paragraphs nested in block quotes and list items, with the same separator
// classification the table splitter uses. It never reads lines outside
// Paragraph content.
func (d *Document) PipeRowCandidates(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out PipeRowCandidatesInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = PipeRowCandidatesInventory{}
		} else if out.Availability != Available {
			out.Rows = nil
		}
	}()
	charge := func(w, n int64) error { return headingFactCharge(ctx, budget, w, n) }
	grow := func(length, capacity int) (int, error) {
		if length < capacity {
			return capacity, nil
		}
		if capacity > int(^uint(0)>>1)/2 {
			return 0, ErrModel
		}
		capacity = max(1, capacity*2)
		if e := charge(int64(length)+int64(capacity)+1, int64(capacity)); e != nil {
			return 0, e
		}
		return capacity, nil
	}
	var rows []PipeRowCandidate
	publish := func(row PipeRowCandidate) error {
		if e := charge(2, 0); e != nil {
			return e
		}
		capacity, e := grow(len(rows), cap(rows))
		if e != nil {
			return e
		}
		if capacity != cap(rows) {
			next := make([]PipeRowCandidate, len(rows), capacity)
			copy(next, rows)
			rows = next
		}
		if e = charge(1, 1); e != nil {
			return e
		}
		rows = append(rows, row)
		return nil
	}
	text := index.source.content
	locate := func(offset int) (int, error) {
		if e := charge(int64(bits.Len(uint(len(index.lines))))+1, 0); e != nil {
			return 0, e
		}
		i := sort.Search(len(index.lines), func(i int) bool { return index.lines[i].start > offset }) - 1
		if i < 0 || offset >= index.source.Len() {
			return 0, ErrModel
		}
		return i, nil
	}
	envelope := func(n *Node) bool {
		e := n.spec.Envelope
		return e.present && e.SourceToken() == index.source.Token() && e.start >= 0 && e.end >= e.start && e.end <= index.source.Len()
	}
	// lines returns the first and last physical line of a non-empty envelope.
	lines := func(n *Node) (int, int, bool, error) {
		if n == nil || !envelope(n) || n.spec.Envelope.end == n.spec.Envelope.start {
			return 0, 0, false, nil
		}
		a, e := locate(n.spec.Envelope.start)
		if e != nil {
			return 0, 0, false, e
		}
		z, e := locate(n.spec.Envelope.end - 1)
		return a, z, e == nil, e
	}
	// adjacent reports whether paragraph and its sibling table touch on
	// consecutive physical lines. Invalid envelopes are reported when the
	// nodes themselves are visited.
	adjacent := func(paragraph, table *Node) (bool, error) {
		pa, pz, ok, e := lines(paragraph)
		if !ok || e != nil {
			return false, e
		}
		ta, tz, ok, e := lines(table)
		if !ok || e != nil {
			return false, e
		}
		return pz+1 == ta || tz+1 == pa, nil
	}
	always := func(int) bool { return true }
	paragraphs := 0
	// line classifies the content of one physical line of the current Paragraph.
	line := func(i, start, end int, nextToTable bool) error {
		if e := charge(2*int64(end-start)+1, 0); e != nil {
			return e
		}
		shape, _ := classifyPipeRow(text, start, end, true, always, nil)
		if shape.separators == 0 {
			return nil
		}
		delimiter := shape.delimiter && (text[shape.start] == '|' || text[shape.start] == ':' || text[shape.start] == '-')
		return publish(PipeRowCandidate{Start: shape.start, End: shape.end, Paragraph: paragraphs, Line: i + 1, Cells: shape.cells, LeadingPipe: shape.leading, TrailingPipe: shape.trailing, Delimiter: delimiter, NextToTable: nextToTable})
	}
	// paragraph groups the Paragraph's content provenance by physical line. It
	// reports false when that provenance is missing or inconsistent.
	paragraph := func(n *Node, nextToTable bool) (bool, error) {
		if !envelope(n) || len(n.spec.Content) == 0 {
			return false, nil
		}
		current, start, end, previous := -1, 0, 0, n.spec.Envelope.start
		for _, part := range n.spec.Content {
			if e := charge(1, 0); e != nil {
				return false, e
			}
			s := part.Source
			if !s.Present() || s.source.Token() != index.source.Token() || s.start < previous || s.end < s.start || s.end > n.spec.Envelope.end {
				return false, nil
			}
			previous = s.end
			if s.start == s.end {
				continue
			}
			a, e := locate(s.start)
			if e != nil {
				return false, e
			}
			z, e := locate(s.end - 1)
			if e != nil {
				return false, e
			}
			for i := a; i <= z; i++ {
				if e := charge(1, 0); e != nil {
					return false, e
				}
				lo, hi := max(s.start, index.lines[i].start), min(s.end, index.lines[i].end)
				if lo >= hi {
					continue
				}
				if i == current {
					end = hi
					continue
				}
				if current >= 0 {
					if e := line(current, start, end, nextToTable); e != nil {
						return false, e
					}
				}
				current, start, end = i, lo, hi
			}
		}
		if current >= 0 {
			if e := line(current, start, end, nextToTable); e != nil {
				return false, e
			}
		}
		paragraphs++
		return true, nil
	}
	// Each pending Paragraph carries whether it touches a sibling Table.
	type pending struct {
		node        *Node
		nextToTable bool
	}
	var stack []pending
	push := func(n *Node, nextToTable bool) error {
		capacity, e := grow(len(stack), cap(stack))
		if e != nil {
			return e
		}
		if capacity != cap(stack) {
			next := make([]pending, len(stack), capacity)
			copy(next, stack)
			stack = next
		}
		if e = charge(1, 1); e != nil {
			return e
		}
		stack = append(stack, pending{n, nextToTable})
		return nil
	}
	if err = push(d.root, false); err != nil {
		return out, err
	}
	for {
		if err = charge(16, 0); err != nil {
			return out, err
		}
		if len(stack) == 0 {
			break
		}
		top := stack[len(stack)-1]
		n := top.node
		stack = stack[:len(stack)-1]
		if n != nil && n.Kind() == Paragraph {
			ok, e := paragraph(n, top.nextToTable)
			if e != nil {
				return out, e
			}
			if !ok {
				return PipeRowCandidatesInventory{Availability: Unavailable}, nil
			}
			continue
		}
		if n == nil || !envelope(n) {
			return out, ErrModel
		}
		switch n.Kind() {
		case DocumentKind, BlockQuote, List, ListItem:
		default:
			continue
		}
		children := n.spec.Children
		if err = charge(int64(len(children))+1, 0); err != nil {
			return out, err
		}
		for i := len(children) - 1; i >= 0; i-- {
			nextToTable := false
			if c := children[i]; c != nil && c.Kind() == Paragraph {
				for _, j := range [2]int{i - 1, i + 1} {
					if j < 0 || j >= len(children) || children[j] == nil || children[j].Kind() != Table || nextToTable {
						continue
					}
					if nextToTable, err = adjacent(c, children[j]); err != nil {
						return out, err
					}
				}
			}
			if err = push(children[i], nextToTable); err != nil {
				return out, err
			}
		}
	}
	return PipeRowCandidatesInventory{Rows: rows, Availability: Available}, nil
}
