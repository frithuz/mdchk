package parser

// Containers and the current paragraph are private builders over the original
// snapshot. Leaves are frozen once, then attached to their actual parent.
func (p *blockParser) paragraphOpen() bool {
	meterWork(p.ctx, 1)
	return p.stack[len(p.stack)-1].kind == Paragraph
}

func (p *blockParser) containerDepth() int {
	meterWork(p.ctx, 1)
	if p.paragraphOpen() {
		return len(p.stack) - 1
	}
	return len(p.stack)
}

func (p *blockParser) attach(node *Node) {
	meterWork(p.ctx, 1)
	p.attachBlock(parsedBlock{node: node})
}

func (p *blockParser) attachBlock(block parsedBlock) {
	meterWork(p.ctx, 1)
	if p.referenceInterrupted {
		return
	}
	if len(p.stack) == 1 {
		p.closed = meteredAppend(p.ctx, p.closed, block)
		return
	}
	parent := &p.stack[len(p.stack)-1]
	parent.children = meteredAppend(p.ctx, parent.children, block)
}

func (p *blockParser) closeLeaves() error {
	meterWork(p.ctx, 1)
	if err := p.closePandocLineBlock(); err != nil {
		return err
	}
	if err := p.closeHTML(); err != nil {
		return err
	}
	if err := p.closeFence(); err != nil {
		return err
	}
	if err := p.closeTable(); err != nil {
		return err
	}
	return p.closeCode()
}

// Prefix spans include indentation and the optional delimiter column. A tab
// partially consumed by that delimiter is shared with the residual content's
// PartialTab span; no original byte is split or synthesized.
func (p *blockParser) quotePrefix(cursor Indentation) (Indentation, Span, bool) {
	meterWork(p.ctx, 1)
	c := cursor
	_, _ = c.Consume(3)
	if c.tabRemaining != 0 || c.offset == c.end || p.index.source.content[c.offset] != '>' {
		return cursor, Span{}, false
	}
	_, _ = c.AdvanceMarker()
	optionalDelimiter := true
	if p.pandoc() {
		optionalDelimiter = c.tabRemaining > 0 || c.offset < c.end && c.index.source.content[c.offset] == ' '
	}
	var delimiter IndentationConsumption
	if optionalDelimiter {
		delimiter, _ = c.Consume(1)
	}
	end := c.offset
	if len(delimiter.Content.Segments) != 0 {
		end = delimiter.Content.Segments[len(delimiter.Content.Segments)-1].Source.End()
	}
	if p.pandoc() {
		c.column = cursor.column
	}
	return c, p.index.span(cursor.Offset(), end), true
}

func (p *blockParser) interruptsParagraph(line blockLine) bool {
	meterWork(p.ctx, 1)
	if line.indent >= 4 {
		return false
	}
	if p.pandoc() {
		if p.paragraphOpen() && len(p.stack[len(p.stack)-1].content) == 0 {
			if _, level := p.setextUnderline(line); level != 0 {
				return true
			}
		}
		if line.cursor.tabRemaining == 0 && line.cursor.offset < line.end && p.index.source.content[line.cursor.offset] == '`' && p.fenceOpening(line) != nil {
			return true
		}
		if p.inPandocItem() {
			_, ok := p.pandocListStart(line, nil, false)
			if ok {
				return true
			}
			_, ok = p.pandocListStart(line, nil, true)
			return ok
		}
		return false
	}
	if _, _, ok := p.quotePrefix(line.cursor); ok {
		return true
	}
	if len(p.thematicMarkers(line)) != 0 || p.fenceOpening(line) != nil || p.htmlOpening(line, true) != 0 {
		return true
	}
	heading, _ := p.atxHeading(line)
	_, list := p.classifyListStart(line, true)
	return heading != nil || list
}

// Append raw logical lines while coalescing adjacent Exact original ranges. This
// keeps unchanged top-level HTML contiguous without copying/rescanning its prefix
// on every line, and leaves stripped quote prefixes out of logical content.
func (p *blockParser) appendRawLine(content []Segment, line blockLine) []Segment {
	meterWork(p.ctx, 1)
	cursor := line.cursor
	cursor.end = line.next
	for _, segment := range cursor.Remaining().Segments {
		meterWork(p.ctx, 1)
		if len(content) != 0 {
			last := &content[len(content)-1]
			if last.Transformation == Exact && segment.Transformation == Exact && last.Source.End() == segment.Source.Start() {
				last.Source = p.index.span(last.Source.Start(), segment.Source.End())
				last.Semantic = Some(p.index.source.content[last.Source.Start():last.Source.End()])
				continue
			}
		}
		content = meteredAppend(p.ctx, content, segment)
	}
	return content
}
