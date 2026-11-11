// Facts for MD054.

package parser

import (
	"context"
	"errors"
	"slices"
)

// LinkRepresentation is one detached original Link or Image use site. Style is
// the configurable MD054 name; Origin retains the parser source kind, so angle
// and bare GFM autolinks remain distinct.
type LinkRepresentation struct {
	Span          Span
	Image         bool
	Style, Origin string
}

// LinkRepresentationsInventory contains every actual Link and Image in Markdown
// prose, sorted by start. Unknown required ownership withholds all items.
type LinkRepresentationsInventory struct {
	Items        []LinkRepresentation
	Availability Availability
}

// LinkRepresentations observes the original parser origin of each Link and
// Image, including nested ones. Code, HTML and raw-HTML lifetimes are excluded.
func (d *Document) LinkRepresentations(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out LinkRepresentationsInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = LinkRepresentationsInventory{}
		} else if out.Availability != Available {
			out.Items = nil
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
	var items []LinkRepresentation
	publish := func(item LinkRepresentation) error {
		if e := charge(2, 0); e != nil {
			return e
		}
		capacity, e := grow(len(items), cap(items))
		if e != nil {
			return e
		}
		if capacity != cap(items) {
			next := make([]LinkRepresentation, len(items), capacity)
			copy(next, items)
			items = next
		}
		if e = charge(1, 1); e != nil {
			return e
		}
		items = append(items, item)
		return nil
	}
	type frame struct {
		node             *Node
		style            string
		prose, alt, exit bool
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
			if !rawKnown {
				uncertain = true
			}
			if rawKnown && raw.name == "" {
				item := LinkRepresentation{Span: n.spec.Envelope, Image: n.Kind() == Image, Style: f.style, Origin: n.spec.Properties.Link.Origin}
				if err = publish(item); err != nil {
					return out, err
				}
			}
			continue
		}
		prose, alt := f.prose, f.alt
		switch n.Kind() {
		case Paragraph, Heading:
			prose = true
		case TableCell:
			if n.spec.Properties.Cell == nil {
				return LinkRepresentationsInventory{Availability: Unavailable}, nil
			}
			prose = !n.spec.Properties.Cell.Alignment.Present
		case DocumentKind, BlockQuote, List, ListItem, Table, TableRow, HTMLBlock, CodeBlock:
			prose = false
		case CodeSpan:
			continue
		case Link, Image:
			p := n.spec.Properties.Link
			if p == nil {
				return LinkRepresentationsInventory{Availability: Unavailable}, nil
			}
			var style string
			switch p.Origin {
			case "inline":
				style = "inline"
			case "full":
				style = "full_reference"
			case "collapsed":
				style = "collapsed_reference"
			case "shortcut":
				style = "shortcut_reference"
			case "autolink", "bare-gfm":
				style = "autolink"
			default:
				return out, ErrModel
			}
			if prose {
				// The exit frame observes raw state after the visible children,
				// like MD042; nested Links and Images are reported on their own.
				if err = push(frame{node: n, style: style, exit: true}); err != nil {
					return out, err
				}
			}
			// Image descriptions cannot open or close an enclosing raw region.
			alt = alt || n.Kind() == Image
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
		if !alt && n.rawTag != "" {
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
				if err = push(frame{node: children[i], prose: prose, alt: alt}); err != nil {
					return out, err
				}
			}
		}
	}
	err = nestedExampleLocked(ctx, budget, index, func(work *parserContext, _ LineIndex) error {
		slices.SortStableFunc(items, func(a, b LinkRepresentation) int {
			meterWork(work, 4)
			if a.Span.start < b.Span.start {
				return -1
			}
			if a.Span.start > b.Span.start {
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
		return LinkRepresentationsInventory{Availability: Unavailable}, nil
	}
	return LinkRepresentationsInventory{Items: items, Availability: Available}, nil
}
