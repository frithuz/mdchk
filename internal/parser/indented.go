package parser

import "strings"

// The builder is private to one parse. Blank lines acquire code ownership only
// when another nonblank code line follows; trailing blanks never reach the node.
type indentedCode struct {
	start   int
	last    blockLine
	blanks  []blockLine
	content []Segment
	markers []Marker
}

func (p *blockParser) appendCodeLine(line blockLine) {
	meterWork(p.ctx, 1)
	code := p.code
	cursor := line.cursor
	// Nonblank code has at least four columns. Interior blanks may have fewer.
	// The fixed nonnegative request cannot fail for this source-owned cursor.
	consumed, _ := cursor.Consume(4)
	if len(consumed.Content.Segments) > 0 {
		code.markers = meteredAppend(p.ctx, code.markers, Marker{Role: "indent-prefix", Source: p.index.span(line.start, consumed.Content.Segments[len(consumed.Content.Segments)-1].Source.End())})
	}
	code.content = meteredAppend(p.ctx, code.content, cursor.Remaining().Segments...)
	ending := Segment{Source: p.index.span(line.end, line.next), Transformation: Exact, Semantic: Some("\n")}
	switch p.index.source.content[line.end:line.next] {
	case "":
		ending.Transformation = Synthetic // Unterminated nonblank EOF gains LF.
	case "\n":
	default:
		ending.Transformation = Normalized
	}
	code.content = meteredAppend(p.ctx, code.content, ending)
	code.last = line
}

func (p *blockParser) closeCode() error {
	meterWork(p.ctx, 1)
	code := p.code
	if code == nil {
		return nil
	}
	var literal strings.Builder
	for _, segment := range code.content {
		meterWork(p.ctx, 1)
		meteredWriteString(p.ctx, &literal, segment.Semantic.Value)
	}
	node, err := meteredNode(p.ctx, NodeSpec{
		Kind: CodeBlock, Envelope: p.index.span(code.start, code.last.next),
		Content: code.content, Markers: code.markers,
		Properties: Properties{Code: &CodeProperties{Style: "indented"}, Literal: Some(literal.String())},
	})
	if err != nil {
		return err
	}
	p.attach(node)
	p.code = nil
	return nil
}
