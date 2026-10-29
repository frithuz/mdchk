package parser

import "context"

// ThematicStyle returns detached original marker envelopes whose style differs
// from the global first eligible pattern or the selected marker family. It
// borrows completed production syntax, source and the original finite allowance.
func (d *Document) ThematicStyle(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, style string) ([]Span, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	charge := func(work, nodes int64) error { return headingFactCharge(ctx, budget, work, nodes) }
	if err := charge(int64(len(style))+1, 1); err != nil {
		return nil, err
	}
	var family byte
	switch style {
	case "consistent":
	case "dash":
		family = '-'
	case "asterisk":
		family = '*'
	case "underscore":
		family = '_'
	default:
		return nil, ErrModel
	}
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
	var out []Span
	var baseline Span // Immutable borrowed original range, never normalized/copied.
	raw := ""
	for len(stack) > 0 {
		if err := charge(1, 0); err != nil {
			return nil, err
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
		if n.Kind() == ThematicBreak && raw == "" {
			var first, last Span
			for _, marker := range n.spec.Markers {
				// Role comparison and original source association precede use.
				if err := charge(int64(len(marker.Role))+2, 0); err != nil {
					return nil, err
				}
				if marker.Role != "thematic-marker" {
					continue
				}
				s := marker.Source
				if !s.Present() || s.source.Token() != index.source.Token() || s.start >= s.end || (last.Present() && s.start < last.end) {
					return nil, ErrModel
				}
				if !first.Present() {
					first = s
				}
				last = s
			}
			if !first.Present() || !last.Present() {
				return nil, ErrModel
			}
			if err := charge(3, 1); err != nil {
				return nil, err
			}
			pattern := Span{source: index.source, start: first.start, end: last.end, present: true}
			mismatch := false
			if family != 0 {
				if err := charge(1, 0); err != nil {
					return nil, err
				}
				mismatch = index.source.content[pattern.start] != family
			} else if !baseline.Present() {
				baseline = pattern
			} else {
				if err := charge(1, 0); err != nil {
					return nil, err
				}
				mismatch = pattern.end-pattern.start != baseline.end-baseline.start
				if !mismatch {
					for i := 0; i < pattern.end-pattern.start; i++ {
						// One checkpoint per pair, including long equal patterns.
						if err := charge(3, 0); err != nil {
							return nil, err
						}
						if index.source.content[pattern.start+i] != index.source.content[baseline.start+i] {
							mismatch = true
							break
						}
					}
				}
			}
			if mismatch {
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
				out = append(out, pattern)
			}
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
		for group := 0; group < 2; group++ {
			children := n.htmlTail
			if group == 1 {
				children = n.spec.Children
			}
			for i := len(children) - 1; i >= 0; i-- {
				if err := push(frame{node: children[i]}); err != nil {
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
