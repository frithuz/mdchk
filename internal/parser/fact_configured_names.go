// Facts for MD044, MD061, MD083 and MD084.

package parser

import (
	"context"
	"errors"
	"slices"
	"unicode"
	"unicode/utf8"
)

// ConfiguredNames compares configured spellings only within exact, contiguous,
// owner-local physical prose islands. Returned spans belong to the original
// completed parse; this projection supplies neither edits nor parse authority.
func (d *Document) ConfiguredNames(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, names []string, codeBlocks bool) ([]Span, error) {
	return d.proseMatches(ctx, budget, index, options, names, codeBlocks, false, false, true, false, nil)
}

// ConfiguredNameReplacements returns the ConfiguredNames spans with, for each,
// the one distinct configured spelling whose full case fold equals the span's
// original bytes. The replacement is empty when more than one distinct spelling
// folds equal, so the correction is ambiguous.
func (d *Document) ConfiguredNameReplacements(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, names []string, codeBlocks bool) ([]Span, []string, error) {
	spans, err := d.ConfiguredNames(ctx, budget, index, options, names, codeBlocks)
	if err != nil {
		return nil, nil, err
	}
	if err = headingFactCharge(ctx, budget, int64(len(names)+len(spans))+1, int64(len(names)+len(spans))); err != nil {
		return nil, nil, err
	}
	folded := make([]string, len(names))
	for i, value := range names {
		if folded[i], err = FoldCase(ctx, budget, value); err != nil {
			return nil, nil, err
		}
	}
	out := make([]string, len(spans))
	for i, s := range spans {
		text := index.source.content[s.start:s.end]
		fold, err := FoldCase(ctx, budget, text)
		if err != nil {
			return nil, nil, err
		}
		chosen, distinct := "", 0
		for j, value := range names {
			if err = headingFactCharge(ctx, budget, int64(len(value)+len(fold))+1, 0); err != nil {
				return nil, nil, err
			}
			if folded[j] != fold || value == chosen {
				continue
			}
			chosen = value
			distinct++
		}
		if distinct == 1 && chosen != text {
			out[i] = chosen
		}
	}
	return spans, out, nil
}

// ProhibitedTerms reports every leftmost-longest configured term occurrence in
// the same prose islands as ConfiguredNames, never scanning code blocks. Terms
// compare through full case folding unless caseSensitive selects exact bytes.
func (d *Document) ProhibitedTerms(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, terms []string, caseSensitive bool) ([]Span, error) {
	return d.proseMatches(ctx, budget, index, options, terms, false, true, caseSensitive, true, false, nil)
}

// SuspiciousSequences reports every leftmost-longest configured sequence
// occurrence in the same prose islands as ProhibitedTerms, never scanning code
// blocks. Sequences compare by exact original bytes and may start or end
// anywhere inside a word.
func (d *Document) SuspiciousSequences(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, sequences []string) ([]Span, error) {
	return d.proseMatches(ctx, budget, index, options, sequences, false, true, true, false, false, nil)
}

// HiddenCharacters reports every exact U+200B, U+202A to U+202E and U+FEFF
// scalar in the same prose islands as ProhibitedTerms, except a U+FEFF at byte
// zero and any code point listed in allowed.
func (d *Document) HiddenCharacters(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, allowed []rune) ([]Span, error) {
	return d.proseMatches(ctx, budget, index, options, nil, false, false, false, true, true, allowed)
}

