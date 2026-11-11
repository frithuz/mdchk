package parser

import (
	"context"
	"unicode"
	"unicode/utf8"
)

// CJKSpacing borrows completed original production Text contributions and the
// original allowance, returning detached zero-width insertion spans. It uses
// Unicode 17 Han intersect Ideographic, Latin intersect Letter and ASCII digits.
// Only physically adjacent Exact scalars within one prose owner participate.
func (d *Document) CJKSpacing(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) ([]Span, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	if unicode.Version != "17.0.0" {
		return nil, ErrModel
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
	var out []Span
	// One scalar of adjacency state, never retained beyond this invocation.
	previous, previousEnd := byte(0), -1
	boundary := func() error {
		if err := charge(1, 0); err != nil {
			return err
		}
		previous, previousEnd = 0, -1
		return nil
	}
	text := func(n *Node) error {
		for _, part := range n.spec.Content {
			if err := charge(1, 0); err != nil {
				return err
			}
			if !part.Semantic.Present || !part.Source.Present() || part.Source.source.Token() != index.source.Token() {
				return ErrModel
			}
			switch part.Transformation {
			case Exact, Escape, Entity, Normalized, PartialTab:
			default:
				return ErrModel
			}
			if part.Transformation != Exact {
				if err := boundary(); err != nil {
					return err
				}
				continue
			}
			if len(part.Semantic.Value) != part.Source.end-part.Source.start {
				return ErrModel
			}
			// Association equality is checked before decoding, with its full byte
			// comparison reserved. No transformed contribution is decoded again.
			if err := charge(int64(len(part.Semantic.Value))+1, 0); err != nil {
				return err
			}
			if index.source.content[part.Source.start:part.Source.end] != part.Semantic.Value {
				return ErrModel
			}
			for offset := 0; ; {
				// Includes the unsuccessful terminating probe. 256 reserves UTF-8
				// decoding, source association and all four Unicode table searches:
				// each table uses at most 32 binary comparisons on admitted slices.
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
				at := part.Source.start + offset
				class := byte(0)
				if unicode.Is(unicode.Han, r) && unicode.Is(unicode.Properties["Ideographic"], r) {
					class = 1
				} else if r >= '0' && r <= '9' || unicode.Is(unicode.Latin, r) && unicode.IsLetter(r) {
					class = 2
				}
				if previousEnd == at && previous != 0 && class != 0 && previous != class {
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
					out = append(out, Span{source: index.source, start: at, end: at, present: true})
				}
				previous, previousEnd = class, at+width
				offset += width
			}
		}
		return charge(1, 0) // unsuccessful terminating contribution probe
	}
	type frame struct {
		node        *Node
		prose, exit bool
		restore     bool
		raw         string
	}
	if err := charge(1, 3); err != nil {
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
	for len(stack) > 0 {
		if err := charge(8, 0); err != nil {
			return nil, err
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := f.node
		if f.exit {
			if f.restore {
				raw = f.raw
			}
			if n.Kind() == Paragraph || n.Kind() == Heading || n.Kind() == TableCell {
				if err := boundary(); err != nil {
					return nil, err
				}
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
			if n.Kind() == Link {
				if err := charge(18, 0); err != nil {
					return nil, err
				}
				if n.spec.Properties.Link == nil {
					return nil, ErrModel
				}
				origin := n.spec.Properties.Link.Origin
				if origin == "autolink" || origin == "bare-gfm" {
					if err := boundary(); err != nil {
						return nil, err
					}
					prose = false
				}
			}
			if len(n.spec.Children) == 0 {
				if err := boundary(); err != nil {
					return nil, err
				}
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
			// Reserve group selection and failed terminating child probe too.
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
