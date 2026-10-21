package parser

import (
	"context"
	"sort"
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
	lazy         bool        // at least one enclosing container prefix was absent
}

// A start hook runs before paragraph continuation. true defers interpretation;
// it does not recognize a block. Future providers can replace this conservative
// boundary when they implement the specification's actual precedence rules.
type blockStartHook func(blockLine, bool) bool

type openBlock struct {
	kind        Kind
	start       int
	last        blockLine
	content     []Segment
	end         int
	markers     []Marker
	children    []parsedBlock
	list        *ListProperties
	empty       bool // an item whose opening line was blank and has no later content
	partial     bool // an unavailable suffix prevents final list interpretation
	reference   *referenceParagraph
	definitions []Span // non-rendered direct definition boundaries (item-only)
}

// Accumulation belongs exclusively to this parse. Freeze nodes once on closure;
// no mutable builder storage crosses the immutable model boundary.
type blockParser struct {
	index                     LineIndex
	stack                     []openBlock
	closed                    []parsedBlock
	code                      *indentedCode
	fence                     *fencedCode
	html                      *htmlBlock
	hooks                     []blockStartHook
	ctx                       context.Context
	definitions               []*ReferenceDefinition
	inlinePayload             bool
	referenceSemanticsPending bool
	referenceInterrupted      bool
	deferred                  bool
}

func parseBlocks(ctx context.Context, document *Document) (PhaseState, error) {
	meterWork(ctx, 1)
	return parseBlockLifecycle(ctx, document, nil)
}

