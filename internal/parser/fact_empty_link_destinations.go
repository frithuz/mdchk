package parser

import (
	"context"
	"errors"
	"slices"
)

// EmptyLinkDestinationsInventory contains detached original destination ranges for
// inline Links with an empty or lone-fragment semantic destination. Empty ranges
// are insertion points. Unknown required ownership withholds all ranges.
type EmptyLinkDestinationsInventory struct {
	Spans        []Span
	Availability Availability
}

// EmptyLinkDestinations observes actual inline Link destinations, decoded once
// by the original parser. It excludes reference origins and whole Images.
func (d *Document) EmptyLinkDestinations(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out EmptyLinkDestinationsInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = EmptyLinkDestinationsInventory{}
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
	// Destination components retain atomic escape/entity spellings. Validate the
	// complete original mapping without decoding again or allocating a joined copy.
	destination := func(n *Node) (Span, Availability, error) {
		v := n.spec.Properties.Link.Destination
		if !v.Semantic.Present || len(v.Segments) == 0 {
			return Span{}, Unavailable, nil
		}
		previous, offset := n.spec.Envelope.start, 0
		start := v.Segments[0].Source.start
		for j, part := range v.Segments {
			if e := charge(16, 0); e != nil {
				return Span{}, "", e
			}
			if part.Transformation == MappingUnavailable || !part.Semantic.Present {
				return Span{}, Unavailable, nil
			}
			if !part.Source.present || part.Source.SourceToken() != index.source.Token() || part.Source.start < previous || part.Source.end < part.Source.start || !contains(n.spec.Envelope, part.Source) || j > 0 && part.Source.start != previous {
				return Span{}, "", ErrModel
			}
			switch part.Transformation {
			case Exact:
				if e := charge(int64(part.Source.end-part.Source.start)+1, 0); e != nil {
					return Span{}, "", e
				}
				if index.source.content[part.Source.start:part.Source.end] != part.Semantic.Value {
					return Span{}, "", ErrModel
				}
			case Normalized, Escape, Entity, PartialTab:
			default:
				return Span{}, "", ErrModel
			}
			size := len(part.Semantic.Value)
			if e := charge(int64(size)+1, 0); e != nil {
				return Span{}, "", e
			}
			if size > len(v.Semantic.Value)-offset || v.Semantic.Value[offset:offset+size] != part.Semantic.Value {
				return Span{}, "", ErrModel
			}
			offset += size
			previous = part.Source.end
		}
		if offset != len(v.Semantic.Value) {
			return Span{}, "", ErrModel
		}
		if v.Semantic.Value != "" && v.Semantic.Value != "#" {
			return Span{}, Available, nil
		}
		return Span{source: index.source, start: start, end: previous, present: true}, Available, nil
	}
	type frame struct {
		node        *Node
		prose, exit bool
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
			span, available, e := destination(n)
			if e != nil {
				return out, e
			}
			if available != Available {
				return EmptyLinkDestinationsInventory{Availability: Unavailable}, nil
			}
			if !rawKnown {
				uncertain = true
			}
			if span.present && rawKnown && raw.name == "" {
				if err = publish(span.start, span.end); err != nil {
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
				return EmptyLinkDestinationsInventory{Availability: Unavailable}, nil
			}
			prose = !n.spec.Properties.Cell.Alignment.Present
		case DocumentKind, BlockQuote, List, ListItem, Table, TableRow, HTMLBlock, CodeBlock:
			prose = false
		case Image, CodeSpan:
			continue
		case Link:
			p := n.spec.Properties.Link
			if p == nil {
				return EmptyLinkDestinationsInventory{Availability: Unavailable}, nil
			}
			switch p.Origin {
			case "autolink", "bare-gfm":
				continue
			case "full", "collapsed", "shortcut":
				// Their visible children can change raw state, but definitions are outside
				// this rule and have no destination source component at the use site.
			case "inline":
				if p.Resolution != Available {
					return EmptyLinkDestinationsInventory{Availability: Unavailable}, nil
				}
				if prose {
					if err = push(frame{node: n, exit: true}); err != nil {
						return out, err
					}
				}
			default:
				return out, ErrModel
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
		return EmptyLinkDestinationsInventory{Availability: Unavailable}, nil
	}
	return EmptyLinkDestinationsInventory{Spans: spans, Availability: Available}, nil
}
