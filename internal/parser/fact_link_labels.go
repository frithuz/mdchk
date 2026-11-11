// Facts for MD059.

package parser

import (
	"context"
	"errors"
	"slices"
	"strings"
)

// LinkLabel is one detached original Link label interior with its visible
// text. Text follows the image-description child rules, trimmed and with each
// internal whitespace run collapsed into one U+0020.
type LinkLabel struct {
	Span Span
	Text string
}

// LinkLabelsInventory contains every actual inline and resolved reference Link
// in Markdown prose, sorted by start. Unknown required ownership withholds all items.
type LinkLabelsInventory struct {
	Items        []LinkLabel
	Availability Availability
}

// NormalizeLinkLabel trims label whitespace and collapses each internal run of
// it into one U+0020. It performs no Unicode normalization or case folding.
func NormalizeLinkLabel(text string) string {
	var b strings.Builder
	linkLabelAppend(&b, false, text)
	return b.String()
}

// linkLabelAppend writes value after any pending separator and reports whether
// a separator is pending: whitespace after earlier visible text.
func linkLabelAppend(b *strings.Builder, pending bool, value string) bool {
	for _, r := range value {
		if linkLabelWhitespaceRune(r) {
			pending = b.Len() > 0
			continue
		}
		if pending {
			b.WriteByte(' ')
			pending = false
		}
		b.WriteRune(r)
	}
	return pending
}

// LinkLabels observes the visible label of each actual Link outside Image
// descriptions. Autolinks, reference identifiers, code, HTML and raw-HTML
// lifetimes are excluded. A nested Image contributes its own description.
func (d *Document) LinkLabels(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out LinkLabelsInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = LinkLabelsInventory{}
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
	var items []LinkLabel
	publish := func(item LinkLabel) error {
		if e := charge(2, 0); e != nil {
			return e
		}
		capacity, e := grow(len(items), cap(items))
		if e != nil {
			return e
		}
		if capacity != cap(items) {
			next := make([]LinkLabel, len(items), capacity)
			copy(next, items)
			items = next
		}
		if e = charge(1, 1); e != nil {
			return e
		}
		items = append(items, item)
		return nil
	}
	// Links cannot contain Links outside Image descriptions, which are never
	// eligible, so each label child is visited by at most one label walk.
	var nodes []*Node
	label := func(link *Node) (LinkLabel, error) {
		var open, close Span
		for _, marker := range link.spec.Markers {
			if e := charge(3, 0); e != nil {
				return LinkLabel{}, e
			}
			switch marker.Role {
			case "label-open":
				open = marker.Source
			case "label-close":
				close = marker.Source
			}
		}
		if !open.present || !close.present || !contains(link.spec.Envelope, open) || !contains(link.spec.Envelope, close) || open.end > close.start {
			return LinkLabel{}, ErrModel
		}
		span := Span{source: index.source, start: open.end, end: close.start, present: true}
		var text strings.Builder
		pending := false
		children := func(n *Node) error {
			if e := charge(int64(len(n.spec.Children))+1, 0); e != nil {
				return e
			}
			for i := len(n.spec.Children) - 1; i >= 0; i-- {
				capacity, e := grow(len(nodes), cap(nodes))
				if e != nil {
					return e
				}
				if capacity != cap(nodes) {
					next := make([]*Node, len(nodes), capacity)
					copy(next, nodes)
					nodes = next
				}
				if e = charge(1, 1); e != nil {
					return e
				}
				nodes = append(nodes, n.spec.Children[i])
			}
			return nil
		}
		nodes = nodes[:0]
		if e := children(link); e != nil {
			return LinkLabel{}, e
		}
		for len(nodes) != 0 {
			if e := charge(16, 0); e != nil {
				return LinkLabel{}, e
			}
			n := nodes[len(nodes)-1]
			nodes = nodes[:len(nodes)-1]
			if n == nil || !contains(link.spec.Envelope, n.spec.Envelope) {
				return LinkLabel{}, ErrModel
			}
			switch n.Kind() {
			case Text, CodeSpan:
				for _, p := range n.spec.Content {
					if e := charge(4, 0); e != nil {
						return LinkLabel{}, e
					}
					if !p.Semantic.Present || !contains(n.spec.Envelope, p.Source) {
						return LinkLabel{}, ErrModel
					}
					if e := charge(int64(len(p.Semantic.Value))+1, 0); e != nil {
						return LinkLabel{}, e
					}
					pending = linkLabelAppend(&text, pending, p.Semantic.Value)
				}
				continue
			case SoftBreak, HardBreak:
				pending = text.Len() > 0
				continue
			case InlineHTML:
				continue
			case Emphasis, Strong, Strike, Link, Image:
				// Formatting contributes its descendants; an Image its description.
			default:
				return LinkLabel{}, ErrModel
			}
			if e := children(n); e != nil {
				return LinkLabel{}, e
			}
		}
		return LinkLabel{Span: span, Text: text.String()}, nil
	}
	type frame struct {
		node             *Node
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
				item, e := label(n)
				if e != nil {
					return out, e
				}
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
				return LinkLabelsInventory{Availability: Unavailable}, nil
			}
			prose = !n.spec.Properties.Cell.Alignment.Present
		case DocumentKind, BlockQuote, List, ListItem, Table, TableRow, HTMLBlock, CodeBlock:
			prose = false
		case CodeSpan:
			continue
		case Link, Image:
			p := n.spec.Properties.Link
			if p == nil {
				return LinkLabelsInventory{Availability: Unavailable}, nil
			}
			switch p.Origin {
			case "inline", "full", "collapsed", "shortcut":
			case "autolink", "bare-gfm":
				if n.Kind() == Link {
					continue
				}
			default:
				return out, ErrModel
			}
			if prose && !alt && n.Kind() == Link {
				if p.Resolution != Available {
					return LinkLabelsInventory{Availability: Unavailable}, nil
				}
				// The exit frame observes raw state after the visible children,
				// like MD042; Links inside Image descriptions are never checked.
				if err = push(frame{node: n, exit: true}); err != nil {
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
		slices.SortFunc(items, func(a, b LinkLabel) int {
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
		return LinkLabelsInventory{Availability: Unavailable}, nil
	}
	return LinkLabelsInventory{Items: items, Availability: Available}, nil
}
