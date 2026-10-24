package parser

import "strings"

// One private builder owns an active fence. Payload is acquired once per line;
// only closure (including normal EOF) freezes the source-bound immutable node.
type fencedCode struct {
	start, end int
	indent     int
	character  byte
	width      int
	info       Value
	content    []Segment
	markers    []Marker
}

func (p *blockParser) fenceOpening(line blockLine) *fencedCode {
	meterWork(p.ctx, 1)
	if line.indent >= 4 || line.first != '`' && line.first != '~' {
		return nil
	}
	text := p.index.source.content
	end := line.contentStart
	for end < line.end && text[end] == line.first {
		meterWork(p.ctx, 1)
		end++
	}
	if end-line.contentStart < 3 || line.first == '`' && meteredContainsRune(p.ctx, text[end:line.end], '`') {
		return nil
	}
	startInfo, endInfo := end, line.end
	for startInfo < endInfo && (text[startInfo] == ' ' || text[startInfo] == '\t') {
		meterWork(p.ctx, 1)
		startInfo++
	}
	for endInfo > startInfo && (text[endInfo-1] == ' ' || text[endInfo-1] == '\t') {
		meterWork(p.ctx, 1)
		endInfo--
	}
	info := decodeValue(p.ctx, p.index.semanticText(startInfo, endInfo).Segments, false)
	return &fencedCode{
		start: line.start, end: line.next, indent: line.indent,
		character: line.first, width: end - line.contentStart, info: info,
		markers: []Marker{{Role: "opening", Source: p.index.span(line.contentStart, end)}},
	}
}

func (p *blockParser) fenceClosing(line blockLine) Span {
	meterWork(p.ctx, 1)
	fence := p.fence
	if line.indent >= 4 || line.first != fence.character {
		return Span{}
	}
	text := p.index.source.content
	end := line.contentStart
	for end < line.end && text[end] == fence.character {
		meterWork(p.ctx, 1)
		end++
	}
	if end-line.contentStart < fence.width {
		return Span{}
	}
	for i := end; i < line.end; i++ {
		meterWork(p.ctx, 1)
		if text[i] != ' ' && text[i] != '\t' {
			return Span{}
		}
	}
	return p.index.span(line.contentStart, end)
}

func (p *blockParser) appendFenceLine(line blockLine) {
	meterWork(p.ctx, 1)
	fence := p.fence
	cursor := line.cursor
	consumed, _ := cursor.Consume(fence.indent)
	if len(consumed.Content.Segments) > 0 {
		// A partially consumed tab belongs to both the removed prefix and the
		// residual PartialTab contribution; byte offsets never split that tab.
		segments := consumed.Content.Segments
		fence.markers = meteredAppend(p.ctx, fence.markers, Marker{Role: "indent-prefix", Source: p.index.span(line.start, segments[len(segments)-1].Source.End())})
	}
	fence.content = meteredAppend(p.ctx, fence.content, cursor.Remaining().Segments...)
	ending := Segment{Source: p.index.span(line.end, line.next), Transformation: Exact, Semantic: Some("\n")}
	switch p.index.source.content[line.end:line.next] {
	case "":
		ending.Transformation = Synthetic
	case "\n":
	default:
		ending.Transformation = Normalized
	}
	fence.content = meteredAppend(p.ctx, fence.content, ending)
	fence.end = line.next
}

func (p *blockParser) closeFence() error {
	meterWork(p.ctx, 1)
	fence := p.fence
	if fence == nil {
		return nil
	}
	var literal strings.Builder
	for _, segment := range fence.content {
		meterWork(p.ctx, 1)
		meteredWriteString(p.ctx, &literal, segment.Semantic.Value)
	}
	node, err := meteredNode(p.ctx, NodeSpec{
		Kind: CodeBlock, Envelope: p.index.span(fence.start, fence.end),
		Content: fence.content, Markers: fence.markers,
		Properties: Properties{
			Code:    &CodeProperties{Style: "fenced", Fence: Some(fence.character), Width: Some(fence.width), Info: &fence.info},
			Literal: Some(literal.String()),
		},
	})
	if err != nil {
		return err
	}
	p.attach(node)
	p.fence = nil
	return nil
}
