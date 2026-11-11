package parser

import "context"

// CodeSpanSpaces returns detached original excess ASCII SPACE ranges at actual
// code-span boundaries. It borrows completed production syntax and its original
// allowance; normalized Literal decides padding policy, never source geometry.
func (d *Document) CodeSpanSpaces(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) ([]Span, error) {
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
	var out []Span
	publish := func(start, end int) error {
		if err := charge(1, 0); err != nil {
			return err
		}
		if start == end {
			return nil
		}
		capacity, err := grow(len(out), cap(out))
		if err != nil {
			return err
		}
		if capacity != cap(out) {
			next := make([]Span, len(out), capacity)
			copy(next, out)
			out = next
		}
		if err := charge(1, 1); err != nil {
			return err
		}
		out = append(out, Span{source: index.source, start: start, end: end, present: true})
		return nil
	}
	span := func(n *Node) error {
		if err := charge(4, 0); err != nil {
			return err
		}
		literal := n.spec.Properties.Literal
		if !literal.Present || len(n.spec.Content) == 0 || len(n.spec.Markers) != 2 {
			return ErrModel
		}
		var opening, closing Span
		for _, m := range n.spec.Markers {
			if err := charge(2*int64(len(m.Role))+1, 0); err != nil {
				return err
			}
			switch m.Role {
			case "opening":
				opening = m.Source
			case "closing":
				closing = m.Source
			default:
				return ErrModel
			}
		}
		if err := charge(8, 0); err != nil {
			return err
		}
		if !opening.Present() || !closing.Present() || opening.SourceToken() != index.source.Token() || closing.SourceToken() != index.source.Token() || opening.end > closing.start {
			return ErrModel
		}
		previous := opening.end
		for _, part := range n.spec.Content {
			if err := charge(6, 0); err != nil {
				return err
			}
			if !part.Source.Present() || part.Source.SourceToken() != index.source.Token() || !part.Semantic.Present || part.Source.start < previous || part.Source.end > closing.start {
				return ErrModel
			}
			previous = part.Source.end
		}
		// Ending normalization and one-byte trimming may mark an entire retained
		// segment Normalized. Its original SPACE bytes still own padding. Atomic
		// pipe escapes, partial tabs and other transformed contributions cannot.
		edge := func(first bool) (int, error) {
			at := opening.end
			if !first {
				at = closing.start
			}
			for j := 0; ; j++ {
				if err := charge(2, 0); err != nil {
					return 0, err
				}
				if j == len(n.spec.Content) {
					return at, nil
				}
				i := j
				if !first {
					i = len(n.spec.Content) - 1 - j
				}
				p := n.spec.Content[i]
				if err := charge(4, 0); err != nil {
					return 0, err
				}
				if p.Transformation != Exact && p.Transformation != Normalized || first && p.Source.start != at || !first && p.Source.end != at {
					return at, nil
				}
				for {
					// Includes failed byte/end probes and bounds cancellation latency.
					if err := charge(3, 0); err != nil {
						return 0, err
					}
					end := p.Source.end
					if !first {
						end = p.Source.start
					}
					if at == end {
						break
					}
					pos := at
					if !first {
						pos--
					}
					if index.source.content[pos] != ' ' {
						return at, nil
					}
					if first {
						at++
					} else {
						at--
					}
				}
			}
		}
		left, err := edge(true)
		if err != nil {
			return err
		}
		right, err := edge(false)
		if err != nil {
			return err
		}
		if err := charge(3, 0); err != nil {
			return err
		}
		if left == opening.end || right == closing.start {
			return nil // Every one-sided run is meaningful, whatever its length.
		}
		allSpaces := true
		for i := 0; ; i++ {
			if err := charge(2, 0); err != nil {
				return err
			}
			if i == len(literal.Value) {
				break
			}
			if literal.Value[i] != ' ' {
				allSpaces = false
				break
			}
		}
		if allSpaces {
			return nil
		}
		if err := charge(5, 0); err != nil {
			return err
		}
		first, last := literal.Value[0], literal.Value[len(literal.Value)-1]
		if first == ' ' || last == ' ' || first == '`' || last == '`' {
			left-- // Reserve the innermost original byte on EACH edge.
			right++
		}
		if err := publish(opening.end, left); err != nil {
			return err
		}
		return publish(right, closing.start)
	}
	type frame struct {
		node           *Node
		prose, restore bool
		raw            string
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
	raw := ""
	for {
		if err := charge(8, 0); err != nil {
			return nil, err
		}
		if len(stack) == 0 {
			break
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if f.restore {
			raw = f.raw
			continue
		}
		n := f.node
		prose := f.prose
		switch n.Kind() {
		case Paragraph, Heading:
			prose = true
		case TableCell:
			if err := charge(1, 0); err != nil {
				return nil, err
			}
			prose = n.spec.Properties.Cell != nil && !n.spec.Properties.Cell.Alignment.Present
		case DocumentKind, BlockQuote, List, ListItem, Table, TableRow, CodeBlock, HTMLBlock:
			prose = false
		case Link:
			if err := charge(18, 0); err != nil {
				return nil, err
			}
			if n.spec.Properties.Link == nil {
				return nil, ErrModel
			}
			origin := n.spec.Properties.Link.Origin
			prose = prose && origin != "autolink" && origin != "bare-gfm"
		case CodeSpan:
			if prose && raw == "" {
				if err := span(n); err != nil {
					return nil, err
				}
			}
			continue // Code payload is never Markdown children.
		default:
			// Other inline kinds inherit their actual Markdown owner.
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
		if n.Kind() == Image {
			if err := push(frame{restore: true, raw: raw}); err != nil {
				return nil, err
			}
			prose = prose && raw == ""
			raw = ""
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
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return nil, err
	}
	return out, nil
}
