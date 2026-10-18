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
	second       byte
}

// A start hook runs before paragraph continuation. true defers interpretation;
// it does not recognize a block. Future providers can replace this conservative
// boundary when they implement the specification's actual precedence rules.
type blockStartHook func(blockLine, bool) bool

type openBlock struct {
	kind    Kind
	start   int
	last    blockLine
	content []Segment
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
		line := p.line(physical)
		if p.html != nil {
			if p.html.form >= 6 && line.contentStart == line.end {
				if err := p.closeHTML(); err != nil {
					return Pending, err
				}
				// The separating blank is outside the HTML block.
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
				p.appendFenceLine(physical)
			}
			continue
		}
		if p.code != nil {
			if line.contentStart == line.end {
				p.code.blanks = append(p.code.blanks, physical)
				continue
			}
			if line.indent >= 4 {
				for _, blank := range p.code.blanks {
					p.appendCodeLine(blank)
				}
				p.code.blanks = nil
				p.appendCodeLine(physical)
				continue
			}
			if err := p.closeCode(); err != nil {
				return Pending, err
			}
			// The dedented line still needs normal block dispatch below.
		}
		if line.contentStart == line.end {
			if err := p.closeTo(1); err != nil {
				return Pending, err
			}
			continue
		}
		if line.indent >= 4 && len(p.stack) == 1 {
			p.code = &indentedCode{start: line.start}
			p.appendCodeLine(physical)
			continue
		}
		if fence := p.fenceOpening(line); fence != nil {
			if err := p.closeTo(1); err != nil {
				return Pending, err
			}
			p.fence = fence
			if !fence.info.Semantic.Present {
				state = Pending // #19 supplies escape/entity info decoding.
			}
			continue
		}
		if form := p.htmlOpening(line, len(p.stack) > 1); form != 0 {
			if err := p.closeTo(1); err != nil {
				return Pending, err
			}
			p.html = &htmlBlock{start: line.start, form: form}
			if err := p.appendHTMLLine(line); err != nil {
				return Pending, err
			}
			continue
		}
		// A valid underline promotes the entire open paragraph before thematic
		// or list dispatch. Without a paragraph, ordinary block precedence wins.
		setext, err := p.setextHeading(line)
		if err != nil {
			return Pending, err
		}
		if setext != nil {
			p.closed = append(p.closed, setext)
			p.stack = p.stack[:1]
			state = Pending // Nonempty content still needs references/inlines.
			continue
		}
		markers := p.thematicMarkers(line)
		if len(markers) > 0 {
			if err := p.closeTo(1); err != nil {
				return Pending, err
			}
			node, err := NewNode(NodeSpec{Kind: ThematicBreak, Envelope: p.index.span(line.start, line.next), Markers: markers})
			if err != nil {
				return Pending, err
			}
			p.closed = append(p.closed, node)
			continue
		}
		heading, err := p.atxHeading(line)
		if err != nil {
			return Pending, err
		}
		if heading != nil {
			if err := p.closeTo(1); err != nil {
				return Pending, err
			}
			p.closed = append(p.closed, heading)
			if len(heading.spec.Content) != 0 {
				state = Pending // #18 supplies nonempty inline interpretation.
			}
			continue
		}
		// Paragraphs and deferred syntax still need later providers. Once any
		// such line occurs, subsequent recognized breaks cannot complete it.
		state = Pending
		deferred := false
		for _, hook := range p.hooks {
			if hook(line, len(p.stack) > 1) {
				deferred = true
				break
			}
		}
		if deferred {
			// The new line might start an unknown container or other block.
			// Retain only previously closed blocks and leave this suffix
			// source-only rather than invent its boundaries.
			p.stack = p.stack[:1]
			break
		}
		p.appendParagraph(line)
	}
	// EOF finalization must not turn an interrupted open builder into valid
	// unclosed content. Already closed blocks remain available as a partial prefix.
	if interruption(ctx) != "" {
		state = Interrupted
	}
	if state == Interrupted {
		p.stack = p.stack[:1]
		p.code = nil // Never publish an unfinished code block after interruption.
		p.fence = nil
		p.html = nil
	}
	if err := p.closeHTML(); err != nil {
		return Pending, err
	}
	if err := p.closeFence(); err != nil {
		return Pending, err
	}
	if err := p.closeCode(); err != nil {
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
		// Every line was recognized as blank, code, opaque HTML, a thematic break
		// or an empty ATX heading. None requires references or inline interpretation.
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
	if len(p.stack) == 1 || line.indent >= 4 || line.first != '=' && line.first != '-' {
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

func (p *blockParser) line(physical physicalLine) blockLine {
	line := blockLine{physicalLine: physical, contentStart: physical.start}
	text := p.index.source.content
	for line.contentStart < line.end {
		switch text[line.contentStart] {
		case ' ':
			line.indent = min(4, line.indent+1)
		case '\t':
			line.indent = 4
		default:
			line.first = text[line.contentStart]
			if line.contentStart+1 < line.end {
				line.second = text[line.contentStart+1]
			}
			return line
		}
		line.contentStart++
	}
	return line
}

func pendingBlockStart(line blockLine, paragraphOpen bool) bool {
	if line.indent >= 4 {
		// Indented code cannot interrupt an existing paragraph.
		return !paragraphOpen
	}
	first := line.first
	if first == '-' {
		// Failed underlines and non-list dash text can be ordinary paragraphs.
		// A potential list item still needs its provider, including an empty one.
		return line.contentStart+1 == line.end || line.second == ' ' || line.second == '\t'
	}
	// Fence and HTML candidates have already been classified. Invalid starts
	// are ordinary paragraph content, still awaiting the inline provider.
	return strings.ContainsRune(">*_+[", rune(first)) || first >= '0' && first <= '9'
}

func (p *blockParser) appendParagraph(line blockLine) {
	if len(p.stack) == 1 {
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
		paragraph := p.stack[len(p.stack)-1]
		node, err := NewNode(NodeSpec{Kind: paragraph.kind, Envelope: p.index.span(paragraph.start, paragraph.last.next), Content: p.paragraphContent(paragraph)})
		if err != nil {
			return err
		}
		p.closed = append(p.closed, node)
		p.stack = p.stack[:len(p.stack)-1]
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
