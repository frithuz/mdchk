package parser

import (
	"context"
	"sort"
	"strings"
)

// Outer quote extraction uses the context captured before recursively parsing
// its payload. Inner items/leaves cannot decide whether the quote owns a line.
func (p *blockParser) pandocQuoteContinuation(depth int, line blockLine) bool {
	if line.contentStart == line.end {
		return false
	}
	c := line.cursor
	if c.tabRemaining == 0 && c.offset < c.end && p.index.source.content[c.offset] == '`' && p.fenceOpening(line) != nil {
		return false
	}
	if p.stack[depth].quoteInList {
		if _, ok := p.pandocListStart(line, nil, false); ok {
			return false
		}
		if _, ok := p.pandocListStart(line, nil, true); ok {
			return false
		}
	}
	return true
}

// Probe the next original reader line without opening a quote or ordered list.
// Literal bullet/fence starts are tried before headers by the pinned reader.
func (p *blockParser) pandocSetextAhead(line blockLine) bool {
	if line.contentStart == line.end || p.fenceOpening(line) != nil {
		return false
	}
	if start, ok := p.pandocListStart(line, nil, false); ok && start.pandoc.Style == "bullet" {
		return false
	}
	i := sort.Search(len(p.index.lines), func(i int) bool { meterWork(p.ctx, 1); return p.index.lines[i].start >= line.next })
	if i == len(p.index.lines) {
		return false
	}
	physical := p.index.lines[i]
	c, ok := p.pandocReplayCursor(physical, p.pandocReplayStates())
	if !ok {
		return false
	}
	_, level := p.setextUnderline(p.logicalLine(physical, c))
	return level != 0
}

// Lookahead borrows only extraction-state values. It reuses the exact original
// cursor decisions without mutating builders or cloning growing child prefixes.
func (p *blockParser) pandocReplayStates() []pandocItemState {
	states := meteredSlice[pandocItemState](p.ctx, p.containerDepth(), p.containerDepth())
	for j := 1; j < len(states); j++ {
		meterWork(p.ctx, 1)
		states[j] = p.stack[j].pandocItem
	}
	return states
}
func (p *blockParser) pandocReplayCursor(physical physicalLine, states []pandocItemState) (Indentation, bool) {
	c := Indentation{index: p.index, offset: physical.start, end: physical.end}
	for j := 1; j < len(states); j++ {
		meterWork(p.ctx, 1)
		b := p.stack[j]
		if b.kind == BlockQuote {
			next, _, ok := p.quotePrefix(c)
			if !ok {
				if !p.pandocQuoteContinuation(j, p.logicalLine(physical, c)) {
					return c, false
				}
				next = c
				next.column = b.quoteColumn
			}
			c = next
		} else {
			match := p.pandocItemLine(p.logicalLine(physical, c), b.list.ContentIndent.Value, states[j])
			if !match.ok {
				return c, false
			}
			states[j] = match.state
			c = match.cursor
		}
	}
	return c, true
}

// definitionLookahead borrows original physical lines and replays only existing
// container prefixes. It does not mutate marker inventories or parser state.
func (p *blockParser) definitionLookahead(line blockLine) bool {
	states := p.pandocReplayStates()
	i := sort.Search(len(p.index.lines), func(i int) bool { meterWork(p.ctx, 1); return p.index.lines[i].start >= line.next })
	for blanks := 0; i < len(p.index.lines) && blanks <= 1; i++ {
		physical := p.index.lines[i]
		c, ok := p.pandocReplayCursor(physical, states)
		if !ok {
			return false
		}
		next := p.logicalLine(physical, c)
		if next.contentStart == next.end {
			blanks++
			continue
		}
		_, ok = p.pandocListStart(next, nil, true)
		return ok
	}
	return false
}
func (p *blockParser) pandocDefinitionBoundary(line blockLine) bool {
	if !p.pandoc() {
		return false
	}
	// A colon line may replace only a single-line open term. Multi-line prose
	// remains prose. Normal dispatch handles block-start terms by lookahead.
	if p.paragraphOpen() {
		b := p.stack[len(p.stack)-1]
		if len(b.content) != 0 {
			return false
		}
		_, ok := p.pandocListStart(line, nil, true)
		return ok
	}
	return false
}
func (p *blockParser) lastPandocBlock() *Node {
	children := p.closed
	if len(p.stack) > 1 {
		children = p.stack[len(p.stack)-1].children
	}
	if len(children) == 0 {
		return nil
	}
	return children[len(children)-1].node
}

