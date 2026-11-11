// Facts for MD070.

package parser

import (
	"context"
	"errors"
	"slices"
	"strings"
)

// NestedExampleFact selects an original manual problem, including insertion
// points for missing boundaries. It carries no edit or derived-source authority.
type NestedExampleFact struct {
	Span Span
	Kind string
}

// NestedExampleInventory is atomic: unknown necessary ownership withholds all
// observations, including observations from earlier independent examples.
type NestedExampleInventory struct {
	Facts        []NestedExampleFact
	Availability Availability
}

const nestedExampleOpen = "<!-- mdchk-example -->"
const nestedExampleClose = "<!-- /mdchk-example -->"

type nestedExampleBody struct {
	node                *Node
	parent, first, last int
	cursors             []Indentation
	known               []bool
}

type nestedExampleNode struct {
	node *Node
	body int
}
type nestedExampleLine struct {
	body, start, end           int
	opening, closing, eligible bool
}
type nestedExamplePair struct{ body, opening, closing int }
type nestedExampleOpening struct {
	offset, order int
	node          *Node
}
type nestedExampleModel struct {
	bodies   []nestedExampleBody
	nodes    []nestedExampleNode
	lines    []nestedExampleLine
	openings []nestedExampleOpening
}

// NestedExamples uses original comments and actual container identities to bound
// examples, then recognizes their mapped payload with the same parser profile and
// cumulative operation allowance. Derived parsing never executes lint directives.
func (d *Document) NestedExamples(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out NestedExampleInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = NestedExampleInventory{}
		} else if out.Availability != Available {
			out.Facts = nil
		}
	}()
	// A document without this exact lexical spelling needs no marker-specific
	// model context. Reserve the entire search before inspecting source bytes.
	if err = headingFactCharge(ctx, budget, int64(index.source.Len())+1, 0); err != nil {
		return out, err
	}
	if !strings.Contains(index.source.content, nestedExampleOpen) {
		return NestedExampleInventory{Availability: Available}, nil
	}
	var model nestedExampleModel
	err = nestedExampleLocked(ctx, budget, index, func(work *parserContext, idx LineIndex) error {
		var e error
		model, e = d.nestedExampleModel(work, idx)
		return e
	})
	if err != nil {
		return out, err
	}
	available, err := model.eligibility(ctx, budget, index)
	if err != nil || !available {
		return NestedExampleInventory{Availability: Unavailable}, err
	}
	var pairs []nestedExamplePair
	err = nestedExampleLocked(ctx, budget, index, func(work *parserContext, idx LineIndex) error {
		var e error
		pairs, available, e = model.pairs(work, idx)
		return e
	})
	if err != nil || !available {
		return NestedExampleInventory{Availability: Unavailable}, err
	}
	for i := 0; ; i++ {
		if err = headingFactCharge(ctx, budget, 1, 0); err != nil {
			return out, err
		}
		if i == len(pairs) {
			break
		}
		var prepared nestedExamplePayload
		err = nestedExampleLocked(ctx, budget, index, func(work *parserContext, idx LineIndex) error {
			var e error
			prepared, available, e = model.prepare(work, idx, pairs[i])
			return e
		})
		if err != nil || !available {
			return NestedExampleInventory{Availability: Unavailable}, err
		}
		fact := prepared.problem
		if !fact.Span.present {
			// Public parsing acquires its own budget lock; no projection lock is
			// held here. Only frontmatter differs from the original selection.
			parsed := ParseWithOptions(ctx, prepared.source, budget, ParseOptions{GFM: options.GFM})
			if parsed.Limit != nil {
				return out, parsed.Limit
			}
			// The parser owns the interruption it observed. Retain that cause
			// even if a caller's later Err observation no longer exposes it.
			switch parsed.Interruption {
			case Cancelled:
				return out, context.Canceled
			case DeadlineExceeded:
				return out, context.DeadlineExceeded
			default:
				// Limits were handled above; syntax availability is checked below.
			}
			if err = headingFactCharge(ctx, budget, 1, 0); err != nil {
				return out, err
			}
			if parsed.Failure != nil {
				return out, parsed.Failure
			}
			if parsed.Status != Complete || parsed.AST != Available || parsed.Document == nil {
				return NestedExampleInventory{Availability: Unavailable}, nil
			}
			var bad bool
			bad, available, err = prepared.compare(ctx, budget, parsed.Document)
			if err != nil || !available {
				return NestedExampleInventory{Availability: Unavailable}, err
			}
			if !bad {
				continue
			}
			fact = NestedExampleFact{Span: prepared.opening, Kind: "length"}
		}
		out.Facts, err = shellPromptGrow(ctx, budget, out.Facts)
		if err != nil {
			return out, err
		}
		if err = headingFactCharge(ctx, budget, 1, 1); err != nil {
			return out, err
		}
		out.Facts = append(out.Facts, fact)
		// Pair order is extent order, but a parent's late structural problem can
		// follow a nested container's problem. Publish original-location order.
		for at := len(out.Facts) - 1; at > 0; at-- {
			if err = headingFactCharge(ctx, budget, 2, 0); err != nil {
				return out, err
			}
			if out.Facts[at-1].Span.start <= out.Facts[at].Span.start {
				break
			}
			out.Facts[at-1], out.Facts[at] = out.Facts[at], out.Facts[at-1]
		}
	}
	out.Availability = Available
	return out, nil
}

