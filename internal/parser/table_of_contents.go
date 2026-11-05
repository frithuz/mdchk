package parser

import (
	"context"
	"sort"
	"strings"
)

// TOCMarker is a parser-confirmed, closed and unprotected HTML comment whose
// exact original spelling is a configured marker and which is alone on its
// physical line apart from container prefixes, spaces and tabs. Container is
// the index in Blocks of the innermost enclosing BlockQuote or ListItem, or -1
// at the document root. Line selects the complete physical line, including its
// ending, so consumers can bound a region without rescanning source.
type TOCMarker struct {
	Envelope, Line Span
	Start          bool
	Container      int
}

// TOCBlock is one actual block in source preorder. Parent is the index of the
// enclosing block, or -1 at the document root; paragraph, heading, table, code
// and HTML interiors are not descended. List items additionally retain Marker,
// from the item marker to the end of its first physical line without the line
// ending, and Link when the item's first block is a paragraph made of exactly
// one inline, full, collapsed or shortcut link. Label is that link's visible
// text, with image alt text kept; Destination is its resolved destination.
type TOCBlock struct {
	Kind     Kind
	Envelope Span
	Parent   int
	Marker   Span
	Link     bool
	Label    string
	// Destination is absent only when Link is false.
	Destination Optional[string]
}

// TableOfContents is an atomic source-owned inventory. Unavailable means a
// candidate entry link lacks a resolved destination; Markers and Blocks are
// then nil. A complete inventory without markers has no Markers.
type TableOfContents struct {
	Markers      []TOCMarker
	Blocks       []TOCBlock
	Availability Availability
}

// TableOfContents projects marker and block facts from the actual completed
// parse, its confirmed comment inventory and the original line index, sharing
// the caller's budget. Failure withholds the complete inventory.
func (d *Document) TableOfContents(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, start, end string) (TableOfContents, error) {
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 1); err != nil {
		return TableOfContents{}, err
	}
	blocks, available, err := d.tocBlocks(ctx, budget)
	if err != nil {
		return TableOfContents{}, err
	}
	if !available {
		return TableOfContents{Availability: Unavailable}, d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0)
	}
	comments, err := d.Comments(ctx, budget)
	if err != nil {
		return TableOfContents{}, err
	}
	var markers []TOCMarker
	text := index.source.content
	var containers []int
	next := 0
	for _, comment := range comments {
		if err := headingFactCharge(ctx, budget, 4, 0); err != nil {
			return TableOfContents{}, err
		}
		if comment.Protected || !comment.Close.Present() {
			continue
		}
		e := comment.Envelope
		if e.SourceToken() != index.source.Token() || e.End() > len(text) {
			return TableOfContents{}, ErrModel
		}
		spelling := text[e.Start():e.End()]
		if spelling != start && spelling != end {
			continue
		}
		line := sort.Search(len(index.lines), func(i int) bool { return index.lines[i].end >= e.Start() })
		if err := headingFactCharge(ctx, budget, int64(len(index.lines)/64+1), 0); err != nil {
			return TableOfContents{}, err
		}
		if line == len(index.lines) || index.lines[line].start > e.Start() || index.lines[line].end < e.End() {
			continue
		}
		l := index.lines[line]
		if err := headingFactCharge(ctx, budget, int64(l.end-l.start)+1, 0); err != nil {
			return TableOfContents{}, err
		}
		if strings.Trim(text[l.start:e.Start()], " \t>") != "" || strings.Trim(text[e.End():l.end], " \t") != "" {
			continue
		}
		// Blocks are in preorder, so a single forward sweep keeps the stack of
		// containers that enclose the current marker.
		for next < len(blocks) && blocks[next].Envelope.Start() <= e.Start() {
			if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
				return TableOfContents{}, err
			}
			if k := blocks[next].Kind; k == BlockQuote || k == ListItem {
				containers = append(containers, next)
			}
			next++
		}
		for len(containers) != 0 && blocks[containers[len(containers)-1]].Envelope.End() < e.End() {
			containers = containers[:len(containers)-1]
		}
		container := -1
		if len(containers) != 0 {
			container = containers[len(containers)-1]
		}
		if err := openingHashGrowth(ctx, budget, len(markers), cap(markers)); err != nil {
			return TableOfContents{}, err
		}
		if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
			return TableOfContents{}, err
		}
		markers = append(markers, TOCMarker{Envelope: e, Line: index.span(l.start, l.next), Start: spelling == start, Container: container})
	}
	if err := d.headingProjectionCheckpoint(ctx, budget, index, options, 0, 0); err != nil {
		return TableOfContents{}, err
	}
	return TableOfContents{Markers: markers, Blocks: blocks, Availability: Available}, nil
}