func (p *blockParser) dispatchPandocSpecial(line blockLine) (bool, *Indentation, error) {
	if p.paragraphOpen() && p.pandocDefinitionBoundary(line) {
		b := p.stack[len(p.stack)-1]
		term, err := meteredNode(p.ctx, NodeSpec{Kind: DefinitionTerm, Envelope: p.index.span(b.start, b.last.next), Content: p.paragraphContent(b)})
		if err != nil {
			return false, nil, err
		}
		p.stack = p.stack[:len(p.stack)-1]
		p.attach(term)
	}
	if !p.paragraphOpen() {
		if previous := p.lastPandocBlock(); previous != nil && (previous.Kind() == DefinitionTerm || previous.Kind() == DefinitionBody) {
			if start, ok := p.pandocActualListStart(line, nil, true); ok {
				item, err := start.openItem()
				if err != nil {
					return false, nil, err
				}
				item.kind = DefinitionBody
				checkDepth(p.ctx, p.syntaxDepth()+3)
				p.stack = meteredAppend(p.ctx, p.stack, item)
				payload := start.payloadCursor()
				return true, &payload, nil
			}
		}
		if p.pandocLineStart(line) {
			checkDepth(p.ctx, p.containerSyntaxDepth()+2)
			p.lineBlock = &pandocLineBlock{start: line.start}
			_, err := p.continuePandocLineBlock(line)
			return true, nil, err
		}
		if p.definitionLookahead(line) {
			checkDepth(p.ctx, p.containerSyntaxDepth()+3)
			term, err := meteredNode(p.ctx, NodeSpec{Kind: DefinitionTerm, Envelope: p.index.span(line.start, line.next), Content: p.index.semanticText(line.contentStart, line.end).Segments})
			if err != nil {
				return false, nil, err
			}
			p.attach(term)
			return true, nil, nil
		}

	}
	return false, nil, nil
}

func (p *blockParser) groupPandocDefinitions(children []parsedBlock) ([]parsedBlock, error) {
	out := children[:0]
	for i := 0; i < len(children); {
		meterWork(p.ctx, 1)
		if children[i].node.Kind() != DefinitionTerm {
			out = meteredAppend(p.ctx, out, children[i])
			i++
			continue
		}
		end := i + 1
		for end < len(children) && children[end].node.Kind() == DefinitionBody {
			meterWork(p.ctx, 1)
			end++
		}
		if end == i+1 { // An interrupted body cannot turn a detached term into a list.
			term := children[i].node
			spec := term.spec
			spec.Kind = Paragraph
			n, err := meteredNode(p.ctx, spec)
			if err != nil {
				return nil, err
			}
			out = meteredAppend(p.ctx, out, parsedBlock{node: n, partial: true})
			i++
			continue
		}
		items := meteredSlice[*Node](p.ctx, 0, 1)
		for {
			term := children[i].node
			last := children[end-1].node
			item, err := meteredNode(p.ctx, NodeSpec{Kind: DefinitionItem, Envelope: p.index.span(term.Span().Start(), last.Span().End()), Children: blockNodesContext(p.ctx, children[i:end]), Properties: Properties{Definition: &DefinitionProperties{Tight: p.definitionTight(term.Span(), children[i+1].node.Span())}}})
			if err != nil {
				return nil, err
			}
			items = meteredAppend(p.ctx, items, item)
			i = end
			if i >= len(children) || children[i].node.Kind() != DefinitionTerm {
				break
			}
			end = i + 1
			for end < len(children) && children[end].node.Kind() == DefinitionBody {
				meterWork(p.ctx, 1)
				end++
			}
			if end == i+1 {
				break
			}
		}
		list, err := meteredNode(p.ctx, NodeSpec{Kind: DefinitionList, Envelope: p.index.span(items[0].Span().Start(), items[len(items)-1].Span().End()), Children: items})
		if err != nil {
			return nil, err
		}
		out = meteredAppend(p.ctx, out, parsedBlock{node: list})
	}
	return out, nil
}

