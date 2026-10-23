package parser

import (
	"context"
	"sort"
	"strings"
	"unicode"
)

// Full recognition borrows the same physical index and extraction state. Failed
// probes publish nothing and retain all work charges. Recursive extraction
// probes strictly decrease their enclosing container depth.
func (p *blockParser) pandocFenceOpening(line blockLine, f *fencedCode, depth int) *fencedCode {
	start := f.markers[0].Source.end
	parts, last := p.pandocOpenerParts(line, start, depth)
	f.end = last.next
	f.pandoc, f.attributes = scanPandocFenceInfo(p.ctx, parts)
	if f.pandoc == nil {
		return nil
	}
	// Only a real closing delimiter establishes a Pandoc code/raw block. Use
	// copied outer extraction states and never mutate the live list/quote state.
	states := p.pandocReplayStates()[:depth]
	next := sort.Search(len(p.index.lines), func(i int) bool { meterWork(p.ctx, 1); return p.index.lines[i].start >= last.next })
	for ; next < len(p.index.lines); next++ {
		meterWork(p.ctx, 1)
		physical := p.index.lines[next]
		cursor, ok := p.pandocReplayCursor(physical, states)
		if !ok {
			return nil
		}
		candidate := p.logicalLine(physical, cursor)
		if p.fenceClosingFor(candidate, f).Present() {
			return f
		}
	}
	return nil
}
func (p *blockParser) activeDiv() int { return p.divInFrame(len(p.stack)) }
func (p *blockParser) divInFrame(depth int) int {
	for i := depth - 1; i > 0; i-- {
		meterWork(p.ctx, 1)
		kind := p.stack[i].kind
		if kind == Div {
			return i
		}
		if kind == BlockQuote || kind == ListItem || kind == DefinitionBody {
			return 0
		}
	}
	return 0
}
func (p *blockParser) divCloser(line blockLine) Span {
	if line.cursor.tabRemaining != 0 || line.cursor.offset != line.contentStart || line.first != ':' {
		return Span{}
	}
	raw := p.index.source.content
	end := line.contentStart
	for end < line.end && raw[end] == ':' {
		meterWork(p.ctx, 1)
		end++
	}
	if end-line.contentStart < 3 {
		return Span{}
	}
	for i := end; i < line.end; i++ {
		meterWork(p.ctx, 1)
		if raw[i] != ' ' && raw[i] != '\t' {
			return Span{}
		}
	}
	return p.index.span(line.contentStart, end)
}
func (p *blockParser) divOpening(line blockLine) (*Attributes, []Marker) {
	if line.cursor.tabRemaining != 0 || line.cursor.offset != line.contentStart || line.first != ':' {
		return nil, nil
	}
	raw := p.index.source.content
	end := line.contentStart
	for end < line.end && raw[end] == ':' {
		meterWork(p.ctx, 1)
		end++
	}
	if end-line.contentStart < 3 {
		return nil, nil
	}
	parts, last := p.pandocOpenerParts(line, end, p.containerDepth())
	a := newAttributeInput(p.ctx, parts)
	p.openerEnd = last.next
	i := 0
	t := a.text
	for i < len(t) && (t[i] == ' ' || t[i] == '\t') {
		meterWork(p.ctx, 1)
		i++
	}
	if i == len(t) {
		return nil, nil
	}
	attrs, finish := a.scan(i)
	if attrs == nil {
		a = newAttributeInput(p.ctx, p.index.semanticText(end, line.end).Segments)
		t = a.text
		p.openerEnd = line.next
		begin := i
		for i < len(t) && t[i] != ' ' && t[i] != '\t' && t[i] != '\n' && t[i] != '\r' {
			meterWork(p.ctx, 1)
			i++
		}
		if i == begin {
			return nil, nil
		}
		value := a.value(begin, i, t[begin:i])
		meterRecords(p.ctx, 2)
		attrs = &Attributes{BareClass: true, Raw: value, Classes: []string{t[begin:i]}, Occurrences: []AttributeOccurrence{{Kind: "class", Raw: value, Value: value}}}
		finish = i
	}
	i = finish
	for i < len(t) && (t[i] == ' ' || t[i] == '\t') {
		meterWork(p.ctx, 1)
		i++
	}
	for i < len(t) && t[i] == ':' {
		meterWork(p.ctx, 1)
		i++
	}
	for i < len(t) && (t[i] == ' ' || t[i] == '\t') {
		meterWork(p.ctx, 1)
		i++
	}
	if i != len(t) {
		return nil, nil
	}
	return attrs, []Marker{{Role: "opening", Source: p.index.span(line.contentStart, end)}}
}

