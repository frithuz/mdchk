package parser

import (
	"context"
	"errors"
	"unicode"
	"unicode/utf8"
)

// ParagraphEmphasisFact is a detached aggregate over one actual Paragraph.
// Content is the original first-to-last Content hull, absent for empty Content.
// Counts use eligible semantic scalars, never markup/source widths or glyphs.
type ParagraphEmphasisFact struct {
	Content           Span
	Total, Emphasized int
}

// ParagraphEmphasis borrows only completed original production ownership. It
// neither renders text nor exposes syntax, options or a new operation allowance.
// Every error, including final publication failure, withholds the entire result.
func (d *Document) ParagraphEmphasis(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out []ParagraphEmphasisFact, err error) {
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
	// Bound the Unicode predicate implementations independently of future table
	// growth. Each search can examine at most 32 entries in each scalar's selected
	// R16 or R32 table. No classification proceeds on a larger runtime table.
	if err = charge(8, 0); err != nil {
		return nil, err
	}
	if len(unicode.Zs.R16) > 32 || len(unicode.Zs.R32) > 32 || len(unicode.Cc.R16) > 32 || len(unicode.Cc.R32) > 32 {
		return nil, ErrModel
	}
	grow := func(length, capacity int) (int, error) {
		if err := charge(1, 0); err != nil {
			return 0, err
		}
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
		node                       *Node
		eligible, emphasized, exit bool
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
	var paragraph ParagraphEmphasisFact
	collecting := false
	for {
		// Includes the negative/terminal probe, even for an empty document.
		if err = charge(4, 0); err != nil {
			return nil, err
		}
		if len(stack) == 0 {
			break
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := f.node
		if err = charge(8, 0); err != nil {
			return nil, err
		}
		if n == nil || !contains(d.root.spec.Envelope, n.spec.Envelope) {
			return nil, ErrModel
		}
		if f.exit {
			if n.Kind() == Paragraph {
				if !collecting {
					return nil, ErrModel
				}
				capacity, e := grow(len(out), cap(out))
				if e != nil {
					return nil, e
				}
				if capacity != cap(out) {
					next := make([]ParagraphEmphasisFact, len(out), capacity)
					copy(next, out)
					out = next
				}
				if err = charge(1, 1); err != nil {
					return nil, err
				}
				out = append(out, paragraph)
				collecting = false
			}
			continue
		}
		eligible, emphasized := f.eligible, f.emphasized
		switch n.Kind() {
		case Paragraph:
			if collecting {
				return nil, ErrModel
			}
			collecting = true
			paragraph = ParagraphEmphasisFact{}
			eligible = true
			emphasized = false
			previous := n.spec.Envelope.start
			for i := 0; ; i++ {
				if err = charge(8, 0); err != nil {
					return nil, err
				}
				if i == len(n.spec.Content) {
					break
				}
				p := n.spec.Content[i].Source
				if !contains(n.spec.Envelope, p) || p.start < previous {
					return nil, ErrModel
				}
				previous = p.end
				if i == 0 {
					paragraph.Content = p
				} else {
					paragraph.Content.end = p.end
				}
			}
		case Heading, Table, TableRow, TableCell, CodeBlock, HTMLBlock, CodeSpan, Image:
			eligible = false
			emphasized = false
		case Emphasis, Strong:
			emphasized = true
		case Text:
			if eligible && raw == "" {
				if !collecting {
					return nil, ErrModel
				}
				if !n.spec.Properties.Literal.Present || len(n.spec.Content) == 0 {
					return nil, ErrModel
				}
				previousStart, previousEnd := n.spec.Envelope.start, n.spec.Envelope.start
				previousPartial := false
				for i := 0; ; i++ {
					if err = charge(12, 0); err != nil {
						return nil, err
					}
					if i == len(n.spec.Content) {
						break
					}
					p := n.spec.Content[i]
					if !p.Semantic.Present || !contains(n.spec.Envelope, p.Source) || !contains(paragraph.Content, p.Source) || p.Source.start < previousStart || p.Source.end < previousEnd || p.Source.start < previousEnd && !(previousPartial && p.Transformation == PartialTab && p.Source.start == previousStart && p.Source.end == previousEnd) {
						return nil, ErrModel
					}
					previousStart, previousEnd = p.Source.start, p.Source.end
					previousPartial = p.Transformation == PartialTab
					switch p.Transformation {
					case Exact, Escape, Entity, Normalized, PartialTab:
					default:
						return nil, ErrModel
					}
					if p.Transformation == Exact {
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
					}
					if p.Transformation == Escape {
						continue
					}
					for offset := 0; ; {
						// Two Unicode searches, each at most 32 entries with six primitive
						// comparisons/advances per entry (384), plus 128 for UTF-8 decode,
						// provenance, terminal/bounds/overflow checks and count updates.
						if err = charge(512, 0); err != nil {
							return nil, err
						}
						if offset == len(p.Semantic.Value) {
							break
						}
						r, width := utf8.DecodeRuneInString(p.Semantic.Value[offset:])
						if r == utf8.RuneError && width == 1 {
							return nil, ErrModel
						}
						offset += width
						if !IsUnicodeWhitespace(r) && !unicode.Is(unicode.Cc, r) {
							if paragraph.Total == int(^uint(0)>>1) {
								return nil, ErrModel
							}
							paragraph.Total++
							if emphasized {
								paragraph.Emphasized++
							}
						}
					}
				}
			}
		case DocumentKind, BlockQuote, List, ListItem, Strike, Link, InlineHTML, SoftBreak, HardBreak, ThematicBreak:
		default:
			return nil, ErrModel
		}
		// Compare borrowed raw-tag bytes only after reserving both possible tests.
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
		if err = push(frame{node: n, exit: true}); err != nil {
			return nil, err
		}
		// An image's entire description is opaque to this population and to outer
		// raw state. No descendant can open or close protection outside the image.
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
			if n.Kind() == Image {
				children = nil
			}
			for i := len(children); ; {
				if err = charge(1, 0); err != nil {
					return nil, err
				}
				if i == 0 {
					break
				}
				i--
				if err = push(frame{node: children[i], eligible: eligible, emphasized: emphasized}); err != nil {
					return nil, err
				}
			}
		}
	}
	return out, nil
}