type pandocLineBlock struct {
	start, end int
	lines      []*Node
	current    *NodeSpec
	last       blockLine
}

func (p *blockParser) pandocLineStart(line blockLine) bool {
	if line.cursor.tabRemaining != 0 || line.cursor.offset != line.contentStart || line.first != '|' {
		return false
	}
	raw := p.index.source.content
	if line.contentStart+1 == line.end || raw[line.contentStart+1] == ' ' {
		return true
	}
	meterWork(p.ctx, line.end-line.contentStart-1)
	return strings.Trim(raw[line.contentStart+1:line.end], " \t") == ""
}
func (p *blockParser) freezePandocLine() error {
	b := p.lineBlock
	if b.current == nil {
		return nil
	}
	n, err := meteredNode(p.ctx, *b.current)
	if err != nil {
		return err
	}
	b.lines = meteredAppend(p.ctx, b.lines, n)
	b.current = nil
	return nil
}
func (p *blockParser) continuePandocLineBlock(line blockLine) (bool, error) {
	b := p.lineBlock
	raw := p.index.source.content
	if p.pandocLineStart(line) {
		if err := p.freezePandocLine(); err != nil {
			return false, err
		}
		start := line.contentStart + 1
		markers := []Marker{{Role: "line-prefix", Source: p.index.span(line.contentStart, start)}}
		if start < line.end && raw[start] == ' ' {
			start++
			markers[0].Source = p.index.span(line.contentStart, start)
		}
		lead := start
		for start < line.end && (raw[start] == ' ' || raw[start] == '\t') {
			meterWork(p.ctx, 1)
			start++
		}
		indentation := Value{Segments: p.index.semanticText(lead, start).Segments, Semantic: Some("")}
		blank := start == line.end
		if start > lead {
			var v strings.Builder
			for i := lead; i < start; i++ {
				meterWork(p.ctx, 1)
				if !blank {
					meterWork(p.ctx, 2) // reserve the UTF-8 projection before writing it
					v.WriteRune('\u00a0')
				}
			}
			indentation.Semantic = Some(v.String())
			for i := range indentation.Segments {
				indentation.Segments[i].Transformation = Normalized
				indentation.Segments[i].Semantic = Optional[string]{}
			}
		}
		b.current = &NodeSpec{Kind: LineBlockLine, Envelope: p.index.span(line.start, line.next), Markers: markers, Content: p.index.semanticText(start, line.end).Segments, Properties: Properties{PandocLine: &PandocLineProperties{Indentation: indentation, Blank: blank}}}
		b.last = line
		b.end = line.next
		return true, nil
	}
	// A logical ASCII space continues a wrapped line. Residual spaces from a
	// tab expanded by an enclosing item qualify; a fresh raw tab does not.
	if b.current != nil && !b.current.Properties.PandocLine.Blank && line.cursor.offset < line.end && (line.cursor.tabRemaining > 0 || raw[line.cursor.offset] == ' ') {
		old := b.last
		if old.next > old.end {
			b.current.Content = meteredAppend(p.ctx, b.current.Content, Segment{Source: p.index.span(old.end, old.next), Transformation: Normalized, Semantic: Some(" ")})
		}
		continuation := line.cursor
		removed, _ := continuation.Consume(1)
		marker := removed.Content.Segments[0].Source
		b.current.Markers = meteredAppend(p.ctx, b.current.Markers, Marker{Role: "line-continuation", Source: marker})
		b.current.Content = meteredAppend(p.ctx, b.current.Content, continuation.Remaining().Segments...)
		b.current.Envelope = p.index.span(b.current.Envelope.Start(), line.next)
		b.last = line
		b.end = line.next
		return true, nil
	}
	return false, nil
}
func (p *blockParser) closePandocLineBlock() error {
	if p.lineBlock == nil {
		return nil
	}
	if err := p.freezePandocLine(); err != nil {
		return err
	}
	b := p.lineBlock
	n, err := meteredNode(p.ctx, NodeSpec{Kind: LineBlock, Envelope: p.index.span(b.start, b.end), Children: b.lines})
	if err != nil {
		return err
	}
	p.lineBlock = nil
	p.attach(n)
	return nil
}

