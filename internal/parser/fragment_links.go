package parser

import (
	"context"
	"errors"
	"slices"
	"strings"
)

// FragmentLink is one prose Link whose parser-semantic destination contains a
// fragment marker. Span is the original destination for inline Links and the
// whole Link for reference origins, whose destination text lives in a definition.
type FragmentLink struct {
	Span        Span
	Destination string
}

// FragmentLinksInventory contains detached prose Links with a fragment marker in
// their semantic destination, in source order. Unknown required ownership
// withholds every link.
type FragmentLinksInventory struct {
	Links        []FragmentLink
	Availability Availability
}

// FragmentLinks observes actual Link destinations, decoded once by the original
// parser, for inline and resolved reference origins. It excludes autolinks,
// bare URLs, protected content and Images.
func (d *Document) FragmentLinks(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (FragmentLinksInventory, error) {
	links, availability, err := proseLinks(d, ctx, budget, index, options, false, func(span Span, destination string, _ bool) (FragmentLink, bool) {
		return FragmentLink{Span: span, Destination: destination}, strings.Contains(destination, "#")
	}, func(l FragmentLink) int { return l.Span.start })
	if err != nil || availability != Available {
		return FragmentLinksInventory{Availability: availability}, err
	}
	return FragmentLinksInventory{Links: links, Availability: Available}, nil
}

// LocalLink is one prose Link or Image destination, decoded once by the parser.
// Span follows FragmentLink: the original destination for inline origins and the
// whole Link or Image for reference origins.
type LocalLink struct {
	Span        Span
	Destination string
	Image       bool
}

// LocalLinksInventory contains detached prose Link and Image destinations in
// source order. Unknown required ownership withholds every item.
type LocalLinksInventory struct {
	Links        []LocalLink
	Availability Availability
}

// LocalLinks observes Link and Image destinations under FragmentLinks' traversal
// and exclusions, whether or not they contain a fragment. An Image's description
// is not traversed.
func (d *Document) LocalLinks(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (LocalLinksInventory, error) {
	links, availability, err := proseLinks(d, ctx, budget, index, options, true, func(span Span, destination string, image bool) (LocalLink, bool) {
		return LocalLink{Span: span, Destination: destination, Image: image}, true
	}, func(l LocalLink) int { return l.Span.start })
	if err != nil || availability != Available {
		return LocalLinksInventory{Availability: availability}, err
	}
	return LocalLinksInventory{Links: links, Availability: Available}, nil
}

// proseLinks walks prose Links, and Images when images is set, publishing the
// items keep admits. Images skip their children; Links classify after theirs.
func proseLinks[T any](d *Document, ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, images bool, keep func(Span, string, bool) (T, bool), start func(T) int) (links []T, availability Availability, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return nil, "", err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			links, availability = nil, ""
		} else if availability != Available {
			links = nil
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
	publish := func(link T, size int) error {
		if e := charge(2, 0); e != nil {
			return e
		}
		capacity, e := grow(len(links), cap(links))
		if e != nil {
			return e
		}
		if capacity != cap(links) {
			next := make([]T, len(links), capacity)
			copy(next, links)
			links = next
		}
		if e = charge(int64(size)+1, 1); e != nil {
			return e
		}
		links = append(links, link)
		return nil
	}
	// Destination components retain atomic escape/entity spellings. Validate the
	// complete original mapping without decoding again or allocating a joined copy.
	destination := func(n *Node) (Span, Availability, error) {
		v := n.spec.Properties.Link.Destination
		if !v.Semantic.Present {
			return Span{}, Unavailable, nil
		}
		if n.spec.Properties.Link.Origin != "inline" {
			if e := charge(int64(len(v.Semantic.Value))+1, 0); e != nil {
				return Span{}, "", e
			}
			return Span{source: index.source, start: n.spec.Envelope.start, end: n.spec.Envelope.end, present: true}, Available, nil
		}
		if len(v.Segments) == 0 {
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
		return nil, "", err
	}
	raw := conflictRawState{}
	rawKnown := true
	uncertain := false
	classify := func(n *Node, image bool) (Availability, error) {
		span, available, e := destination(n)
		if e != nil || available != Available {
			return available, e
		}
		if !rawKnown {
			uncertain = true
		}
		if rawKnown && raw.name == "" {
			value := n.spec.Properties.Link.Destination.Semantic.Value
			if item, ok := keep(span, value, image); ok {
				if e = publish(item, len(value)); e != nil {
					return "", e
				}
			}
		}
		return Available, nil
	}
	if err = charge(2, 2); err != nil {
		return nil, "", err
	}
	htmlWork := &parserContext{Context: ctx, budget: &parseBudget{operation: budget}}
	for {
		if err = charge(32, 0); err != nil {
			return nil, "", err
		}
		if len(stack) == 0 {
			break
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := f.node
		if n == nil || !n.spec.Envelope.present || n.spec.Envelope.SourceToken() != index.source.Token() || n.spec.Envelope.start < 0 || n.spec.Envelope.end < n.spec.Envelope.start || n.spec.Envelope.end > index.source.Len() {
			return nil, "", ErrModel
		}
		if f.exit {
			available, e := classify(n, false)
			if e != nil {
				return nil, "", e
			}
			if available != Available {
				return nil, Unavailable, nil
			}
			continue
		}
		prose := f.prose
		switch n.Kind() {
		case Paragraph, Heading:
			prose = true
		case TableCell:
			if n.spec.Properties.Cell == nil {
				return nil, Unavailable, nil
			}
			prose = !n.spec.Properties.Cell.Alignment.Present
		case DocumentKind, BlockQuote, List, ListItem, Table, TableRow, HTMLBlock, CodeBlock:
			prose = false
		case Image:
			if !images {
				continue
			}
			p := n.spec.Properties.Link
			if p == nil {
				return nil, Unavailable, nil
			}
			switch p.Origin {
			case "full", "collapsed", "shortcut", "inline":
				if p.Resolution != Available {
					return nil, Unavailable, nil
				}
			default:
				return nil, "", ErrModel
			}
			// The description is not traversed, so the Image classifies at once.
			if prose {
				available, e := classify(n, true)
				if e != nil {
					return nil, "", e
				}
				if available != Available {
					return nil, Unavailable, nil
				}
			}
			continue
		case CodeSpan:
			continue
		case Link:
			p := n.spec.Properties.Link
			if p == nil {
				return nil, Unavailable, nil
			}
			switch p.Origin {
			case "autolink", "bare-gfm":
				continue
			case "full", "collapsed", "shortcut", "inline":
				if p.Resolution != Available {
					return nil, Unavailable, nil
				}
				// Visible children can change raw state, so classify after them.
				if prose {
					if err = push(frame{node: n, exit: true}); err != nil {
						return nil, "", err
					}
				}
			default:
				return nil, "", ErrModel
			}
		case Text, InlineHTML, SoftBreak, HardBreak, ThematicBreak, Emphasis, Strong, Strike:
		default:
			return nil, "", ErrModel
		}
		if n.Kind() == HTMLBlock {
			var available bool
			raw, _, available, err = conflictHTMLRaw(htmlWork, index.source, n, raw)
			if err != nil {
				return nil, "", err
			}
			rawKnown = rawKnown && available
			continue
		}
		if err = charge(2*int64(len(n.rawTag))+int64(len(raw.name))+1, 0); err != nil {
			return nil, "", err
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
				return nil, "", err
			}
			for i := len(children) - 1; i >= 0; i-- {
				if err = push(frame{node: children[i], prose: prose}); err != nil {
					return nil, "", err
				}
			}
		}
	}
	err = nestedExampleLocked(ctx, budget, index, func(work *parserContext, _ LineIndex) error {
		slices.SortFunc(links, func(a, b T) int {
			meterWork(work, 4)
			if start(a) < start(b) {
				return -1
			}
			if start(a) > start(b) {
				return 1
			}
			return 0
		})
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	if uncertain {
		return nil, Unavailable, nil
	}
	return links, Available, nil
}