func parseBlockLifecycle(ctx context.Context, document *Document, hooks []blockStartHook) (PhaseState, error) {
	meterWork(ctx, 1)
	document.references = ReferenceCollection{Availability: Unavailable, LookupAvailability: Unavailable}
	document.inlineFree = false
	document.blocksRecognized = false
	if interruption(ctx) != "" {
		return Interrupted, nil
	}
	p := blockParser{index: newLineIndex(ctx, document.Source()), stack: []openBlock{{kind: DocumentKind}}, hooks: hooks, ctx: ctx}
	state := PhaseComplete
	for _, physical := range p.index.lines {
		meterWork(ctx, 1)
		if interruption(ctx) != "" {
			state = Interrupted
			break
		}
		cursor := Indentation{index: p.index, offset: physical.start, end: physical.end}
		matched := 1
		for matched < p.containerDepth() {
			meterWork(ctx, 1)
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
			if p.paragraphOpen() && line.contentStart != line.end && !p.siblingItem(matched, line) && !p.interruptsParagraph(line) && p.paragraphAllowsLazy(physical) {
				for i := 1; i < p.containerDepth(); i++ {
					meterWork(ctx, 1)
					p.stack[i].end = physical.next
				}
				line.lazy = true
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
			meterWork(ctx, 1)
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
				p.fence.markers = meteredAppend(ctx, p.fence.markers, Marker{Role: "closing", Source: closing})
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
				p.code.blanks = meteredAppend(ctx, p.code.blanks, line)
				continue
			}
			if line.indent >= 4 {
				for _, blank := range p.code.blanks {
					meterWork(ctx, 1)
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
			meterWork(ctx, 1)
			next, prefix, ok := p.quotePrefix(cursor)
			if !ok {
				break
			}
			if err := p.closeTo(p.containerDepth()); err != nil {
				return Pending, err
			}
			checkDepth(ctx, p.syntaxDepth()+1)
			p.stack = meteredAppend(ctx, p.stack, openBlock{kind: BlockQuote, start: cursor.Offset(), end: physical.next,
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
		// Nonempty dispatch necessarily opens or continues a leaf/definition.
		// Check its combined container depth before allocating leaf metadata.
		checkDepth(p.ctx, p.containerSyntaxDepth()+1)
		if line.indent >= 4 && !p.paragraphOpen() {
			checkDepth(p.ctx, p.syntaxDepth()+1)
			p.code = &indentedCode{start: line.start}
			p.appendCodeLine(line)
			continue
		}
		if fence := p.fenceOpening(line); fence != nil {
			if err := p.closeTo(depth); err != nil {
				return Pending, err
			}
			checkDepth(p.ctx, p.syntaxDepth()+1)
			p.fence = fence
			if !fence.info.Semantic.Present {
				state = Pending // interrupted info decoding cannot establish semantics.
			}
			continue
		}
		if form := p.htmlOpening(line, p.paragraphOpen()); form != 0 {
			if err := p.closeTo(depth); err != nil {
				return Pending, err
			}
			checkDepth(p.ctx, p.syntaxDepth()+1)
			p.html = &htmlBlock{start: line.start, form: form}
			if form == 7 {
				raw := p.index.source.content[line.contentStart:line.end]
				if len(raw) > 2 && raw[1] == '/' {
					end := htmlTagNameEndContext(p.ctx, raw, 2)
					if end > 2 && htmlRawTag(raw[2:end]) {
						p.html.rawTag = strings.ToLower(raw[2:end])
						p.html.rawClosing = true
					}
				}
			}
			if form == 2 {
				p.html.open = p.index.span(line.contentStart, line.contentStart+4)
			}
			if err := p.appendHTMLLine(line); err != nil {
				return Pending, err
			}
			continue
		}
		setext, err := p.setextHeading(line)
		if err != nil {
			return Pending, err
		}
		if p.referenceInterrupted {
			state = Interrupted
			break
		}
		// Promote marked, eligible paragraphs before thematic/list dispatch.
		if setext != nil {
			p.stack = p.stack[:depth]
			p.attach(setext)
			p.inlinePayload = true
			continue
		}
		markers := p.thematicMarkers(line)
		if len(markers) > 0 {
			if err := p.closeTo(depth); err != nil {
				return Pending, err
			}
			node, err := meteredNode(p.ctx, NodeSpec{Kind: ThematicBreak, Envelope: p.index.span(line.start, line.next), Markers: markers})
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
				p.inlinePayload = true
			}
			continue
		}
		if start, ok := classifyListStart(line, p.paragraphOpen()); ok {
			if err := p.closeTo(depth); err != nil {
				return Pending, err
			}
			checkDepth(p.ctx, p.syntaxDepth()+2)
			item, err := start.openItem()
			if err != nil {
				return Pending, err
			}
			p.stack = meteredAppend(ctx, p.stack, item)
			cursor = start.content
			// The first payload can itself open another item/quote, or any leaf.
			// Completed siblings are grouped once when their parent closes.
			goto dispatch
		}
		deferred := false
		for _, hook := range p.hooks {
			meterWork(ctx, 1)
			if hook(line, p.paragraphOpen()) {
				deferred = true
				break
			}
		}
		if deferred {
			p.deferred = true
			state = Pending
			// Do not invent the shape of an unavailable reference suffix. Keep
			// established quote prefixes/closed children, but discard an open paragraph.
			p.stack = p.stack[:depth]
			for i := range p.stack {
				meterWork(ctx, 1)
				p.stack[i].partial = true
			}
			break
		}
		p.appendParagraph(line)
	}
	if interruption(ctx) != "" || p.referenceInterrupted {
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
	if p.referenceInterrupted {
		state = Interrupted
	}
	if state != Interrupted && p.referenceSemanticsPending {
		state = Pending
	}
	children, err := p.groupLists(p.closed)
	if err != nil {
		return Pending, err
	}
	root, err := meteredNode(p.ctx, NodeSpec{Kind: DocumentKind, Envelope: p.index.span(0, p.index.source.Len()), Children: blockNodesContext(p.ctx, children)})
	if err != nil {
		return Pending, err
	}
	document.root = root
	if state != Interrupted && !p.deferred {
		sort.SliceStable(p.definitions, func(i, j int) bool {
			meterWork(p.ctx, 1)
			return p.definitions[i].spec.Envelope.Start() < p.definitions[j].spec.Envelope.Start()
		})
		lookup := make(map[string]int)
		for i, definition := range p.definitions {
			meterWork(ctx, 1)
			label := definition.spec.NormalizedLabel.Value
			meterWork(p.ctx, len(label)*2)
			if _, exists := lookup[label]; !exists {
				lookup[label] = i
			}
		}
		document.references = ReferenceCollection{Availability: Available, LookupAvailability: Available, Definitions: p.definitions, Lookup: lookup}
		document.inlineFree = state == PhaseComplete && !p.inlinePayload
		document.blocksRecognized = state == PhaseComplete
	}
	return state, nil
}

// atxHeading classifies original bytes before acquiring immutable metadata.
// Closing hashes are block syntax; their escaped/nonclosing spellings remain
// raw content for the later inline decoder. All trimming is ASCII space/tab.
func (p *blockParser) atxHeading(line blockLine) (*Node, error) {
	meterWork(p.ctx, 1)
	if line.indent >= 4 || line.first != '#' {
		return nil, nil
	}
	text := p.index.source.content
	openingEnd := line.contentStart
	for openingEnd < line.end && text[openingEnd] == '#' {
		meterWork(p.ctx, 1)
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
		meterWork(p.ctx, 1)
		start++
	}
	end := line.end
	for end > start && (text[end-1] == ' ' || text[end-1] == '\t') {
		meterWork(p.ctx, 1)
		end--
	}
	markers := []Marker{{Role: "opening", Source: p.index.span(line.contentStart, openingEnd)}}
	closingStart := end
	for closingStart > start && text[closingStart-1] == '#' {
		meterWork(p.ctx, 1)
		closingStart--
	}
	if closingStart < end && (text[closingStart-1] == ' ' || text[closingStart-1] == '\t') {
		markers = meteredAppend(p.ctx, markers, Marker{Role: "closing", Source: p.index.span(closingStart, end)})
		end = closingStart
		for end > start && (text[end-1] == ' ' || text[end-1] == '\t') {
			meterWork(p.ctx, 1)
			end--
		}
	}
	return meteredNode(p.ctx, NodeSpec{
		Kind: Heading, Envelope: p.index.span(line.start, line.next), Markers: markers,
		Content:    p.index.semanticText(start, end).Segments,
		Properties: Properties{Heading: &HeadingProperties{Level: openingEnd - line.contentStart, Style: "atx"}},
	})
}

// setextHeading validates the complete underline before freezing an eligible
// paragraph. Its delayed last line contributes content without its ending;
// the envelope, unlike content and marker, retains both physical line endings.
func (p *blockParser) setextHeading(line blockLine) (*Node, error) {
	meterWork(p.ctx, 1)
	if !p.paragraphOpen() {
		return nil, nil
	}
	end, level := p.setextUnderline(line)
	if level == 0 {
		return nil, nil
	}
	if err := p.stripReferences(); err != nil {
		return nil, err
	}
	if !p.paragraphOpen() || p.referenceInterrupted {
		return nil, nil
	}
	paragraph := p.stack[len(p.stack)-1]
	return meteredNode(p.ctx, NodeSpec{
		Kind: Heading, Envelope: p.index.span(paragraph.start, line.next),
		Content:    p.paragraphContent(paragraph),
		Markers:    []Marker{{Role: "underline", Source: p.index.span(line.contentStart, end)}},
		Properties: Properties{Heading: &HeadingProperties{Level: level, Style: "setext"}},
	})
}

// Classification is shared with read-only reference lookahead. Unlike heading
// promotion, it does not extract definitions or mutate the open paragraph.
func (p *blockParser) setextUnderline(line blockLine) (end, level int) {
	meterWork(p.ctx, 1)
	if line.indent >= 4 || line.first != '=' && line.first != '-' {
		return 0, 0
	}
	text := p.index.source.content
	end = line.contentStart
	for end < line.end && text[end] == line.first {
		meterWork(p.ctx, 1)
		end++
	}
	for i := end; i < line.end; i++ {
		meterWork(p.ctx, 1)
		if text[i] != ' ' && text[i] != '\t' {
			return 0, 0
		}
	}
	level = 1
	if line.first == '-' {
		level = 2
	}
	return end, level
}

// thematicMarkers validates one whole physical line before allocating metadata.
// Each maximal contiguous marker run gets its exact original-byte range; spaces,
// tabs and endings remain in the envelope, never in a marker. A single dash run
// could instead underline an open paragraph, handled before this recognizer.
func (p *blockParser) thematicMarkers(line blockLine) (markers []Marker) {
	meterWork(p.ctx, 1)
	if line.indent >= 4 || !meteredContainsRune(p.ctx, "*-_", rune(line.first)) {
		return nil
	}
	text := p.index.source.content
	count, last := 0, line.contentStart
	for i := line.contentStart; i < line.end; i++ {
		meterWork(p.ctx, 1)
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
		meterWork(p.ctx, 1)
		if text[i] != line.first {
			i++
			continue
		}
		start := i
		for i <= last && text[i] == line.first {
			meterWork(p.ctx, 1)
			i++
		}
		markers = meteredAppend(p.ctx, markers, Marker{Role: "thematic-marker", Source: p.index.span(start, i)})
	}
	return markers
}

// Classify without acquiring semantic suffixes at every quote depth. The cursor
// retains physical tab-column phase; indent is relative to the current container.
func (p *blockParser) logicalLine(physical physicalLine, cursor Indentation) blockLine {
	meterWork(p.ctx, 1)
	physical.start = cursor.Offset()
	line := blockLine{physicalLine: physical, cursor: cursor}
	c := cursor
	text := p.index.source.content
	for c.offset < c.end {
		meterWork(p.ctx, 1)
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

func (p *blockParser) appendParagraph(line blockLine) {
	meterWork(p.ctx, 1)
	if !p.paragraphOpen() {
		checkDepth(p.ctx, p.syntaxDepth()+1)
		block := openBlock{kind: Paragraph, start: line.start, last: line}
		if line.first == '[' {
			block.reference = &referenceParagraph{}
			block.reference.append(p.index.source, line)
		}
		p.stack = meteredAppend(p.ctx, p.stack, block)
		return
	}
	paragraph := &p.stack[len(p.stack)-1]
	if paragraph.reference != nil {
		paragraph.reference.append(p.index.source, line)
	}
	// Keep interior trailing spaces and the complete original ending. Delay the
	// final line's contribution so closure can trim only paragraph-final bytes.
	paragraph.content = meteredAppend(p.ctx, paragraph.content, p.index.semanticText(paragraph.last.contentStart, paragraph.last.next).Segments...)
	paragraph.last = line
}

func (p *blockParser) closeTo(depth int) error {
	meterWork(p.ctx, 1)
	for len(p.stack) > depth {
		meterWork(p.ctx, 1)
		if p.paragraphOpen() {
			if err := p.stripReferences(); err != nil {
				return err
			}
			if p.referenceInterrupted {
				p.stack = p.stack[:1]
				return nil
			}
			if len(p.stack) <= depth {
				break
			}
		}
		block := p.stack[len(p.stack)-1]
		children, err := p.groupLists(block.children)
		if err != nil {
			return err
		}
		spec := NodeSpec{Kind: block.kind, Envelope: p.index.span(block.start, block.end), Markers: block.markers, Children: blockNodesContext(p.ctx, children)}
		if block.list != nil {
			spec.Properties.List = block.list
		}
		if block.kind == Paragraph {
			p.inlinePayload = true
			spec.Envelope = p.index.span(block.start, block.last.next)
			spec.Content = p.paragraphContent(block)
		}
		node, err := meteredNode(p.ctx, spec)
		if err != nil {
			return err
		}
		p.stack = p.stack[:len(p.stack)-1]
		closed := parsedBlock{node: node, partial: block.partial}
		if block.kind == ListItem {
			closed.loose, closed.trailingBlank = itemBlankBoundariesContext(p.ctx, block, children)
		}
		p.attachBlock(closed)
	}
	return nil
}

// The private builder owns the segment backing storage until NewNode freezes it.
// This is called once on closure/promotion, never for each continuation line.
func (p *blockParser) paragraphContent(paragraph openBlock) []Segment {
	meterWork(p.ctx, 1)
	end := paragraph.last.end
	text := p.index.source.content
	for end > paragraph.last.contentStart && (text[end-1] == ' ' || text[end-1] == '\t') {
		meterWork(p.ctx, 1)
		end--
	}
	return meteredAppend(p.ctx, paragraph.content, p.index.semanticText(paragraph.last.contentStart, end).Segments...)
}

// List grouping contributes an explicit List ancestor around every open item.
func (p *blockParser) syntaxDepth() int {
	depth := 0
	for _, b := range p.stack {
		meterWork(p.ctx, 1)
		if b.kind != DocumentKind {
			depth++
		}
		if b.kind == ListItem {
			depth++
		}
	}
	return depth
}

func (p *blockParser) containerSyntaxDepth() int {
	depth := p.syntaxDepth()
	if p.paragraphOpen() {
		depth--
	}
	return depth
}
