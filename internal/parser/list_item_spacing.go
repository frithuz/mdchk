package parser

import (
	"context"
	"math/bits"
	"sort"
)

// ListItemSpacingFact identifies a mismatching direct sibling separator.
// Span is original diagnostic evidence, never edit authority. Counts include
// whole blank body lines only, after actual ancestor prefixes.
type ListItemSpacingFact struct {
	Span             Span
	Actual, Expected int
}

// ListItemSpacing projects exact sibling counts from the original production
// tree. It follows ListSpacing's source-floor traversal: definitions remain body
// participants, terminal lists/items propagate floors, opaque leaves and quotes
// retain their envelopes, and ancestor prefixes roll back before comparisons.
// Style is an admitted immutable scalar; each actual List owns its baseline.
func (d *Document) ListItemSpacing(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, style string) ([]ListItemSpacingFact, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	if err := headingFactCharge(ctx, budget, int64(len(style))+1, 0); err != nil {
		return nil, err
	}
	if style != "consistent" && style != "tight" && style != "loose" {
		return nil, ErrModel
	}

	type lineState struct{ last, prefix int }
	if err := headingFactCharge(ctx, budget, int64(len(index.lines))+1, int64(len(index.lines))); err != nil {
		return nil, err
	}
	lines := make([]lineState, len(index.lines))
	for i, line := range index.lines {
		if err := headingFactCharge(ctx, budget, int64(line.end-line.start)+1, 0); err != nil {
			return nil, err
		}
		lines[i] = lineState{last: line.start - 1, prefix: line.start}
		for j := line.start; j < line.end; j++ {
			if (j-line.start)&255 == 0 {
				if err := headingFactCharge(ctx, budget, 0, 0); err != nil {
					return nil, err
				}
			}
			if b := index.source.content[j]; b != ' ' && b != '\t' {
				lines[i].last = j
			}
		}
	}
	locate := func(offset int) (int, error) {
		if err := headingFactCharge(ctx, budget, int64(bits.Len(uint(len(index.lines))))+1, 0); err != nil {
			return 0, err
		}
		i := sort.Search(len(index.lines), func(i int) bool { return index.lines[i].start > offset }) - 1
		if i < 0 || offset >= index.lines[i].next && offset != index.lines[i].end {
			return 0, ErrModel
		}
		return i, nil
	}
	type participant struct {
		span          Span
		list          bool
		marker, floor int
	}
	type frame struct {
		node          *Node
		next, undo    int
		previous      participant
		marker, floor int
		entered       bool
		expected      int
	}
	type rollback struct{ line, prefix int }
	stack := make([]frame, 1)
	stack[0].node = d.root
	var undo []rollback
	var out []ListItemSpacingFact
	definitions := d.references.Definitions
	reference := 0
	// Explicit growth reserves storage and copied prefixes before allocation.
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
	accept := func(f *frame, current participant) error {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return err
		}

		previous := f.previous
		if f.node.Kind() == List && previous.span.Present() {
			last, err := locate(previous.floor - 1)
			if err != nil {
				return err
			}
			first, err := locate(current.marker)
			if err != nil {
				return err
			}
			count := 0
			var extra Span
			for i := last + 1; i < first; i++ {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return err
				}
				if lines[i].last < lines[i].prefix {
					if count == f.expected {
						extra = Span{source: index.source, start: lines[i].prefix, end: index.lines[i].next, present: true}
					}
					count++
				}
			}
			if f.expected < 0 {
				f.expected = count
			}
			if count != f.expected {
				span := extra
				if count < f.expected {
					span = Span{source: index.source, start: current.marker, end: current.marker, present: true}
				}
				if !span.Present() {
					return ErrModel
				}
				capacity, err := grow(len(out), cap(out))
				if err != nil {
					return err
				}
				if capacity != cap(out) {
					next := make([]ListItemSpacingFact, len(out), capacity)
					copy(next, out)
					out = next
				}
				if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
					return err
				}
				out = append(out, ListItemSpacingFact{Span: span, Actual: count, Expected: f.expected})
			}
		}
		f.previous = current
		f.floor = max(f.floor, current.floor)
		if f.marker < 0 && current.marker >= 0 {
			f.marker = current.marker
		}
		return nil
	}
	for len(stack) != 0 {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		f := &stack[len(stack)-1]
		if !f.entered {
			f.entered, f.undo, f.marker = true, len(undo), -1
			f.expected = -1
			if style == "tight" {
				f.expected = 0
			} else if style == "loose" {
				f.expected = 1
			}
			for _, marker := range f.node.spec.Markers {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return nil, err
				}
				if marker.Role == "item-marker" {
					f.marker = marker.Source.Start()
					f.floor = max(f.floor, marker.Source.End())
				}
				if marker.Role != "item-prefix" && marker.Role != "item-indent" && marker.Role != "quote-prefix" {
					continue
				}
				line, err := locate(marker.Source.Start())
				if err != nil {
					return nil, err
				}
				if marker.Source.End() > index.lines[line].end {
					return nil, ErrModel
				}
				capacity, err := grow(len(undo), cap(undo))
				if err != nil {
					return nil, err
				}
				if capacity != cap(undo) {
					next := make([]rollback, len(undo), capacity)
					copy(next, undo)
					undo = next
				}
				if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
					return nil, err
				}
				undo = append(undo, rollback{line: line, prefix: lines[line].prefix})
				lines[line].prefix = max(lines[line].prefix, marker.Source.End())
			}
		}
		end := f.node.spec.Envelope.End()
		var child *Node
		if f.next < len(f.node.spec.Children) {
			child = f.node.spec.Children[f.next]
			end = child.spec.Envelope.Start()
		}
		if reference < len(definitions) {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return nil, err
			}
			span := definitions[reference].spec.Envelope
			if span.Start() < end {
				if f.node.Kind() != DocumentKind && f.node.Kind() != BlockQuote && f.node.Kind() != ListItem || span.Start() < f.node.spec.Envelope.Start() || span.End() > f.node.spec.Envelope.End() {
					return nil, ErrModel
				}
				if err := accept(f, participant{span: span, marker: -1, floor: span.End()}); err != nil {
					return nil, err
				}
				reference++
				continue
			}
		}
		if child != nil {
			f.next++
			if child.Kind() == List || child.Kind() == ListItem || child.Kind() == BlockQuote {
				capacity, err := grow(len(stack), cap(stack))
				if err != nil {
					return nil, err
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
			} else {
				if reference < len(definitions) && definitions[reference].spec.Envelope.Start() < child.spec.Envelope.End() {
					return nil, ErrModel
				}
				if err := accept(f, participant{span: child.spec.Envelope, marker: -1, floor: child.spec.Envelope.End()}); err != nil {
					return nil, err
				}
			}
			continue
		}
		// Roll back before the parent comparison: the target's own syntax may not
		// disappear into a blank at its parent's body level.
		for len(undo) > f.undo {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return nil, err
			}
			r := undo[len(undo)-1]
			lines[r.line].prefix = r.prefix
			undo = undo[:len(undo)-1]
		}
		current := participant{span: f.node.spec.Envelope, list: f.node.Kind() == List, marker: f.marker, floor: f.floor}
		if f.node.Kind() == BlockQuote {
			current.floor = f.node.spec.Envelope.End()
		}
		if current.list && (current.marker < 0 || current.floor <= current.marker) {
			return nil, ErrModel
		}
		stack = stack[:len(stack)-1]
		if len(stack) != 0 {
			if err := accept(&stack[len(stack)-1], current); err != nil {
				return nil, err
			}
		}
	}
	if reference != len(definitions) {
		return nil, ErrModel
	}
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
