package parser

import (
	"context"
	"unicode"
	"unicode/utf8"
)

// RepeatedProseSpaces borrows only the completed original production tree and
// allowance. Semantic tokens witness words; only contiguous exact authored SPACE
// contributions supply counts. No rendered text, tree or correction is retained.
func (d *Document) RepeatedProseSpaces(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, maximum int) ([]Span, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	if maximum < 1 {
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
	// At most one pending run waits for the complete immediately following token.
	// Formatting edges preserve token evidence but never merge source SPACE runs.
	var run, pending Span
	left, word := false, false
	publish := func() error {
		if err := charge(1, 0); err != nil {
			return err
		}
		if pending.Present() && word {
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
			out = append(out, pending)
		}
		pending = Span{}
		return nil
	}
	boundary := func() error {
		if err := publish(); err != nil {
			return err
		}
		run = Span{}
		left = false
		word = false
		return nil
	}
	finishRun := func() error {
		if err := charge(1, 0); err != nil {
			return err
		}
		if !run.Present() {
			return nil
		}
		if left && run.end-run.start > maximum {
			// Probe both original neighbors even when the run ends at a Text edge.
			// A partial ownership fragment of a longer physical run is never eligible.
			if err := charge(2, 0); err != nil {
				return err
			}
			if (run.start == 0 || index.source.content[run.start-1] != ' ') && (run.end == index.source.Len() || index.source.content[run.end] != ' ') {
				pending = run
				pending.start += maximum
			}
		}
		run = Span{}
		left = false
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
			if part.Transformation == Exact && len(part.Semantic.Value) != part.Source.end-part.Source.start {
				return ErrModel
			}
			for offset := 0; offset < len(part.Semantic.Value); {
				// UTF-8 decode (up to four bytes), association, original NUL probe and
				// bounded Unicode predicates are reserved before classification.
				if err := charge(12, 0); err != nil {
					return err
				}
				r, width := utf8.DecodeRuneInString(part.Semantic.Value[offset:])
				at := part.Source.start + offset
				exact := part.Transformation == Exact
				nul := part.Transformation == Normalized && part.Source.end-part.Source.start == 1 && index.source.content[part.Source.start] == 0
				offset += width
				if exact && r == ' ' {
					if run.Present() && run.end == at {
						run.end++
						continue
					}
					adjacentWhitespace := run.Present()
					if err := publish(); err != nil {
						return err
					}
					run = Span{source: index.source, start: at, end: at + 1, present: true}
					left = word && !adjacentWhitespace
					word = false
					continue
				}
				if err := finishRun(); err != nil {
					return err
				}
				if nul || unicode.IsSpace(r) || unicode.Is(unicode.Cc, r) {
					if err := boundary(); err != nil {
						return err
					}
				} else {
					word = word || unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r)
				}
			}
		}
		return nil
	}
	type frame struct {
		node        *Node
		prose, exit bool
		restore     bool
		raw         string
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
			if n.Kind() == Paragraph || n.Kind() == Heading {
				if err := boundary(); err != nil {
					return nil, err
				}
			}
			continue
		}
		prose := f.prose
		switch n.Kind() {
		case Paragraph, Heading:
			if err := boundary(); err != nil {
				return nil, err
			}
			prose = true
		case DocumentKind, BlockQuote, List, ListItem, Table, TableRow, TableCell, CodeBlock, HTMLBlock:
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