// Collect a multiline attribute candidate through existing recursive cursors.
// This only extends a physically unfinished group; malformed closed groups do
// not swallow later blocks. Work, copied parts and scan retries stay metered.
func (p *blockParser) pandocOpenerParts(line blockLine, start, depth int) ([]Segment, physicalLine) {
	parts := attributeParts(p.ctx, p.index.semanticText(start, line.end).Segments)
	var text strings.Builder
	for _, part := range parts {
		meteredWriteString(p.ctx, &text, part.Semantic.Value)
	}
	meterWork(p.ctx, text.Len())
	brace := strings.IndexByte(text.String(), '{')
	last := line.physicalLine
	if brace < 0 {
		return parts, last
	}
	completion := attributeCompletion{}
	a := attributeInput{ctx: p.ctx, text: text.String()}
	if completion.resume(a, brace, false) {
		return parts, last
	}
	// Raw formats have a separate single-line grammar. Only try that branch
	// when its '=' discriminator exists; do not reconstruct ordinary attributes.
	rawStart := brace + 1
	for rawStart < len(a.text) && (a.text[rawStart] == ' ' || a.text[rawStart] == '\t') {
		meterWork(p.ctx, 1)
		rawStart++
	}
	if rawStart < len(a.text) && a.text[rawStart] == '=' {
		return parts, last
	}
	states := p.pandocReplayStates()[:depth]
	next := sort.Search(len(p.index.lines), func(i int) bool { meterWork(p.ctx, 1); return p.index.lines[i].start >= line.next })
	for ; next < len(p.index.lines); next++ {
		physical := p.index.lines[next]
		cursor, ok := p.pandocReplayCursor(physical, states)
		if !ok {
			break
		}
		candidate := p.logicalLine(physical, cursor)
		if candidate.contentStart == candidate.end {
			break
		}
		appendParts := func(fragment []Segment) {
			normalized := attributeParts(p.ctx, fragment)
			for _, part := range normalized {
				meteredWriteString(p.ctx, &text, part.Semantic.Value)
			}
			parts = meteredAppend(p.ctx, parts, normalized...)
		}
		appendParts(p.index.semanticText(last.end, last.next).Segments)
		appendParts(cursor.Remaining().Segments)
		last = physical
		a.text = text.String()
		if completion.resume(a, brace, false) {
			break
		}
	}
	if completion.resume(a, brace, true) {
		// A quoted alternative may fail only at the physical input boundary, then
		// its unquoted fallback can close on an earlier line. Retain that opener
		// line alone; later collected source remains ordinary payload.
		offset := 0
		sourceEnd := 0
		for _, part := range parts {
			meterWork(p.ctx, 1)
			n := len(part.Semantic.Value)
			if completion.end <= offset+n {
				sourceEnd = part.Source.end
				if part.Transformation == Exact {
					sourceEnd = part.Source.start + completion.end - offset
				}
				break
			}
			offset += n
		}
		i := sort.Search(len(p.index.lines), func(i int) bool { meterWork(p.ctx, 1); return p.index.lines[i].end >= sourceEnd })
		if i < len(p.index.lines) {
			last = p.index.lines[i]
			end := 0
			for end < len(parts) && parts[end].Source.end <= last.end {
				meterWork(p.ctx, 1)
				end++
			}
			parts = parts[:end]
		}
	}

	return parts, last
}

func scanPandocFenceInfo(ctx context.Context, parts []Segment) (*PandocFenceProperties, *Attributes) {
	var attrs *Attributes
	a := newAttributeInput(ctx, parts)
	t := a.text
	i := 0
	spaces := func() {
		for i < len(t) && (t[i] == ' ' || t[i] == '\t') {
			meterWork(ctx, 1)
			i++
		}
	}
	spaces()
	meterRecords(ctx, 1)
	facts := &PandocFenceProperties{Info: a.value(0, len(t), t)}
	if i < len(t) && t[i] == '{' {
		j := i + 1
		for j < len(t) && (t[j] == ' ' || t[j] == '\t') {
			meterWork(ctx, 1)
			j++
		}
		if j < len(t) && t[j] == '=' {
			j++
			begin := j
			for j < len(t) {
				r, n := a.runeAt(j)
				if !attributeAlphaNum(r) && r != '-' && r != '_' {
					break
				}
				j += n
			}
			if j == begin {
				return nil, nil
			}
			format := a.value(begin, j, t[begin:j])
			for j < len(t) && (t[j] == ' ' || t[j] == '\t') {
				meterWork(ctx, 1)
				j++
			}
			if j == len(t) || t[j] != '}' {
				return nil, nil
			}
			i = j + 1
			facts.RawFormat = &format
		}
	}
	if facts.RawFormat == nil {
		begin := i
		for i < len(t) {
			r, n := a.runeAt(i)
			if r == '`' || r == '{' || r == '}' || unicode.IsSpace(r) {
				break
			}
			i += n
		}
		if i > begin {
			language := t[begin:i]
			switch language {
			case "c++":
				language = "cpp"
			case "objective-c":
				language = "objectivec"
			}
			meterWork(ctx, len(language))
			facts.Language = Some(strings.ToLower(language))
			facts.Classes = meteredAppend(ctx, facts.Classes, facts.Language.Value)
		}
		spaces()
		if i < len(t) && t[i] == '{' {
			var end int
			attrs, end = a.scan(i)
			if attrs == nil {
				return nil, nil
			}
			i = end
		}
		if attrs != nil {
			facts.Classes = meteredAppend(ctx, facts.Classes, attrs.Classes...)
		}
		if !facts.Language.Present && attrs != nil && len(attrs.Classes) > 0 {
			facts.Language = Some(attrs.Classes[0])
		}
	}
	spaces()
	if i != len(t) {
		return nil, nil
	}
	return facts, attrs
}
