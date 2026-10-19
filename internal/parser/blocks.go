package parser

import (
	"context"
	"strings"
)

// blockLine is a borrowed view of one indexed physical line. Indent is capped
// at four: thematic breaks and heading markers allow fewer than four columns.
type blockLine struct {
	physicalLine
	contentStart int
	indent       int
	first        byte
	cursor       Indentation // after container prefixes, before leaf indentation
}

// A start hook runs before paragraph continuation. true defers interpretation;
// it does not recognize a block. Future providers can replace this conservative
// boundary when they implement the specification's actual precedence rules.
type blockStartHook func(blockLine, bool) bool

type openBlock struct {
	kind     Kind
	start    int
	last     blockLine
	content  []Segment
	end      int
	markers  []Marker
	children []*Node
	list     *ListProperties
	empty    bool // an item whose opening line was blank and has no later content
}

// Accumulation belongs exclusively to this parse. Freeze nodes once on closure;
// no mutable builder storage crosses the immutable model boundary.
type blockParser struct {
	index  LineIndex
	stack  []openBlock
	closed []*Node
	code   *indentedCode
	fence  *fencedCode
	html   *htmlBlock
	hooks  []blockStartHook
}

func parseBlocks(ctx context.Context, document *Document) (PhaseState, error) {
	return parseBlockLifecycle(ctx, document, []blockStartHook{pendingBlockStart})
}

func parseBlockLifecycle(ctx context.Context, document *Document, hooks []blockStartHook) (PhaseState, error) {
	document.references = ReferenceCollection{Availability: Unavailable, LookupAvailability: Unavailable}
	document.inlineFree = false
	if interruption(ctx) != "" {
		return Interrupted, nil
	}
	p := blockParser{index: NewLineIndex(document.Source()), stack: []openBlock{{kind: DocumentKind}}, hooks: hooks}
	state := PhaseComplete
	for _, physical := range p.index.lines {
		if interruption(ctx) != "" {
			state = Interrupted
			break
		}
		cursor := Indentation{index: p.index, offset: physical.start, end: physical.end}
		matched := 1
		for matched < p.containerDepth() {
			next, ok := p.matchContainer(matched, physical, cursor)
			if !ok {
				break
			}
			p.stack[matched].end = physical.next
			cursor = next
			matched++
		}
		line := p.logicalLine(physical, cursor)
		if matched < p.containerDepth() {
			// Unmatched containers may still own paragraph continuation text. In
			// particular an unmarked equals underline cannot promote a lazy paragraph.
			if p.paragraphOpen() && line.contentStart != line.end && !p.siblingItem(matched, line) && !p.interruptsParagraph(line) {
				for i := 1; i < p.containerDepth(); i++ {
					p.stack[i].end = physical.next
				}
				p.appendParagraph(line)
				continue
			}
			if err := p.closeLeaves(); err != nil {
				return Pending, err
			}
			if err := p.closeTo(matched); err != nil {
				return Pending, err
			}
		}
		for i := 1; i < p.containerDepth(); i++ {
			p.stack[i].end = physical.next
		}
		if p.html != nil {
			if p.html.form >= 6 && line.contentStart == line.end {
				if err := p.closeHTML(); err != nil {
					return Pending, err
				}
			} else {
				if err := p.appendHTMLLine(line); err != nil {
					return Pending, err
				}
				continue
			}
		}
		if p.fence != nil {
			if closing := p.fenceClosing(line); closing.Present() {
				p.fence.markers = append(p.fence.markers, Marker{Role: "closing", Source: closing})
				p.fence.end = line.next
				if err := p.closeFence(); err != nil {
					return Pending, err
				}
			} else {
				p.appendFenceLine(line)
			}
			continue
		}
		if p.code != nil {
			if line.contentStart == line.end {
				p.code.blanks = append(p.code.blanks, line)
				continue
			}
			if line.indent >= 4 {
				for _, blank := range p.code.blanks {
					p.appendCodeLine(blank)
				}
				p.code.blanks = nil
				p.appendCodeLine(line)
				continue
			}
			if err := p.closeCode(); err != nil {
				return Pending, err
			}
		}
	dispatch:
		// Continuing leaves have already consumed this line. A dedented code
		// line is reprocessed here too, so it can open a new quote container.
		for {
			next, prefix, ok := p.quotePrefix(cursor)
			if !ok {
				break
			}
			if err := p.closeTo(p.containerDepth()); err != nil {
				return Pending, err
			}
			p.stack = append(p.stack, openBlock{kind: BlockQuote, start: cursor.Offset(), end: physical.next,
				markers: []Marker{{Role: "quote-prefix", Source: prefix}}})
			cursor = next
		}
		line = p.logicalLine(physical, cursor)
		depth := p.containerDepth()
		if line.contentStart == line.end {
			if err := p.closeTo(depth); err != nil {
				return Pending, err
			}
			continue
		}
		if line.indent >= 4 && !p.paragraphOpen() {
			p.code = &indentedCode{start: line.start}
			p.appendCodeLine(line)
			continue
		}
		if fence := p.fenceOpening(line); fence != nil {
			if err := p.closeTo(depth); err != nil {
				return Pending, err
			}
			p.fence = fence
			if !fence.info.Semantic.Present {
				state = Pending // #19 supplies escape/entity info decoding.
			}
			continue
		}
		if form := p.htmlOpening(line, p.paragraphOpen()); form != 0 {
			if err := p.closeTo(depth); err != nil {
				return Pending, err
			}
			p.html = &htmlBlock{start: line.start, form: form}
			if err := p.appendHTMLLine(line); err != nil {
				return Pending, err
			}
			continue
		}
		setext, err := p.setextHeading(line)
		if err != nil {
			return Pending, err
		}
		// Promote marked, eligible paragraphs before thematic/list dispatch.
		if setext != nil {
			p.stack = p.stack[:depth]
			p.attach(setext)
			state = Pending
			continue
		}
		markers := p.thematicMarkers(line)
		if len(markers) > 0 {
			if err := p.closeTo(depth); err != nil {
				return Pending, err
			}
			node, err := NewNode(NodeSpec{Kind: ThematicBreak, Envelope: p.index.span(line.start, line.next), Markers: markers})
			if err != nil {
				return Pending, err
			}
			p.attach(node)
			continue
		}
		heading, err := p.atxHeading(line)
		if err != nil {
			return Pending, err
		}
		if heading != nil {
			if err := p.closeTo(depth); err != nil {
				return Pending, err
			}
			p.attach(heading)
			if len(heading.spec.Content) != 0 {
				state = Pending
			}
			continue
		}
		state = Pending
		if start, ok := classifyListStart(line, p.paragraphOpen()); ok {
			if err := p.closeTo(depth); err != nil {
				return Pending, err
			}
			item, err := start.openItem()
			if err != nil {
				return Pending, err
			}
			p.stack = append(p.stack, item)
			cursor = start.content
			// The first payload can itself open another item/quote, or any leaf.
			// Grouping/tightness still belong to #16; no List parent is guessed.
			goto dispatch
		}
		deferred := false
		for _, hook := range p.hooks {
			if hook(line, p.paragraphOpen()) {
				deferred = true
				break
			}
		}
		if deferred {
			// Do not invent the shape of an unavailable reference suffix. Keep
			// established quote prefixes/closed children, but discard an open paragraph.
			p.stack = p.stack[:depth]
			break
		}
		p.appendParagraph(line)
	}
	if interruption(ctx) != "" {
		state = Interrupted
	}
	if state == Interrupted {
		// An unfinished container is not a normal unclosed-EOF result. Only root
		// children already attached before interruption survive.
		p.stack = p.stack[:1]
		p.code, p.fence, p.html = nil, nil, nil
	}
	if err := p.closeLeaves(); err != nil {
		return Pending, err
	}
	if err := p.closeTo(1); err != nil {
		return Pending, err
	}
	root, err := NewNode(NodeSpec{Kind: DocumentKind, Envelope: p.index.span(0, p.index.source.Len()), Children: p.closed})
	if err != nil {
		return Pending, err
	}
	document.root = root
	if state == PhaseComplete {
		// Recognition proves that every container/leaf is established inline-free.
		document.references = ReferenceCollection{Availability: Available, LookupAvailability: Available}
		document.inlineFree = true
	}
	return state, nil
}

