package parser

import "strings"

// The builder is private to one parse. Blank lines acquire code ownership only
// when another nonblank code line follows; trailing blanks never reach the node.
type indentedCode struct {
	start   int
	last    physicalLine
	blanks  []physicalLine
	content []Segment
	markers []Marker
}

func (p *blockParser) appendCodeLine(line physicalLine) {
	code := p.code
	cursor := Indentation{index: p.index, offset: line.start, end: line.end}
	// Nonblank code has at least four columns. Interior blanks may have fewer.
	// The fixed nonnegative request cannot fail for this source-owned cursor.
	_, _ = cursor.Consume(4)
	if cursor.Offset() > line.start {
		code.markers = append(code.markers, Marker{Role: "indent-prefix", Source: p.index.span(line.start, cursor.Offset())})
	}
	code.content = append(code.content, cursor.Remaining().Segments...)
	ending := Segment{Source: p.index.span(line.end, line.next), Transformation: Exact, Semantic: Some("\n")}
	switch p.index.source.content[line.end:line.next] {
	case "":
		ending.Transformation = Synthetic // Unterminated nonblank EOF gains LF.
	case "\n":
	default:
		ending.Transformation = Normalized
	}
	code.content = append(code.content, ending)
	code.last = line
}

func (p *blockParser) closeCode() error {
	code := p.code
	if code == nil {
		return nil
	}
	var literal strings.Builder
	for _, segment := range code.content {
		literal.WriteString(segment.Semantic.Value)
	}
	node, err := NewNode(NodeSpec{
		Kind: CodeBlock, Envelope: p.index.span(code.start, code.last.next),
		Content: code.content, Markers: code.markers,
		Properties: Properties{Code: &CodeProperties{Style: "indented"}, Literal: Some(literal.String())},
	})
	if err != nil {
		return err
	}
	p.closed = append(p.closed, node)
	p.code = nil
	return nil
}
