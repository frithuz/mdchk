package parser

import (
	"context"
	"errors"
	"slices"
	"unicode"
	"unicode/utf8"
)

// LinkLabelWhitespaceInventory contains detached original whitespace ranges at
// visible bracket-label edges. Unknown required ownership withholds all ranges.
type LinkLabelWhitespaceInventory struct {
	Spans        []Span
	Availability Availability
}

func linkLabelWhitespaceRune(r rune) bool {
	return r == '\t' || r == '\n' || r == '\f' || r == '\r' || unicode.Is(unicode.Zs, r)
}

// LinkLabelWhitespace observes actual inline and resolved reference Link labels.
// It never decodes entities, inspects reference identifiers or enters Images.
func (d *Document) LinkLabelWhitespace(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out LinkLabelWhitespaceInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = LinkLabelWhitespaceInventory{}
		} else if out.Availability != Available {
			out.Spans = nil
		}
	}()
	charge := func(w, n int64) error { return headingFactCharge(ctx, budget, w, n) }
	grow := func(length, capacity int) (int, error) {
		if length < capacity {
			return capacity, nil
		}
		if capacity > int(^uint(0)>>1)/2 {
			return 0, ErrModel
		}
		capacity = max(1, capacity*2)
		if e := charge(int64(length)+int64(capacity)+1, int64(capacity)); e != nil {
			return 0, e
		}
		return capacity, nil
	}
	var spans []Span
	publish := func(start, end int) error {
		if e := charge(2, 0); e != nil {
			return e
		}
		if start == end {
			return nil
		}
		capacity, e := grow(len(spans), cap(spans))
		if e != nil {
			return e
		}
		if capacity != cap(spans) {
			next := make([]Span, len(spans), capacity)
			copy(next, spans)
			spans = next
		}
		if e = charge(1, 1); e != nil {
			return e
		}
		spans = append(spans, Span{source: index.source, start: start, end: end, present: true})
		return nil
	}
	// Each range is source-contiguous. Logical runs may cross consumed container
	// prefixes, which never enter a range. CRLF stays atomic during both scans.
	edge := func(n *Node, first, emit bool) (bool, error) {
		parts := n.spec.Properties.Link.Label.Segments
		start, end, present := 0, 0, false
		flush := func() error {
			if !present || !emit {
				present = false
				return nil
			}
			e := publish(start, end)
			present = false
			return e
		}
		for j := 0; ; j++ {
			if e := charge(8, 0); e != nil {
				return false, e
			}
			if j == len(parts) {
				return true, flush()
			}
			i := j
			if !first {
				i = len(parts) - 1 - j
			}
			p := parts[i]
			if p.Transformation != Exact && p.Transformation != Normalized {
				return false, flush()
			}
			at, stop := p.Source.start, p.Source.end
			if !first {
				at, stop = stop, at
			}
			for at != stop {
				if e := charge(16, 0); e != nil {
					return false, e
				}
				var r rune
				var width int
				if first {
					r, width = utf8.DecodeRuneInString(index.source.content[at:stop])
				} else {
					r, width = utf8.DecodeLastRuneInString(index.source.content[stop:at])
				}
				if r == utf8.RuneError && width == 1 {
					return false, ErrModel
				}
				if !linkLabelWhitespaceRune(r) {
					return false, flush()
				}
				lo, hi := at, at+width
				if !first {
					lo, hi = at-width, at
				}
				if first && r == '\r' && hi < stop && index.source.content[hi] == '\n' {
					hi++
				}
				if !first && r == '\n' && lo > stop && index.source.content[lo-1] == '\r' {
					lo--
				}
				if present && (first && end != lo || !first && start != hi) {
					if e := flush(); e != nil {
						return false, e
					}
				}
				if !present {
					start, end, present = lo, hi, true
				} else if first {
					end = hi
				} else {
					start = lo
				}
				if first {
					at = hi
				} else {
					at = lo
				}
			}
		}
	}
	type frame struct {
		node                  *Node
		prose, exit, allWhite bool
	}
	var stack []frame
	push := func(f frame) error {
		capacity, e := grow(len(stack), cap(stack))
		if e != nil {
			return e
		}
		if capacity != cap(stack) {
			next := make([]frame, len(stack), capacity)
			copy(next, stack)
			stack = next
		}
		if e = charge(1, 1); e != nil {
			return e
		}
		stack = append(stack, f)
		return nil
	}
	if err = push(frame{node: d.root}); err != nil {
		return out, err
	}
	raw := conflictRawState{}
	rawKnown := true
	uncertain := false
	if err = charge(2, 2); err != nil {
		return out, err
	}
	htmlWork := &parserContext{Context: ctx, budget: &parseBudget{operation: budget}}
	for {
		if err = charge(32, 0); err != nil {
			return out, err
		}
		if len(stack) == 0 {
			break
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := f.node
		if n == nil || !n.spec.Envelope.present || n.spec.Envelope.SourceToken() != index.source.Token() || n.spec.Envelope.start < 0 || n.spec.Envelope.end < n.spec.Envelope.start || n.spec.Envelope.end > index.source.Len() {
			return out, ErrModel
		}
		if f.exit {
			if !f.allWhite {
				if !rawKnown {
					uncertain = true
				}
				if _, err = edge(n, false, rawKnown && raw.name == ""); err != nil {
					return out, err
				}
			}
			continue
		}
		prose := f.prose
		switch n.Kind() {
		case Paragraph, Heading:
			prose = true
		case TableCell:
			if n.spec.Properties.Cell == nil {
				return LinkLabelWhitespaceInventory{Availability: Unavailable}, nil
			}
			prose = !n.spec.Properties.Cell.Alignment.Present
		case DocumentKind, BlockQuote, List, ListItem, Table, TableRow, HTMLBlock, CodeBlock:
			prose = false
		case Image, CodeSpan:
			continue
		case Link:
			p := n.spec.Properties.Link
			if p == nil {
				return LinkLabelWhitespaceInventory{Availability: Unavailable}, nil
			}
			if p.Origin == "autolink" || p.Origin == "bare-gfm" {
				continue
			}
			if p.Origin != "inline" && p.Origin != "full" && p.Origin != "collapsed" && p.Origin != "shortcut" {
				return out, ErrModel
			}
			if prose {
				if !p.Label.Semantic.Present || len(p.Label.Segments) == 0 {
					return LinkLabelWhitespaceInventory{Availability: Unavailable}, nil
				}
				previous := n.spec.Envelope.start
				for _, part := range p.Label.Segments {
					if err = charge(12, 0); err != nil {
						return out, err
					}
					if part.Transformation == MappingUnavailable || !part.Semantic.Present {
						return LinkLabelWhitespaceInventory{Availability: Unavailable}, nil
					}
					if !part.Source.present || part.Source.SourceToken() != index.source.Token() || part.Source.start < previous || !contains(n.spec.Envelope, part.Source) {
						return out, ErrModel
					}
					switch part.Transformation {
					case Exact:
						if err = charge(int64(part.Source.end-part.Source.start)+1, 0); err != nil {
							return out, err
						}
						if index.source.content[part.Source.start:part.Source.end] != part.Semantic.Value {
							return out, ErrModel
						}
					case Normalized, Escape, Entity, PartialTab:
						// Retained transformations stay atomic; only original exact or
						// normalized whitespace spelling can contribute an edge run.
					default:
						return out, ErrModel
					}
					previous = part.Source.end
				}
				if !rawKnown {
					uncertain = true
				}
				allWhite, e := edge(n, true, rawKnown && raw.name == "")
				if e != nil {
					return out, e
				}
				if err = push(frame{node: n, exit: true, allWhite: allWhite}); err != nil {
					return out, err
				}
			}
		case Text, InlineHTML, SoftBreak, HardBreak, ThematicBreak, Emphasis, Strong, Strike:
		default:
			return out, ErrModel
		}
		if n.Kind() == HTMLBlock {
			var available bool
			raw, _, available, err = conflictHTMLRaw(htmlWork, index.source, n, raw)
			if err != nil {
				return out, err
			}
			rawKnown = rawKnown && available
			continue
		}
		if err = charge(2*int64(len(n.rawTag))+int64(len(raw.name))+1, 0); err != nil {
			return out, err
		}
		if n.rawTag != "" {
			if raw.name == "" && !n.rawClosing {
				raw = conflictRawState{name: n.rawTag, start: n.spec.Envelope.start}
			} else if raw.name == n.rawTag && n.rawClosing {
				raw = conflictRawState{}
			}
		}
		if n.Kind() == CodeBlock {
			continue
		}
		for group := 0; group < 2; group++ {
			children := n.htmlTail
			if group == 1 {
				children = n.spec.Children
			}
			if err = charge(int64(len(children))+3, 0); err != nil {
				return out, err
			}
			for i := len(children) - 1; i >= 0; i-- {
				if err = push(frame{node: children[i], prose: prose}); err != nil {
					return out, err
				}
			}
		}
	}
	err = nestedExampleLocked(ctx, budget, index, func(work *parserContext, _ LineIndex) error {
		slices.SortFunc(spans, func(a, b Span) int {
			meterWork(work, 4)
			if a.start < b.start {
				return -1
			}
			if a.start > b.start {
				return 1
			}
			return 0
		})
		return nil
	})
	if err != nil {
		return out, err
	}
	if uncertain {
		return LinkLabelWhitespaceInventory{Availability: Unavailable}, nil
	}
	return LinkLabelWhitespaceInventory{Spans: spans, Availability: Available}, nil
}