// atxHeading classifies original bytes before acquiring immutable metadata.
// Closing hashes are block syntax; their escaped/nonclosing spellings remain
// raw content for the later inline decoder. All trimming is ASCII space/tab.
func (p *blockParser) atxHeading(line blockLine) (*Node, error) {
	if line.indent >= 4 || line.first != '#' {
		return nil, nil
	}
	text := p.index.source.content
	openingEnd := line.contentStart
	for openingEnd < line.end && text[openingEnd] == '#' {
		openingEnd++
		if openingEnd-line.contentStart > 6 {
			return nil, nil
		}
	}
	if openingEnd < line.end && text[openingEnd] != ' ' && text[openingEnd] != '\t' {
		return nil, nil
	}
	start := openingEnd
	for start < line.end && (text[start] == ' ' || text[start] == '\t') {
		start++
	}
	end := line.end
	for end > start && (text[end-1] == ' ' || text[end-1] == '\t') {
		end--
	}
	markers := []Marker{{Role: "opening", Source: p.index.span(line.contentStart, openingEnd)}}
	closingStart := end
	for closingStart > start && text[closingStart-1] == '#' {
		closingStart--
	}
	if closingStart < end && (text[closingStart-1] == ' ' || text[closingStart-1] == '\t') {
		markers = append(markers, Marker{Role: "closing", Source: p.index.span(closingStart, end)})
		end = closingStart
		for end > start && (text[end-1] == ' ' || text[end-1] == '\t') {
			end--
		}
	}
	return NewNode(NodeSpec{
		Kind: Heading, Envelope: p.index.span(line.start, line.next), Markers: markers,
		Content:    p.index.semanticText(start, end).Segments,
		Properties: Properties{Heading: &HeadingProperties{Level: openingEnd - line.contentStart, Style: "atx"}},
	})
}