func (d *Document) tocBlocks(ctx context.Context, budget *OperationBudget) ([]TOCBlock, bool, error) {
	type frame struct {
		node   *Node
		next   int
		parent int
	}
	stack := []frame{{node: d.root, parent: -1}}
	var out []TOCBlock
	text := d.Source().content
	for len(stack) != 0 {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return nil, false, err
		}
		f := &stack[len(stack)-1]
		if f.next == len(f.node.spec.Children) {
			stack = stack[:len(stack)-1]
			continue
		}
		n := f.node.spec.Children[f.next]
		f.next++
		b := TOCBlock{Kind: n.Kind(), Envelope: n.Span(), Parent: f.parent}
		if n.Kind() == ListItem {
			for _, m := range n.spec.Markers {
				if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
					return nil, false, err
				}
				if m.Role == "item-marker" {
					b.Marker = m.Source
				}
			}
			if !b.Marker.Present() || b.Marker.End() > len(text) {
				return nil, false, ErrModel
			}
			stop := b.Marker.End()
			for stop < len(text) && text[stop] != '\n' && text[stop] != '\r' {
				stop++
			}
			if err := headingFactCharge(ctx, budget, int64(stop-b.Marker.End())+1, 0); err != nil {
				return nil, false, err
			}
			b.Marker = Span{source: b.Marker.source, start: b.Marker.Start(), end: stop, present: true}
			if len(n.spec.Children) != 0 {
				if err := headingFactCharge(ctx, budget, int64(len(n.spec.Children[0].spec.Children))+1, 0); err != nil {
					return nil, false, err
				}
			}
			link := tocItemLink(n)
			if link != nil {
				p := link.spec.Properties.Link
				if p == nil || p.Resolution != Available || !p.Destination.Semantic.Present {
					return nil, false, nil
				}
				label, err := headingVisibleText(ctx, budget, link)
				if err != nil {
					return nil, false, err
				}
				b.Link, b.Label, b.Destination = true, label, p.Destination.Semantic
			}
		}
		if err := openingHashGrowth(ctx, budget, len(out), cap(out)); err != nil {
			return nil, false, err
		}
		if err := headingFactCharge(ctx, budget, 1, 1); err != nil {
			return nil, false, err
		}
		out = append(out, b)
		if len(n.spec.Children) != 0 && (n.Kind() == BlockQuote || n.Kind() == List || n.Kind() == ListItem) {
			if err := openingHashGrowth(ctx, budget, len(stack), cap(stack)); err != nil {
				return nil, false, err
			}
			stack = append(stack, frame{node: n, parent: len(out) - 1})
		}
	}
	return out, true, nil
}

// tocItemLink returns the only link of an item whose first block is a paragraph
// holding that link and, at most, whitespace-only text.
func tocItemLink(item *Node) *Node {
	if len(item.spec.Children) == 0 || item.spec.Children[0].Kind() != Paragraph {
		return nil
	}
	var link *Node
	for _, child := range item.spec.Children[0].spec.Children {
		switch {
		case child.Kind() == Text && child.spec.Properties.Literal.Present && strings.Trim(child.spec.Properties.Literal.Value, " \t") == "":
		case child.Kind() == Link && link == nil && child.spec.Properties.Link != nil:
			switch child.spec.Properties.Link.Origin {
			case "inline", "full", "collapsed", "shortcut":
				link = child
			default:
				return nil
			}
		default:
			return nil
		}
	}
	return link
}
