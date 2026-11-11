package parser

import (
	"context"
	"errors"
	"unicode/utf8"
)

// ASCIIPunctuationFact identifies one of the six literal source scalars. It
// grants no correction authority and retains only its original source range.
type ASCIIPunctuationFact struct {
	Span   Span
	Scalar rune
}

// ASCIIPunctuation projects actual prose Exact text under the completed original
// parse. Links are opaque for findings but retain global raw transitions; images
// are opaque to both. No public tree, source, index or allowance is copied.
func (d *Document) ASCIIPunctuation(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out []ASCIIPunctuationFact, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = nil
		}
	}()
	charge := func(w, n int64) error { return headingFactCharge(ctx, budget, w, n) }
	grow := func(length, capacity int) (int, error) {
		if e := charge(1, 0); e != nil {
			return 0, e
		}
		if length < capacity {
			return capacity, nil
		}
		if capacity > int(^uint(0)>>1)/2 {
			return 0, ErrModel
		}
		capacity = max(1, capacity*2)
		if e := charge(int64(length)+1, int64(capacity)); e != nil {
			return 0, e
		}
		return capacity, nil
	}
	type frame struct {
		node        *Node
		prose, link bool
	}
	if err = charge(1, 2); err != nil {
		return nil, err
	}
	stack := make([]frame, 1)
	stack[0].node = d.root
	push := func(f frame) error {
		if e := charge(1, 1); e != nil {
			return e
		}
		capacity, e := grow(len(stack), cap(stack))
		if e != nil {
			return e
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
		if err = charge(4, 0); err != nil {
			return nil, err
		}
		if len(stack) == 0 {
			break
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := f.node
		if err = charge(16, 0); err != nil {
			return nil, err
		}
		if n == nil || !contains(d.root.spec.Envelope, n.spec.Envelope) {
			return nil, ErrModel
		}
		prose, link := f.prose, f.link
		switch n.Kind() {
		case Paragraph, Heading:
			prose = true
		case TableCell:
			prose = n.spec.Properties.Cell != nil && !n.spec.Properties.Cell.Alignment.Present
		case DocumentKind, BlockQuote, List, ListItem, Table, TableRow, CodeBlock, HTMLBlock:
			prose = false
		case Link:
			link = true
		case CodeSpan:
			prose = false
		case Image:
			continue // Neither children, tails nor image-owned raw state escape.
		case Text, Emphasis, Strong, Strike, InlineHTML, SoftBreak, HardBreak, ThematicBreak:
		default:
			return nil, ErrModel
		}
		if n.Kind() == Text && prose && !link && raw == "" {
			if err = charge(2, 0); err != nil {
				return nil, err
			}
			if !n.spec.Properties.Literal.Present || len(n.spec.Content) == 0 {
				return nil, ErrModel
			}
			previousStart, previousEnd := n.spec.Envelope.start, n.spec.Envelope.start
			previousPartial := false
			for i := 0; ; i++ {
				if err = charge(16, 0); err != nil {
					return nil, err
				}
				if i == len(n.spec.Content) {
					break
				}
				p := n.spec.Content[i]
				if !p.Semantic.Present || !contains(n.spec.Envelope, p.Source) || p.Source.start < previousStart || p.Source.end < previousEnd || p.Source.start < previousEnd && !(previousPartial && p.Transformation == PartialTab && p.Source.start == previousStart && p.Source.end == previousEnd) {
					return nil, ErrModel
				}
				previousStart, previousEnd = p.Source.start, p.Source.end
				previousPartial = p.Transformation == PartialTab
				switch p.Transformation {
				case Exact, Escape, Entity, Normalized, PartialTab:
				default:
					return nil, ErrModel
				}
				if p.Transformation != Exact {
					continue
				}
				if err = charge(2, 0); err != nil {
					return nil, err
				}
				if len(p.Semantic.Value) != p.Source.end-p.Source.start {
					return nil, ErrModel
				}
				for j := 0; ; j++ {
					if err = charge(2, 0); err != nil {
						return nil, err
					}
					if j == len(p.Semantic.Value) {
						break
					}
					if p.Semantic.Value[j] != index.source.content[p.Source.start+j] {
						return nil, ErrModel
					}
				}
				for offset := 0; ; {
					// Reserve 64 units for the standard library UTF-8 decoder
					// (at most four bytes, fixed branches/bit operations) and
					// provenance, plus 32 for six comparisons, offsets, errors,
					// terminal/bounds checks and range bookkeeping. No table search.
					if err = charge(96, 0); err != nil {
						return nil, err
					}
					if offset == len(p.Semantic.Value) {
						break
					}
					r, width := utf8.DecodeRuneInString(p.Semantic.Value[offset:])
					if r == utf8.RuneError && width == 1 {
						return nil, ErrModel
					}
					at := p.Source.start + offset
					offset += width
					if r != '‘' && r != '’' && r != '“' && r != '”' && r != '–' && r != '—' {
						continue
					}
					capacity, e := grow(len(out), cap(out))
					if e != nil {
						return nil, e
					}
					if capacity != cap(out) {
						next := make([]ASCIIPunctuationFact, len(out), capacity)
						copy(next, out)
						out = next
					}
					if err = charge(1, 1); err != nil {
						return nil, err
					}
					out = append(out, ASCIIPunctuationFact{Span: Span{source: index.source, start: at, end: at + width, present: true}, Scalar: r})
				}
			}
		}
		if err = charge(1+2*int64(len(n.rawTag))+int64(len(raw)), 0); err != nil {
			return nil, err
		}
		if n.rawTag != "" {
			if raw == "" && !n.rawClosing {
				raw = n.rawTag
			} else if raw == n.rawTag && n.rawClosing {
				raw = ""
			}
		}
		for group := 0; ; group++ {
			if err = charge(1, 0); err != nil {
				return nil, err
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
					return nil, err
				}
				if i == 0 {
					break
				}
				i--
				if err = push(frame{node: children[i], prose: prose, link: link}); err != nil {
					return nil, err
				}
			}
		}
	}
	return out, nil
}