// setextHeading validates the complete underline before freezing an eligible
// paragraph. Its delayed last line contributes content without its ending;
// the envelope, unlike content and marker, retains both physical line endings.
func (p *blockParser) setextHeading(line blockLine) (*Node, error) {
	if !p.paragraphOpen() || line.indent >= 4 || line.first != '=' && line.first != '-' {
		return nil, nil
	}
	text := p.index.source.content
	end := line.contentStart
	for end < line.end && text[end] == line.first {
		end++
	}
	for i := end; i < line.end; i++ {
		if text[i] != ' ' && text[i] != '\t' {
			return nil, nil
		}
	}
	level := 1
	if line.first == '-' {
		level = 2
	}
	paragraph := p.stack[len(p.stack)-1]
	return NewNode(NodeSpec{
		Kind: Heading, Envelope: p.index.span(paragraph.start, line.next),
		Content:    p.paragraphContent(paragraph),
		Markers:    []Marker{{Role: "underline", Source: p.index.span(line.contentStart, end)}},
		Properties: Properties{Heading: &HeadingProperties{Level: level, Style: "setext"}},
	})
}

// thematicMarkers validates one whole physical line before allocating metadata.
// Each maximal contiguous marker run gets its exact original-byte range; spaces,
// tabs and endings remain in the envelope, never in a marker. A single dash run
// could instead underline an open paragraph, handled before this recognizer.
func (p *blockParser) thematicMarkers(line blockLine) (markers []Marker) {
	if line.indent >= 4 || !strings.ContainsRune("*-_", rune(line.first)) {
		return nil
	}
	text := p.index.source.content
	count, last := 0, line.contentStart
	for i := line.contentStart; i < line.end; i++ {
		switch text[i] {
		case line.first:
			count++
			last = i
		case ' ', '\t':
		default:
			return nil
		}
	}
	if count < 3 {
		return nil
	}
	for i := line.contentStart; i <= last; {
		if text[i] != line.first {
			i++
			continue
		}
		start := i
		for i <= last && text[i] == line.first {
			i++
		}
		markers = append(markers, Marker{Role: "thematic-marker", Source: p.index.span(start, i)})
	}
	return markers
}

// Classify without acquiring semantic suffixes at every quote depth. The cursor
// retains physical tab-column phase; indent is relative to the current container.
func (p *blockParser) logicalLine(physical physicalLine, cursor Indentation) blockLine {
	physical.start = cursor.Offset()
	line := blockLine{physicalLine: physical, cursor: cursor}
	c := cursor
	text := p.index.source.content
	for c.offset < c.end {
		switch text[c.offset] {
		case ' ':
			c.column++
		case '\t':
			if c.tabRemaining != 0 {
				c.column += c.tabRemaining
				c.tabRemaining = 0
			} else {
				c.column += 4 - c.column%4
			}
		default:
			line.contentStart, line.indent = c.offset, min(4, c.column-cursor.column)
			line.first = text[c.offset]
			return line
		}
		c.offset++
	}
	line.contentStart, line.indent = c.offset, min(4, c.column-cursor.column)
	return line
}

func pendingBlockStart(line blockLine, paragraphOpen bool) bool {
	if line.indent >= 4 {
		// Indented code cannot interrupt an existing paragraph.
		return !paragraphOpen
	}
	// Lists, fences and HTML have their own grammar-aware classifiers. Invalid
	// candidates are ordinary paragraph content, still awaiting inlines. Reference
	// definitions retain their conservative boundary until #17.
	return line.first == '['
}

func (p *blockParser) appendParagraph(line blockLine) {
	if !p.paragraphOpen() {
		p.stack = append(p.stack, openBlock{kind: Paragraph, start: line.start, last: line})
		return
	}
	paragraph := &p.stack[len(p.stack)-1]
	// Keep interior trailing spaces and the complete original ending. Delay the
	// final line's contribution so closure can trim only paragraph-final bytes.
	paragraph.content = append(paragraph.content, p.index.semanticText(paragraph.last.contentStart, paragraph.last.next).Segments...)
	paragraph.last = line
}

func (p *blockParser) closeTo(depth int) error {
	for len(p.stack) > depth {
		block := p.stack[len(p.stack)-1]
		spec := NodeSpec{Kind: block.kind, Envelope: p.index.span(block.start, block.end), Markers: block.markers, Children: block.children}
		if block.list != nil {
			spec.Properties.List = block.list
		}
		if block.kind == Paragraph {
			spec.Envelope = p.index.span(block.start, block.last.next)
			spec.Content = p.paragraphContent(block)
		}
		node, err := NewNode(spec)
		if err != nil {
			return err
		}
		p.stack = p.stack[:len(p.stack)-1]
		p.attach(node)
	}
	return nil
}

// The private builder owns the segment backing storage until NewNode freezes it.
// This is called once on closure/promotion, never for each continuation line.
func (p *blockParser) paragraphContent(paragraph openBlock) []Segment {
	end := paragraph.last.end
	text := p.index.source.content
	for end > paragraph.last.contentStart && (text[end-1] == ' ' || text[end-1] == '\t') {
		end--
	}
	return append(paragraph.content, p.index.semanticText(paragraph.last.contentStart, end).Segments...)
}
