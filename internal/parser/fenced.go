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
	if line.indent >= 4 || line.first != '`' && line.first != '~' {
		return nil
	}
	text := p.index.source.content
	end := line.contentStart
	for end < line.end && text[end] == line.first {
		end++
	}
	if end-line.contentStart < 3 || line.first == '`' && strings.ContainsRune(text[end:line.end], '`') {
		return nil
	}
	startInfo, endInfo := end, line.end
	for startInfo < endInfo && (text[startInfo] == ' ' || text[startInfo] == '\t') {
		startInfo++
	}
	for endInfo > startInfo && (text[endInfo-1] == ' ' || text[endInfo-1] == '\t') {
		endInfo--
	}
	info := p.index.semanticText(startInfo, endInfo)
	if fenceInfoNeedsDecoder(text[startInfo:endInfo]) {
		// Keep raw spelling, explicitly withholding interpretation until #19.
		info = Value{Segments: []Segment{{Source: p.index.span(startInfo, endInfo), Transformation: MappingUnavailable}}}
	}
	return &fencedCode{
		start: line.start, end: line.next, indent: line.indent,
		character: line.first, width: end - line.contentStart, info: info,
		markers: []Marker{{Role: "opening", Source: p.index.span(line.contentStart, end)}},
	}
}

func (p *blockParser) fenceClosing(line blockLine) Span {
	fence := p.fence
	if line.indent >= 4 || line.first != fence.character {
		return Span{}
	}
	text := p.index.source.content
	end := line.contentStart
	for end < line.end && text[end] == fence.character {
		end++
	}
	if end-line.contentStart < fence.width {
		return Span{}
	}
	for i := end; i < line.end; i++ {
		if text[i] != ' ' && text[i] != '\t' {
			return Span{}
		}
	}
	return p.index.span(line.contentStart, end)
}

func (p *blockParser) appendFenceLine(line physicalLine) {
	fence := p.fence
	cursor := Indentation{index: p.index, offset: line.start, end: line.end}
	consumed, _ := cursor.Consume(fence.indent)
	if len(consumed.Content.Segments) > 0 {
		// A partially consumed tab belongs to both the removed prefix and the
		// residual PartialTab contribution; byte offsets never split that tab.
		segments := consumed.Content.Segments
		fence.markers = append(fence.markers, Marker{Role: "indent-prefix", Source: p.index.span(line.start, segments[len(segments)-1].Source.End())})
	}
	fence.content = append(fence.content, cursor.Remaining().Segments...)
	ending := Segment{Source: p.index.span(line.end, line.next), Transformation: Exact, Semantic: Some("\n")}
	switch p.index.source.content[line.end:line.next] {
	case "":
		ending.Transformation = Synthetic
	case "\n":
	default:
		ending.Transformation = Normalized
	}
	fence.content = append(fence.content, ending)
	fence.end = line.next
}

func (p *blockParser) closeFence() error {
	fence := p.fence
	if fence == nil {
		return nil
	}
	var literal strings.Builder
	for _, segment := range fence.content {
		literal.WriteString(segment.Semantic.Value)
	}
	node, err := NewNode(NodeSpec{
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
	p.closed = append(p.closed, node)
	p.fence = nil
	return nil
}

// This is a conservative availability guard, not the #19 decoder. Literal
// backslashes and ampersands remain known; punctuation escapes and syntactically
// plausible semicolon-terminated references await the named/numeric provider.
func fenceInfoNeedsDecoder(info string) bool {
	for i := 0; i < len(info); i++ {
		if info[i] == '\\' && i+1 < len(info) && IsASCIIPunctuation(rune(info[i+1])) {
			return true
		}
		if info[i] != '&' {
			continue
		}
		start := i + 1
		if start < len(info) && info[start] == '#' {
			start++
			limit, hex := 7, false
			if start < len(info) && (info[start] == 'x' || info[start] == 'X') {
				start++
				limit, hex = 6, true
			}
			end := start
			for end < len(info) && end-start < limit && (info[end] >= '0' && info[end] <= '9' || hex && (info[end] >= 'a' && info[end] <= 'f' || info[end] >= 'A' && info[end] <= 'F')) {
				end++
			}
			if end > start && end < len(info) && info[end] == ';' {
				return true
			}
		} else if start < len(info) && (info[start] >= 'a' && info[start] <= 'z' || info[start] >= 'A' && info[start] <= 'Z') {
			end := start + 1
			for end < len(info) && end-start < 32 && (info[end] >= 'a' && info[end] <= 'z' || info[end] >= 'A' && info[end] <= 'Z' || info[end] >= '0' && info[end] <= '9') {
				end++
			}
			if end-start >= 2 && end < len(info) && info[end] == ';' {
				return true
			}
		}
	}
	return false
}