// proseMatches is the shared walker. Prohibited mode publishes every winner;
// otherwise winners spelled exactly as a fold-equal configured name are accepted.
// Whole-word mode requires both outside neighbors to be non-word scalars.
// Hidden mode ignores names and publishes each reported hidden scalar instead.
func (d *Document) proseMatches(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, names []string, codeBlocks, prohibited, caseSensitive, wholeWord, hidden bool, allowed []rune) (out []Span, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	if unicode.Version != "17.0.0" {
		return nil, ErrModel
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
		if e := charge(int64(length)+1, int64(capacity)); e != nil {
			return 0, e
		}
		return capacity, nil
	}
	type name struct{ original, folded string }
	if err = charge(int64(len(names))+1, int64(len(names))); err != nil {
		return nil, err
	}
	configured := make([]name, 0, len(names))
	for _, value := range names {
		// Validate even inert names; FoldCase performs its own bounded reservations.
		folded, e := FoldCase(ctx, budget, value)
		if e != nil {
			return nil, e
		}
		if caseSensitive {
			folded = value
		}
		if folded != "" {
			configured = append(configured, name{value, folded})
		}
	}
	if err = charge(int64(len(allowed))+1, 0); err != nil {
		return nil, err
	}
	if len(configured) == 0 && !hidden {
		return nil, nil
	}
	type scalar struct {
		start, end int
		folded     string
		word       bool
	}
	var island []scalar
	leftWord, lastWord := false, false
	previousEnd := -1
	word := func(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r) }
	publish := func(start, end int) error {
		capacity, e := grow(len(out), cap(out))
		if e != nil {
			return e
		}
		if capacity != cap(out) {
			next := make([]Span, len(out), capacity)
			copy(next, out)
			out = next
		}
		if e = charge(1, 1); e != nil {
			return e
		}
		out = append(out, Span{source: index.source, start: start, end: end, present: true})
		return nil
	}
	flush := func(rightWord bool) error {
		for i := 0; ; {
			if e := charge(2, 0); e != nil {
				return e
			}
			if i == len(island) {
				break
			}
			before := leftWord
			if i > 0 {
				before = island[i-1].word
			}
			if before && wholeWord {
				i++
				continue
			}
			best := -1
			accepted := false
			for _, n := range configured {
				if e := charge(1, 0); e != nil {
					return e
				}
				at, j := 0, i
				for {
					if e := charge(2, 0); e != nil {
						return e
					}
					if at == len(n.folded) || j == len(island) {
						break
					}
					f := island[j].folded
					if e := charge(int64(len(f))+1, 0); e != nil {
						return e
					}
					if len(f) > len(n.folded)-at || f != n.folded[at:at+len(f)] {
						break
					}
					at += len(f)
					j++
				}
				if e := charge(2, 0); e != nil {
					return e
				}
				if at != len(n.folded) {
					continue
				}
				after := rightWord
				if j < len(island) {
					after = island[j].word
				}
				if after && wholeWord {
					continue
				}
				if e := charge(int64(island[j-1].end-island[i].start)+1, 0); e != nil {
					return e
				}
				exact := index.source.content[island[i].start:island[j-1].end] == n.original
				if j > best {
					best, accepted = j, exact
				} else if j == best {
					accepted = accepted || exact
				}
			}
			if e := charge(1, 0); e != nil {
				return e
			} // unsuccessful name probe
			if best < 0 {
				i++
				continue
			}
			if prohibited || !accepted {
				if e := publish(island[i].start, island[best-1].end); e != nil {
					return e
				}
			}
			i = best
		}
		island = island[:0]
		return nil
	}
	boundary := func() error {
		if e := flush(false); e != nil {
			return e
		}
		leftWord, lastWord, previousEnd = false, false, -1
		return nil
	}
	text := func(n *Node) error {
		for _, part := range n.spec.Content {
			if e := charge(6, 0); e != nil {
				return e
			}
			if !part.Source.Present() || part.Source.SourceToken() != index.source.Token() || !part.Semantic.Present {
				return ErrModel
			}
			if part.Source.start != previousEnd {
				if e := boundary(); e != nil {
					return e
				}
			}
			switch part.Transformation {
			case Exact, Entity, Escape, Normalized, PartialTab:
			case Synthetic:
				// Literal code's synthetic final LF owns no authored bytes.
				if n.Kind() != CodeBlock || part.Source.start != part.Source.end || part.Semantic.Value != "\n" {
					return ErrModel
				}
				if e := boundary(); e != nil {
					return e
				}
				continue
			default:
				return ErrModel
			}
			if part.Transformation == Exact {
				if e := charge(int64(len(part.Semantic.Value))+1, 0); e != nil {
					return e
				}
				if part.Source.end-part.Source.start != len(part.Semantic.Value) || index.source.content[part.Source.start:part.Source.end] != part.Semantic.Value {
					return ErrModel
				}
			}
			first := true
			for offset := 0; ; {
				// UTF8 decoding, three Unicode predicates (at most 32 comparisons each),
				// full-fold lookup and scalar bookkeeping are reserved before inspection.
				if e := charge(128, 0); e != nil {
					return e
				}
				if offset == len(part.Semantic.Value) {
					break
				}
				r, width := utf8.DecodeRuneInString(part.Semantic.Value[offset:])
				if r == utf8.RuneError && width == 1 {
					return ErrModel
				}
				isWord := word(r)
				if part.Transformation != Exact {
					if first {
						if e := flush(isWord); e != nil {
							return e
						}
					}
					leftWord, lastWord = isWord, isWord
				} else if r == '\n' || r == '\r' {
					if e := boundary(); e != nil {
						return e
					}
				} else if hidden {
					at := part.Source.start + offset
					if r == 0x200B || r >= 0x202A && r <= 0x202E || r == 0xFEFF && at != 0 {
						if e := charge(int64(len(allowed))+1, 0); e != nil {
							return e
						}
						if !slices.Contains(allowed, r) {
							if e := publish(at, at+width); e != nil {
								return e
							}
						}
					}
				} else {
					if len(island) == 0 {
						leftWord = lastWord
					}
					folded, ok := fullCaseFold[r]
					if !ok || caseSensitive {
						folded = part.Semantic.Value[offset : offset+width]
					}
					capacity, e := grow(len(island), cap(island))
					if e != nil {
						return e
					}
					if capacity != cap(island) {
						next := make([]scalar, len(island), capacity)
						copy(next, island)
						island = next
					}
					if e = charge(1, 1); e != nil {
						return e
					}
					at := part.Source.start + offset
					island = append(island, scalar{at, at + width, folded, isWord})
					lastWord = isWord
				}
				offset += width
				first = false
			}
			if part.Transformation != Exact && first {
				if e := boundary(); e != nil {
					return e
				}
			}
			previousEnd = part.Source.end
		}
		return charge(1, 0)
	}
	type frame struct {
		node                 *Node
		prose, exit, restore bool
		raw                  string
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
		if err = charge(8, 0); err != nil {
			return nil, err
		}
		if len(stack) == 0 {
			break
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := f.node
		if f.exit {
			if n.Kind() != Text {
				if err = boundary(); err != nil {
					return nil, err
				}
			}
			if f.restore {
				raw = f.raw
			}
			continue
		}
		prose := f.prose
		switch n.Kind() {
		case Paragraph, Heading:
			prose = true
		case TableCell:
			prose = n.spec.Properties.Cell != nil && !n.spec.Properties.Cell.Alignment.Present
		case DocumentKind, BlockQuote, List, ListItem, Table, TableRow, HTMLBlock, CodeBlock:
			prose = false
		case Link:
			if err = charge(18, 0); err != nil {
				return nil, err
			}
			if n.spec.Properties.Link == nil {
				return nil, ErrModel
			}
			origin := n.spec.Properties.Link.Origin
			prose = prose && origin != "autolink" && origin != "bare-gfm"
		default:
			// Other inline kinds inherit their actual owner.
		}
		if n.Kind() != Text {
			if err = boundary(); err != nil {
				return nil, err
			}
		}
		if n.Kind() == Text && prose && raw == "" || n.Kind() == CodeBlock && codeBlocks && raw == "" {
			if err = text(n); err != nil {
				return nil, err
			}
		}
		if n.rawTag != "" {
			if err = charge(2*int64(len(n.rawTag))+int64(len(raw))+1, 0); err != nil {
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
		if err = push(exit); err != nil {
			return nil, err
		}
		for group := 0; group < 2; group++ {
			children := n.htmlTail
			if group == 1 {
				children = n.spec.Children
			}
			if err = charge(int64(len(children))+3, 0); err != nil {
				return nil, err
			}
			for i := len(children) - 1; i >= 0; i-- {
				if err = push(frame{node: children[i], prose: prose}); err != nil {
					return nil, err
				}
			}
		}
	}
	if err = boundary(); err != nil {
		return nil, err
	}
	return out, nil
}
