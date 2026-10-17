package parser

import (
	"context"
	"strings"
)

// blockLine is a borrowed view of one indexed physical line. Indent is capped
// at four: thematic breaks allow fewer than four structural columns.
type blockLine struct {
	physicalLine
	contentStart int
	indent       int
	first        byte
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
		if line.contentStart == line.end {
			if err := p.closeTo(1); err != nil {
				return Pending, err
			}
			continue
		}
		markers, underline := p.thematicMarkers(line)
		// Setext wins over a dash break after an open paragraph. Its provider
		// is still pending, so let the conservative hooks defer that suffix.
		// Otherwise thematic breaks win over list starts and continuation.
		if len(markers) > 0 && !(underline && len(p.stack) > 1) {
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
			// The new line might transform the open paragraph (e.g. Setext), or
			// start an unknown container. Retain only previously closed blocks;
			// leave this suffix source-only rather than invent its boundaries.
			p.stack = p.stack[:1]
			break
		}
		p.appendParagraph(line)
	}
	if state == Interrupted {
		p.stack = p.stack[:1]
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
		// Every physical line was established as blank or a thematic break;
		// neither can contain a definition or require inline interpretation.
		document.references = ReferenceCollection{Availability: Available, LookupAvailability: Available}
		document.inlineFree = true
	}
	return state, nil
}

// thematicMarkers validates one whole physical line before allocating metadata.
// Each maximal contiguous marker run gets its exact original-byte range; spaces,
// tabs and endings remain in the envelope, never in a marker. A single dash run
// could instead underline an open paragraph, unlike spaced dashes.
func (p *blockParser) thematicMarkers(line blockLine) (markers []Marker, underline bool) {
	if line.indent >= 4 || !strings.ContainsRune("*-_", rune(line.first)) {
		return nil, false
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
			return nil, false
		}
	}
	if count < 3 {
		return nil, false
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
	return markers, line.first == '-' && len(markers) == 1
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
	return strings.ContainsRune("#>*_~`-+=[<", rune(first)) || first >= '0' && first <= '9'
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
		end := paragraph.last.end
		text := p.index.source.content
		for end > paragraph.last.contentStart && (text[end-1] == ' ' || text[end-1] == '\t') {
			end--
		}
		paragraph.content = append(paragraph.content, p.index.semanticText(paragraph.last.contentStart, end).Segments...)
		node, err := NewNode(NodeSpec{Kind: paragraph.kind, Envelope: p.index.span(paragraph.start, paragraph.last.next), Content: paragraph.content})
		if err != nil {
			return err
		}
		p.closed = append(p.closed, node)
		p.stack = p.stack[:len(p.stack)-1]
	}
	return nil
}
