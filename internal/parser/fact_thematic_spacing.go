// Facts for MD058 and MD065.

package parser

import (
	"context"
	"math/bits"
	"sort"
)

// ThematicSpacingFact identifies one missing source-body separator. Points are
// detached original spans; they confer no edit or mutable syntax authority.
// At is the line boundary a blank line would be inserted at: the start of the
// break's line for a before side and the start of the next line for an after
// side. Prefix runs from At through that line's active ancestor container
// prefixes, Ending is the previous line's terminator and Neighbour is the whole
// line on the far side of At, with its ending. Loosens reports that the owning
// body is a list item whose list is not known to be loose, and Lazy that the
// neighbour carries fewer active quote markers than the target's line, as a lazy
// continuation does.
type ThematicSpacingFact struct {
	Point                         Span
	Before                        bool
	At, Prefix, Ending, Neighbour Span
	Loosens, Lazy                 bool
}

// TableSpacingFact identifies one missing source-body separator around a
// table. It shares the thematic break fact shape and its detached authority.
type TableSpacingFact = ThematicSpacingFact

// ThematicSpacing reports missing immediate physical separators around eligible
// thematic breaks in their real owning bodies. Any interruption withholds the
// complete inventory.
func (d *Document) ThematicSpacing(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) ([]ThematicSpacingFact, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	eligible, err := d.spacingTargets(ctx, budget, ThematicBreak)
	if err != nil {
		return nil, err
	}
	return d.bodySpacing(ctx, budget, index, options, eligible)
}

// TableSpacing reports missing immediate physical separators around eligible
// GFM tables in their real owning bodies, with ThematicSpacing's semantics.
// Any interruption withholds the complete inventory.
func (d *Document) TableSpacing(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) ([]TableSpacingFact, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	eligible, err := d.spacingTargets(ctx, budget, Table)
	if err != nil {
		return nil, err
	}
	return d.bodySpacing(ctx, budget, index, options, eligible)
}

