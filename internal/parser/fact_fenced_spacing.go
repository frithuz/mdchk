// Facts for MD031.

package parser

import (
	"context"
	"errors"
)

// FencedSpacingFact identifies one missing source-body separator. Points are
// detached original spans; they confer no edit or mutable syntax authority.
type FencedSpacingFact struct {
	Point  Span
	Before bool
}

// FencedSpacingInventory owns detached points. Unknown necessary ownership
// withholds the entire inventory; absence is proved only by Available.
type FencedSpacingInventory struct {
	Facts        []FencedSpacingFact
	Availability Availability
}

// FencedSpacing reports missing immediate physical separators around eligible
// fenced blocks in their real owning bodies. It compares source participants.
// Removed definitions are merged once, in source order. Only ancestor prefixes
// are active during a comparison. Terminal list/item summaries propagate real
// content floors, while quotes and opaque blocks retain their entire boundary.
// Any interruption withholds the complete inventory.
func (d *Document) FencedSpacing(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out FencedSpacingInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = FencedSpacingInventory{}
		} else if out.Availability != Available {
			out.Facts = nil
		}
	}()
	eligible, available, err := d.fencedSpacingTargets(ctx, budget, index)
	if err != nil {
		return out, err
	}
	if !available {
		return FencedSpacingInventory{Availability: Unavailable}, nil
	}
	if len(eligible) == 0 {
		return FencedSpacingInventory{Availability: Available}, nil
	}
	facts, err := d.fencedSpacingBodies(ctx, budget, index, eligible)
	if err != nil {
		return out, err
	}
	return FencedSpacingInventory{Facts: facts, Availability: Available}, nil
}