func (p *blockParser) recognizePandocTask(spec *NodeSpec) {
	recognizePandocTask(p.ctx, p.index, spec)
}

func recognizePandocTask(ctx context.Context, index LineIndex, spec *NodeSpec) {
	// Retain literal task candidates without removing their raw inline payload.
	// The later inline provider resolves reference/entity/escape-dependent forms.
	partIndex, offset := 0, 0
	next := func() Span {
		for partIndex < len(spec.Content) {
			part := spec.Content[partIndex].Source
			if !part.Present() {
				return Span{}
			}
			if offset == 0 {
				offset = part.start
			}
			if offset < part.end {
				meterWork(ctx, 1)
				start := offset
				offset++
				if index.source.content[start] == '\r' && offset < part.end && index.source.content[offset] == '\n' {
					offset++
				}
				return index.span(start, offset)
			}
			partIndex++
			offset = 0
		}
		return Span{}
	}
	raw := func(s Span) string {
		if !s.Present() {
			return ""
		}
		return index.source.content[s.start:s.end]
	}
	opening := next()
	if raw(opening) != "[" {
		return
	}
	check := next()
	checked := raw(check) == "x" || raw(check) == "X"
	checks := meteredSlice[Span](ctx, 0, 1)
	if checked {
		checks = meteredAppend(ctx, checks, check)
	} else {
		for taskToken(check) {
			if len(checks) > 0 && checks[len(checks)-1].end == check.start {
				checks[len(checks)-1].end = check.end
			} else {
				checks = meteredAppend(ctx, checks, check)
			}
			check = next()
		}
		if len(checks) == 0 {
			return
		}
	}
	closing := check
	if checked {
		closing = next()
	}
	if raw(closing) != "]" {
		return
	}
	separator := next()
	if separator.Present() && !taskToken(separator) {
		return
	}
	marker := index.span(opening.start, closing.end)
	checkSpan := index.span(checks[0].start, checks[len(checks)-1].end)
	segments := meteredSlice[Span](ctx, 0, len(checks)+2)
	appendPart := func(part Span) {
		if len(segments) > 0 && segments[len(segments)-1].end == part.start {
			segments[len(segments)-1].end = part.end
		} else {
			segments = meteredAppend(ctx, segments, part)
		}
	}
	appendPart(opening)
	for _, part := range checks {
		appendPart(part)
	}
	appendPart(closing)
	spec.Properties.PandocTask = &PandocTaskProperties{Marker: marker, Check: checkSpan, Separator: separator, Segments: segments, CheckSegments: checks, Checked: checked, CheckSpelling: raw(checkSpan), InlinePending: true}
}

func (p *blockParser) definitionTight(term, body Span) bool {
	meterWork(p.ctx, body.Start()-term.End())
	return !strings.ContainsAny(p.index.source.content[term.End():body.Start()], "\r\n")
}