// bodySpacing compares source participants around the eligible targets, in
// tree order. Removed definitions are merged once, in source order. Only
// ancestor prefixes are active during a comparison. Terminal list/item
// summaries propagate real content floors, while quotes and opaque blocks
// retain their entire boundary.
func (d *Document) bodySpacing(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, eligible []*Node) ([]ThematicSpacingFact, error) {
	target := 0
	type lineState struct{ last, prefix, quotes int }
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
		target        bool
		marker, floor int
	}
	type frame struct {
		node          *Node
		next, undo    int
		previous      participant
		marker, floor int
		entered       bool
	}
	type rollback struct{ line, prefix, quotes int }
	stack := make([]frame, 1)
	stack[0].node = d.root
	var undo []rollback
	var out []ThematicSpacingFact
	definitions := d.references.Definitions
	// Validate the borrowed source-order inventory before advancing its cursor.
	// The terminal probe is charged even when there are no definitions.
	for i := 0; ; i++ {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		if i == len(definitions) {
			break
		}
		span := definitions[i].spec.Envelope
		if !span.Present() || span.source.Token() != index.source.Token() || span.Start() >= span.End() || (i > 0 && span.Start() < definitions[i-1].spec.Envelope.End()) {
			return nil, ErrModel
		}
	}
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
	// emit records one side. boundary is the line receiving the insertion, neighbour
	// the line across from it and target the line of the target's own syntax.
	emit := func(f *frame, point int, before bool, boundary, neighbour, target int) error {
		capacity, err := grow(len(out), cap(out))
		if err != nil {
			return err
		}
		if capacity != cap(out) {
			next := make([]ThematicSpacingFact, len(out), capacity)
			copy(next, out)
			out = next
		}
		if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
			return err
		}
		fact := ThematicSpacingFact{Point: Span{source: index.source, start: point, end: point, present: true}, Before: before}
		if boundary > 0 && boundary < len(index.lines) && neighbour >= 0 && neighbour < len(index.lines) {
			line, previous, far := index.lines[boundary], index.lines[boundary-1], index.lines[neighbour]
			fact.At = Span{source: index.source, start: line.start, end: line.start, present: true}
			fact.Prefix = Span{source: index.source, start: line.start, end: max(line.start, lines[boundary].prefix), present: true}
			fact.Ending = Span{source: index.source, start: previous.end, end: previous.next, present: true}
			fact.Neighbour = Span{source: index.source, start: far.start, end: far.next, present: true}
			fact.Lazy = lines[neighbour].quotes < lines[target].quotes
		}
		if f.node.Kind() == ListItem {
			parent := stack[len(stack)-2].node.spec.Properties.List
			fact.Loosens = parent == nil || !parent.Tight.Present || parent.Tight.Value
		}
		out = append(out, fact)
		return nil
	}
	accept := func(f *frame, current participant) error {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return err
		}
		body := f.node.Kind() == DocumentKind || f.node.Kind() == BlockQuote || f.node.Kind() == ListItem
		previous := f.previous
		if body && previous.span.Present() && (previous.target || current.target) {
			last, err := locate(previous.floor - 1)
			if err != nil {
				return err
			}
			first, err := locate(current.span.Start())
			if err != nil {
				return err
			}
			// Check each target's immediate physical neighbor, never a distant
			// blank or a blank retained inside its neighboring participant.
			if previous.target {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return err
				}
				blank := last+1 < first && lines[last+1].last < lines[last+1].prefix
				if !blank {
					if err := emit(f, index.lines[last].end, false, last+1, last+1, last); err != nil {
						return err
					}
				}
			}
			if current.target {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return err
				}
				blank := first-1 > last && lines[first-1].last < lines[first-1].prefix
				if !blank {
					if err := emit(f, max(index.lines[first].start, lines[first].prefix), true, first, first-1, first); err != nil {
						return err
					}
				}
			}
		}
		f.previous = current
		f.floor = max(f.floor, current.floor)
		if f.marker < 0 && current.marker >= 0 {
			f.marker = current.marker
		}
		return nil
	}
	for {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, err
		}
		if len(stack) == 0 {
			break
		}
		f := &stack[len(stack)-1]
		if !f.entered {
			f.entered, f.undo, f.marker = true, len(undo), -1
			for _, marker := range f.node.spec.Markers {
				if err := headingFactCharge(ctx, budget, int64(len(marker.Role))+1, 0); err != nil {
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
				undo = append(undo, rollback{line: line, prefix: lines[line].prefix, quotes: lines[line].quotes})
				lines[line].prefix = max(lines[line].prefix, marker.Source.End())
				if marker.Role == "quote-prefix" {
					lines[line].quotes++
				}
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
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return nil, err
				}
				matched := target < len(eligible) && eligible[target] == child
				if matched {
					target++
				}
				if err := accept(f, participant{span: child.spec.Envelope, target: matched, marker: -1, floor: child.spec.Envelope.End()}); err != nil {
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
			lines[r.line].prefix, lines[r.line].quotes = r.prefix, r.quotes
			undo = undo[:len(undo)-1]
		}
		current := participant{span: f.node.spec.Envelope, marker: f.marker, floor: f.floor}
		if f.node.Kind() == BlockQuote {
			current.floor = f.node.spec.Envelope.End()
		}
		if f.node.Kind() == List && (current.marker < 0 || current.floor <= current.marker) {
			return nil, ErrModel
		}
		stack = stack[:len(stack)-1]
		if len(stack) != 0 {
			if err := accept(&stack[len(stack)-1], current); err != nil {
				return nil, err
			}
		}
	}
	if reference != len(definitions) || target != len(eligible) {
		return nil, ErrModel
	}
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}

// spacingTargets collects nodes of kind outside any open raw HTML
// lifetime. It follows the complete inline/tree/raw-tail order before body
// comparisons. Inline HTML in a Paragraph may protect a later block; Image
// alternatives save and restore the surrounding raw state. Borrowed node pointers
// remain private to this call and confer no AST authority.
func (d *Document) spacingTargets(ctx context.Context, budget *OperationBudget, kind Kind) ([]*Node, error) {
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
	type frame struct {
		node          *Node
		exit, restore bool
		raw           string
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
	var out []*Node
	raw := ""
	for {
		if err := charge(1, 0); err != nil {
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
			continue
		}
		if n.Kind() == kind && raw == "" {
			capacity, err := grow(len(out), cap(out))
			if err != nil {
				return nil, err
			}
			if capacity != cap(out) {
				next := make([]*Node, len(out), capacity)
				copy(next, out)
				out = next
			}
			if err := charge(1, 1); err != nil {
				return nil, err
			}
			out = append(out, n)
		}
		if err := charge(1, 0); err != nil {
			return nil, err
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
			exit.restore, exit.raw = true, raw
			raw = ""
		}
		if err := push(exit); err != nil {
			return nil, err
		}
		// Reserve both child-group choices and the terminal group probe.
		if err := charge(3, 0); err != nil {
			return nil, err
		}
		for group := 0; group < 2; group++ {
			children := n.htmlTail
			if group == 1 {
				children = n.spec.Children
			}
			for i := len(children) - 1; ; i-- {
				if err := charge(1, 0); err != nil {
					return nil, err
				}
				if i < 0 {
					break
				}
				if err := push(frame{node: children[i]}); err != nil {
					return nil, err
				}
			}
		}
	}
	return out, nil
}