// Bodies borrow actual source participants, not rendered children alone.
func (d *Document) fencedSpacingBodies(ctx context.Context, budget *OperationBudget, index LineIndex, eligible []*Node) ([]FencedSpacingFact, error) {
	target := 0
	type lineState struct{ last, prefix int }
	if err := headingFactCharge(ctx, budget, int64(len(index.lines))+1, int64(len(index.lines))); err != nil {
		return nil, err
	}
	lines := make([]lineState, len(index.lines))
	for i, line := range index.lines {
		if err := headingFactCharge(ctx, budget, 3*int64(line.end-line.start)+1, 0); err != nil {
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
		return fencedSpacingLine(ctx, budget, index, offset)
	}
	type participant struct {
		span          Span
		fenced        bool
		marker, floor int
	}
	type frame struct {
		node          *Node
		next, undo    int
		previous      participant
		marker, floor int
		entered       bool
	}
	type rollback struct{ line, prefix int }
	if err := headingFactCharge(ctx, budget, 2, 1); err != nil {
		return nil, err
	}
	stack := make([]frame, 1)
	stack[0].node = d.root
	var undo []rollback
	var out []FencedSpacingFact
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
		if err := headingFactCharge(ctx, budget, 16, 0); err != nil {
			return nil, err
		}
		if definitions[i] == nil {
			return nil, ErrModel
		}
		span := definitions[i].spec.Envelope
		if !span.Present() || span.source.Token() != index.source.Token() || span.Start() < 0 || span.Start() >= span.End() || span.End() > index.source.Len() || (i > 0 && span.Start() < definitions[i-1].spec.Envelope.End()) {
			return nil, ErrModel
		}
	}
	reference := 0
	// Explicit growth reserves storage and copied prefixes before allocation.
	grow := func(length, capacity int) (int, error) {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return 0, err
		}
		if length < capacity {
			return capacity, nil
		}
		if capacity > int(^uint(0)>>1)/2 {
			return 0, ErrModel
		}
		capacity = max(1, capacity*2)
		if err := headingFactCharge(ctx, budget, int64(length)+int64(capacity)+1, int64(capacity)); err != nil {
			return 0, err
		}
		return capacity, nil
	}
	emit := func(point int, before bool) error {
		capacity, err := grow(len(out), cap(out))
		if err != nil {
			return err
		}
		if capacity != cap(out) {
			next := make([]FencedSpacingFact, len(out), capacity)
			copy(next, out)
			out = next
		}
		if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
			return err
		}
		out = append(out, FencedSpacingFact{Point: Span{source: index.source, start: point, end: point, present: true}, Before: before})
		return nil
	}
	accept := func(f *frame, current participant) error {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return err
		}
		body := f.node.Kind() == DocumentKind || f.node.Kind() == BlockQuote || f.node.Kind() == ListItem
		previous := f.previous
		if body && previous.span.Present() && (previous.fenced || current.fenced) {
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
			if previous.fenced {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return err
				}
				blank := last+1 < first && lines[last+1].last < lines[last+1].prefix
				if !blank {
					if err := emit(index.lines[last].end, false); err != nil {
						return err
					}
				}
			}
			if current.fenced {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return err
				}
				blank := first-1 > last && lines[first-1].last < lines[first-1].prefix
				if !blank {
					if err := emit(max(index.lines[first].start, lines[first].prefix), true); err != nil {
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
			for i := 0; ; i++ {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return nil, err
				}
				if i == len(f.node.spec.Markers) {
					break
				}
				marker := f.node.spec.Markers[i]
				if err := headingFactCharge(ctx, budget, 4*int64(len(marker.Role))+16, 0); err != nil {
					return nil, err
				}
				if !marker.Source.present || marker.Source.SourceToken() != index.source.Token() || marker.Source.start < f.node.spec.Envelope.start || marker.Source.end < marker.Source.start || marker.Source.end > f.node.spec.Envelope.end {
					return nil, ErrModel
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
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return nil, err
				}
				fenced := target < len(eligible) && eligible[target] == child
				if fenced {
					target++
				}
				if err := accept(f, participant{span: child.spec.Envelope, fenced: fenced, marker: -1, floor: child.spec.Envelope.End()}); err != nil {
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
	return out, nil
}

// Select actual code owners first, then resolve raw lifetimes only through the
// final possible target. Unknown unrelated HTML after that point is irrelevant.
func (d *Document) fencedSpacingTargets(ctx context.Context, budget *OperationBudget, index LineIndex) ([]*Node, bool, error) {
	charge := func(work, nodes int64) error { return headingFactCharge(ctx, budget, work, nodes) }
	walk := shellPromptWalk{ctx: ctx, budget: budget, source: index.source, root: d.root}
	if err := walk.push(d.root); err != nil {
		return nil, false, err
	}
	var candidates []*Node
	for {
		n, err := walk.next()
		if err != nil {
			return nil, false, err
		}
		if n == nil {
			break
		}
		if err := charge(1, 0); err != nil {
			return nil, false, err
		}
		if n.spec.Kind != CodeBlock {
			continue
		}
		if err := charge(16, 0); err != nil {
			return nil, false, err
		}
		code := n.spec.Properties.Code
		if code != nil {
			if err := charge(2*int64(len(code.Style))+2, 0); err != nil {
				return nil, false, err
			}
			if code.Style == "indented" {
				continue
			}
			if code.Style != "fenced" {
				return nil, false, ErrModel
			}
		}
		candidates, err = shellPromptGrow(ctx, budget, candidates)
		if err != nil {
			return nil, false, err
		}
		if err := charge(1, 1); err != nil {
			return nil, false, err
		}
		candidates = append(candidates, n)
	}
	if err := charge(1, 0); err != nil {
		return nil, false, err
	}
	if len(candidates) == 0 {
		return nil, true, nil
	}
	if err := walk.push(d.root); err != nil {
		return nil, false, err
	}
	raw := conflictRawState{}
	var htmlWork *parserContext
	next, kept := 0, 0
	for {
		if err := charge(1, 0); err != nil {
			return nil, false, err
		}
		if next == len(candidates) {
			break
		}
		n, err := walk.next()
		if err != nil {
			return nil, false, err
		}
		if n == nil {
			return nil, false, ErrModel
		}
		if err := charge(2, 0); err != nil {
			return nil, false, err
		}
		if n.spec.Kind == Image {
			continue
		}
		if n.spec.Kind == HTMLBlock {
			if htmlWork == nil {
				if err := charge(2, 2); err != nil {
					return nil, false, err
				}
				htmlWork = &parserContext{Context: ctx, budget: &parseBudget{operation: budget}}
			}
			var available bool
			raw, _, available, err = conflictHTMLRaw(htmlWork, index.source, n, raw)
			if err != nil || !available {
				return nil, false, err
			}
			continue
		}
		if err := charge(int64(len(raw.name))+int64(len(n.rawTag))+1, 0); err != nil {
			return nil, false, err
		}
		if n.rawTag != "" {
			if raw.name == "" && !n.rawClosing {
				raw = conflictRawState{name: n.rawTag, start: n.spec.Envelope.start}
			} else if raw.name == n.rawTag && n.rawClosing {
				raw = conflictRawState{}
			}
		}
		if err := charge(1, 0); err != nil {
			return nil, false, err
		}
		if n != candidates[next] {
			continue
		}
		next++
		if raw.name != "" {
			continue
		}
		if n.spec.Properties.Code == nil {
			return nil, false, nil
		}
		if err := fencedSpacingMarkers(ctx, budget, index, n); err != nil {
			return nil, false, err
		}
		// Compact only already-consumed entries; future candidate pointers stay
		// intact, and this private scratch cannot mutate the original tree.
		if err := charge(1, 0); err != nil {
			return nil, false, err
		}
		candidates[kept] = n
		kept++
	}
	return candidates[:kept], true, nil
}

func fencedSpacingLine(ctx context.Context, budget *OperationBudget, index LineIndex, offset int) (int, error) {
	lo, hi := 0, len(index.lines)
	for {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return 0, err
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
	if err := headingFactCharge(ctx, budget, 8, 0); err != nil {
		return 0, err
	}
	if lo == 0 || offset < 0 || offset > index.source.Len() {
		return 0, ErrModel
	}
	line := index.lines[lo-1]
	if offset >= line.next && offset != line.end {
		return 0, ErrModel
	}
	return lo - 1, nil
}

// Marker validation borrows original fence syntax, without interpreting payload
// or requiring Info.Semantic: all actual fenced languages are eligible.
func fencedSpacingMarkers(ctx context.Context, budget *OperationBudget, index LineIndex, n *Node) error {
	charge := func(work int64) error { return headingFactCharge(ctx, budget, work, 0) }
	if err := charge(16); err != nil {
		return err
	}
	code := n.spec.Properties.Code
	if code == nil || !code.Fence.Present || code.Fence.Value != '`' && code.Fence.Value != '~' || !code.Width.Present || code.Width.Value < 3 {
		return ErrModel
	}
	first, err := fencedSpacingLine(ctx, budget, index, n.spec.Envelope.start)
	if err != nil {
		return err
	}
	last, err := fencedSpacingLine(ctx, budget, index, n.spec.Envelope.end-1)
	if err != nil {
		return err
	}
	var opening, closing Span
	for i := 0; ; i++ {
		if err := charge(1); err != nil {
			return err
		}
		if i == len(n.spec.Markers) {
			break
		}
		m := n.spec.Markers[i]
		if err := charge(3*int64(len(m.Role)) + 16); err != nil {
			return err
		}
		if !m.Source.present || m.Source.SourceToken() != index.source.Token() || m.Source.start < n.spec.Envelope.start || m.Source.end < m.Source.start || m.Source.end > n.spec.Envelope.end {
			return ErrModel
		}
		switch m.Role {
		case "opening":
			if opening.present {
				return ErrModel
			}
			opening = m.Source
		case "closing":
			if closing.present {
				return ErrModel
			}
			closing = m.Source
		case "indent-prefix":
			continue
		default:
			return ErrModel
		}
		for at := m.Source.start; ; at++ {
			if err := charge(1); err != nil {
				return err
			}
			if at == m.Source.end {
				break
			}
			if index.source.content[at] != code.Fence.Value {
				return ErrModel
			}
		}
	}
	if err := charge(32); err != nil {
		return err
	}
	if !opening.present || opening.start < index.lines[first].start || opening.end > index.lines[first].end || opening.end-opening.start != code.Width.Value {
		return ErrModel
	}
	if closing.present && (closing.start < index.lines[last].start || closing.end > index.lines[last].end || closing.end-closing.start < code.Width.Value || closing.start <= opening.end || n.spec.Envelope.end != index.lines[last].next) {
		return ErrModel
	}
	return nil
}