func nestedExampleAppend[T any](ctx context.Context, values []T, value T) []T {
	values = htmlMarkdownGrow(ctx, values)
	meterWork(ctx, 1)
	meterRecords(ctx, 1)
	return append(values, value)
}

func nestedExampleLineAt(ctx context.Context, index LineIndex, offset int) (int, error) {
	lo, hi := 0, len(index.lines)
	for lo < hi {
		meterWork(ctx, 1)
		mid := lo + (hi-lo)/2
		if index.lines[mid].start <= offset {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	meterWork(ctx, 8)
	if lo == 0 || offset < 0 || offset > index.source.Len() {
		return 0, ErrModel
	}
	return lo - 1, nil
}

func (d *Document) nestedExampleModel(ctx context.Context, index LineIndex) (m nestedExampleModel, err error) {
	m.lines = htmlMarkdownSlice[nestedExampleLine](ctx, len(index.lines))
	type visit struct {
		node   *Node
		parent int
	}
	stack := nestedExampleAppend(ctx, []visit(nil), visit{d.root, -1})
	for len(stack) != 0 {
		meterWork(ctx, 32)
		if err := ctx.Err(); err != nil {
			return m, err
		}
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := v.node
		if n == nil || !validKind(n.spec.Kind) || !n.spec.Envelope.present || n.spec.Envelope.SourceToken() != index.source.Token() || n.spec.Envelope.start < 0 || n.spec.Envelope.end < n.spec.Envelope.start || n.spec.Envelope.end > index.source.Len() {
			return m, ErrModel
		}
		body := v.parent
		if n.Kind() == DocumentKind || n.Kind() == BlockQuote || n.Kind() == ListItem {
			first, e := nestedExampleLineAt(ctx, index, n.spec.Envelope.start)
			if e != nil {
				return m, e
			}
			last, e := nestedExampleLineAt(ctx, index, max(n.spec.Envelope.start, n.spec.Envelope.end-1))
			if e != nil {
				return m, e
			}
			b := nestedExampleBody{node: n, parent: v.parent, first: first, last: last}
			b.cursors = htmlMarkdownSlice[Indentation](ctx, last-first+1)
			b.known = htmlMarkdownSlice[bool](ctx, last-first+1)
			body = len(m.bodies)
			m.bodies = nestedExampleAppend(ctx, m.bodies, b)
			if err = m.projectContainer(ctx, index, body); err != nil {
				return m, err
			}
			for line := first; line <= last; line++ {
				meterWork(ctx, 1)
				m.lines[line].body = body
			}
		}
		if body < 0 {
			return m, ErrModel
		}
		m.nodes = nestedExampleAppend(ctx, m.nodes, nestedExampleNode{n, body})
		meterWork(ctx, 1)
		if n.Kind() == CodeBlock && len(n.spec.Markers) != 0 {
			marker := n.spec.Markers[0]
			meterWork(ctx, len(marker.Role)+16)
			if marker.Role == "opening" {
				// Retain candidate identity only. Required span/info authority is
				// still checked at selection, not demanded from unrelated blocks.
				m.openings = nestedExampleAppend(ctx, m.openings, nestedExampleOpening{
					offset: marker.Source.start, order: len(m.nodes) - 1, node: n,
				})
			}
		}
		switch n.Kind() {
		case CodeBlock, CodeSpan, HTMLBlock, Image:
			continue
		default:
			// Other original owners retain their ordinary children and tails.
		}
		for group := 0; group < 2; group++ {
			meterWork(ctx, 1)
			children := n.htmlTail
			if group == 1 {
				children = n.spec.Children
			}
			for i := len(children) - 1; i >= 0; i-- {
				meterWork(ctx, 16)
				child := children[i]
				if child == nil || child.spec.Envelope.start < n.spec.Envelope.start || child.spec.Envelope.end > n.spec.Envelope.end {
					return m, ErrModel
				}
				stack = nestedExampleAppend(ctx, stack, visit{child, body})
			}
		}
	}
	// Owned HTML tails and nested containers need not have globally monotone
	// preorder offsets. Sort this dedicated index explicitly, retaining traversal
	// order for equal offsets to preserve first-matching-owner selection.
	slices.SortFunc(m.openings, func(a, b nestedExampleOpening) int {
		meterWork(ctx, 4)
		if a.offset < b.offset {
			return -1
		}
		if a.offset > b.offset {
			return 1
		}
		if a.order < b.order {
			return -1
		}
		if a.order > b.order {
			return 1
		}
		return 0
	})
	for i := range m.lines {
		meterWork(ctx, 1)
		if err := ctx.Err(); err != nil {
			return m, err
		}
		line := &m.lines[i]
		b := &m.bodies[line.body]
		if !b.known[i-b.first] {
			continue
		}
		cursor := b.cursors[i-b.first]
		start, end := cursor.offset, cursor.end
		for start < end && (index.source.content[start] == ' ' || index.source.content[start] == '\t') {
			meterWork(ctx, 1)
			start++
		}
		for end > start && (index.source.content[end-1] == ' ' || index.source.content[end-1] == '\t') {
			meterWork(ctx, 1)
			end--
		}
		meterWork(ctx, 2*(end-start)+1)
		line.start, line.end = start, end
		line.opening = index.source.content[start:end] == nestedExampleOpen
		line.closing = index.source.content[start:end] == nestedExampleClose
	}
	return m, nil
}

// originalOpening associates an intended run with its original owner in
// logarithmic lookup work. Equal-offset candidates retain traversal precedence;
// their envelope test is the same one used by the former full-node search.
func (m *nestedExampleModel) originalOpening(ctx context.Context, span Span) (*Node, error) {
	lo, hi := 0, len(m.openings)
	for lo < hi {
		meterWork(ctx, 1)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		mid := lo + (hi-lo)/2
		if m.openings[mid].offset < span.start {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	for {
		meterWork(ctx, 1)
		if lo == len(m.openings) || m.openings[lo].offset != span.start {
			return nil, nil
		}
		meterWork(ctx, 16)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n := m.openings[lo].node
		if n.spec.Envelope.start <= span.start && n.spec.Envelope.end >= span.end {
			return n, nil
		}
		lo++
	}
}

// Every cursor is rebuilt from the real parent's cursor and the child's retained
// prefix marker. Grammar validates that marker; it never invents a container.
// A missing prefix that grammar would consume is unknown, whereas a failed
// continuation is the original parser's lazy-line decision and removes nothing.
func (m *nestedExampleModel) projectContainer(ctx context.Context, index LineIndex, id int) error {
	b := &m.bodies[id]
	p := blockParser{ctx: ctx, index: index}
	prefixes := htmlMarkdownSlice[Marker](ctx, b.last-b.first+1)
	for _, marker := range b.node.spec.Markers {
		meterWork(ctx, len(marker.Role)+16)
		s := marker.Source
		if !s.present || s.SourceToken() != index.source.Token() || s.start < b.node.spec.Envelope.start || s.end < s.start || s.end > b.node.spec.Envelope.end {
			return ErrModel
		}
		if marker.Role != "quote-prefix" && marker.Role != "item-prefix" && marker.Role != "item-indent" {
			continue
		}
		line, err := nestedExampleLineAt(ctx, index, s.start)
		if err != nil {
			return err
		}
		if line < b.first || line > b.last || s.end > index.lines[line].end || prefixes[line-b.first].Source.present {
			return ErrModel
		}
		prefixes[line-b.first] = marker
	}
	for line := b.first; line <= b.last; line++ {
		meterWork(ctx, 16)
		if err := ctx.Err(); err != nil {
			return err
		}
		cursor, _ := index.Indentation(line + 1)
		known := true
		if b.parent >= 0 {
			parent := &m.bodies[b.parent]
			if line < parent.first || line > parent.last {
				return ErrModel
			}
			cursor, known = parent.cursors[line-parent.first], parent.known[line-parent.first]
		}
		marker := prefixes[line-b.first]
		next, actual, matched := cursor, Span{}, false
		switch b.node.Kind() {
		case DocumentKind:
		case BlockQuote:
			next, actual, matched = p.quotePrefix(cursor)
		case ListItem:
			props := b.node.spec.Properties.List
			if props == nil || !props.ContentIndent.Present {
				known = false
				break
			}
			if props.ContentIndent.Value < 1 {
				return ErrModel
			}
			if line == b.first {
				start, ok := classifyListStart(p.logicalLine(index.lines[line], cursor), false)
				matched = ok
				if ok {
					if start.properties.ContentIndent != props.ContentIndent {
						return ErrModel
					}
					next = start.content
					end := next.offset
					if next.tabRemaining != 0 {
						end++
					}
					actual = index.span(cursor.offset, end)
				}
			} else {
				consumed, _ := next.Consume(props.ContentIndent.Value)
				matched = consumed.Columns == props.ContentIndent.Value || p.logicalLine(index.lines[line], cursor).contentStart == index.lines[line].end
				if len(consumed.Content.Segments) != 0 {
					actual = index.span(cursor.offset, consumed.Content.Segments[len(consumed.Content.Segments)-1].Source.end)
				}
			}
		default:
			return ErrModel // Only actual document, quote and item bodies are retained.
		}
		if !known {
			b.cursors[line-b.first], b.known[line-b.first] = cursor, false
			continue
		}
		if b.node.Kind() != DocumentKind {
			if marker.Source.present {
				if !matched || !actual.present || actual.start != marker.Source.start || actual.end != marker.Source.end {
					return ErrModel
				}
				cursor = next
			} else if matched && actual.present {
				known = false
			}
		}
		b.cursors[line-b.first], b.known[line-b.first] = cursor, known
	}
	return nil
}

func (m *nestedExampleModel) pairs(ctx context.Context, index LineIndex) ([]nestedExamplePair, bool, error) {
	type group struct {
		opening, closing, depth int
		active, ambiguous       bool
	}
	groups := htmlMarkdownSlice[group](ctx, len(m.bodies))
	var pairs []nestedExamplePair
	finish := func(id int) {
		meterWork(ctx, 1)
		g := &groups[id]
		if g.active && !g.ambiguous && g.depth == 0 {
			pairs = nestedExampleAppend(ctx, pairs, nestedExamplePair{id, g.opening, g.closing})
		}
		*g = group{}
	}
	for i, line := range m.lines {
		meterWork(ctx, 4)
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		g := &groups[line.body]
		b := &m.bodies[line.body]
		if !b.known[i-b.first] && g.active {
			return nil, false, nil
		}
		if line.eligible {
			if g.active && g.depth == 0 {
				finish(line.body)
			}
			if !g.active {
				*g = group{opening: i, depth: 1, active: true}
			} else {
				g.depth++
				g.ambiguous = true
			}
		} else if line.closing && g.active {
			if g.depth == 0 {
				g.ambiguous = true
			} else {
				g.depth--
				if g.depth == 0 {
					g.closing = i
				}
			}
		}
	}
	for id := range groups {
		meterWork(ctx, 1)
		finish(id)
	}
	slices.SortFunc(pairs, func(a, b nestedExamplePair) int { meterWork(ctx, 1); return a.opening - b.opening })
	return pairs, true, nil
}
