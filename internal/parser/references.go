package parser

import (
	"context"
	"sort"
	"strings"
	"unicode/utf8"
)

// normalizeReferenceLabel matches raw label spelling, without its brackets.
// Shared by definition collection and full/collapsed/shortcut use resolution.
// Unicode default full folding is independent of locale and semantic decoding.
func normalizeReferenceLabel(raw string) string {
	var out strings.Builder
	space := false
	for _, r := range raw {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			space = out.Len() != 0
			continue
		}
		if space {
			out.WriteByte(' ')
			space = false
		}
		if r == 0 {
			r = '\ufffd'
		}
		if folded, ok := fullCaseFold[r]; ok {
			out.WriteString(folded)
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// referenceParagraph is a parse-private logical buffer, never a replacement
// Source. Each part maps bytes to an original line after container/paragraph
// indentation. The builder grows once; extraction advances offsets without
// rematerializing each suffix. Original coordinates always come from parts.
type referenceParagraph struct {
	text         strings.Builder
	parts        []referencePart
	lazyOrdinary bool // a proved ordinary remainder stays ordinary on append
	firstLazy    Optional[int]
}

type referencePart struct {
	start int // logical byte offset
	line  blockLine
}

var referenceSemanticText = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\x00", "\ufffd")

func (r *referenceParagraph) append(source Source, line blockLine) {
	if line.lazy && !r.firstLazy.Present {
		r.firstLazy = Some(r.text.Len())
	}
	r.parts = append(r.parts, referencePart{r.text.Len(), line})
	r.text.WriteString(source.content[line.contentStart:line.next])
}

func (r *referenceParagraph) position(offset int) int {
	i := sort.Search(len(r.parts), func(i int) bool { return r.parts[i].start > offset }) - 1
	part := r.parts[max(i, 0)]
	return part.line.contentStart + offset - part.start
}

// value selects original components across stripped container prefixes. A
// present empty component has an explicit zero-width source range. Labels keep
// this raw spelling; destinations and titles pass through the shared decoder.
func (r *referenceParagraph) value(index LineIndex, start, end int) Value {
	var value Value
	var semantic strings.Builder
	first := sort.Search(len(r.parts), func(i int) bool { return r.parts[i].start > start }) - 1
	for i := max(first, 0); i < len(r.parts); i++ {
		part := r.parts[i]
		a, b := max(start, part.start), min(end, part.start+part.line.next-part.line.contentStart)
		if a > b || a == b && start != end {
			continue
		}
		span := index.span(part.line.contentStart+a-part.start, part.line.contentStart+b-part.start)
		raw := index.source.content[span.start:span.end]
		text := referenceSemanticText.Replace(raw)
		kind := Exact
		if text != raw {
			kind = Normalized
		}
		value.Segments = append(value.Segments, Segment{Source: span, Transformation: kind, Semantic: Some(text)})
		semantic.WriteString(text)
		if b == end {
			break
		}
	}
	value.Semantic = Some(semantic.String())
	return value
}

type referenceSyntax struct {
	start, labelEnd                  int
	destinationStart, destinationEnd int
	titleStart, titleEnd             int
	end                              int
	angle, title                     bool
}

func referenceSpace(c byte) bool  { return c == ' ' || c == '\t' }
func referenceEnding(c byte) bool { return c == '\n' || c == '\r' }

func skipReferenceEnding(s string, i int) int {
	if i < len(s) && s[i] == '\r' {
		i++
		if i < len(s) && s[i] == '\n' {
			i++
		}
	} else if i < len(s) && s[i] == '\n' {
		i++
	}
	return i
}

func skipReferenceSpaces(s string, i int) int {
	for i < len(s) && referenceSpace(s[i]) {
		i++
	}
	return i
}

func referenceWhitespace(s string, i int) int {
	i = skipReferenceSpaces(s, i)
	if i < len(s) && referenceEnding(s[i]) {
		i = skipReferenceSpaces(s, skipReferenceEnding(s, i))
	}
	return i
}

func escapedReferenceByte(s string, i int) bool {
	return s[i] == '\\' && i+1 < len(s) && IsASCIIPunctuation(rune(s[i+1]))
}

// scanReference recognizes only one whole leading definition. A failed title
// on a subsequent line rolls back to the destination line; same-line failure
// invalidates the definition. Scanning is iterative and checks long components.
func scanReference(ctx context.Context, s string, start int) (referenceSyntax, bool) {
	result := probeReference(ctx, s, start)
	return result.syntax, result.valid
}

type referenceScan struct {
	syntax referenceSyntax
	valid  bool
	more   bool // additional physical lines might complete this candidate
	title  *referenceTitleScan
}

type referenceTitleScan struct {
	syntax          referenceSyntax
	offset          int
	open, close     byte
	fallbackEnd     int
	destinationOnly bool
}

func probeReference(ctx context.Context, s string, start int) referenceScan {
	r := referenceSyntax{start: start}
	if start >= len(s) || s[start] != '[' {
		return referenceScan{}
	}
	i, characters, nonblank := start+1, 0, false
	for i < len(s) && s[i] != ']' {
		if s[i] == '[' || interruption(ctx) != "" {
			return referenceScan{}
		}
		c, size := utf8.DecodeRuneInString(s[i:])
		if c == '\r' && i+1 < len(s) && s[i+1] == '\n' {
			// CommonMark preprocessing treats CRLF as one line ending, without
			// changing the original byte offsets retained by the logical view.
			size = 2
		}
		characters++
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			nonblank = true
		}
		if escapedReferenceByte(s, i) {
			i++
			characters++
		}
		i += size
		if characters > 999 {
			return referenceScan{}
		}
	}
	if i == len(s) {
		return referenceScan{more: true}
	}
	if !nonblank || i+1 >= len(s) || s[i] != ']' || s[i+1] != ':' {
		return referenceScan{}
	}
	r.labelEnd = i
	i = referenceWhitespace(s, i+2)
	if i >= len(s) {
		return referenceScan{more: true}
	}
	if referenceEnding(s[i]) {
		return referenceScan{}
	}
	r.angle = s[i] == '<'
	if r.angle {
		i++
	}
	r.destinationStart = i
	depth := 0
	for i < len(s) {
		if i%256 == 0 && interruption(ctx) != "" {
			return referenceScan{}
		}
		c := s[i]
		if escapedReferenceByte(s, i) {
			i += 2
			continue
		}
		if r.angle {
			if c == '>' {
				break
			}
			if c == '<' || referenceEnding(c) {
				return referenceScan{}
			}
		} else {
			if c != 0 && c <= ' ' || c == 127 {
				break
			}
			if c == '(' {
				depth++
			} else if c == ')' {
				if depth == 0 {
					return referenceScan{}
				}
				depth--
			}
		}
		i++
	}
	r.destinationEnd = i
	if r.angle {
		if i == len(s) || s[i] != '>' {
			return referenceScan{}
		}
		i++
	} else if depth != 0 || i == r.destinationStart {
		return referenceScan{}
	}
	destinationEnd := i
	lineEnd := skipReferenceSpaces(s, i)
	destinationOnly := lineEnd == len(s) || referenceEnding(s[lineEnd])
	fallback := skipReferenceEnding(s, lineEnd)
	i = referenceWhitespace(s, i)
	if i > destinationEnd && i < len(s) && (s[i] == '\'' || s[i] == '"' || s[i] == '(') {
		open, close := s[i], s[i]
		if close == '(' {
			close = ')'
		}
		r.titleStart = i + 1
		return resumeReferenceTitle(ctx, s, &referenceTitleScan{syntax: r, offset: i + 1, open: open, close: close, destinationOnly: destinationOnly, fallbackEnd: fallback})
	}
	if destinationOnly {
		r.end = fallback
		return referenceScan{syntax: r, valid: true}
	}
	return referenceScan{}
}

// The lookahead cursor resumes at the previous physical line's end. Escaped
// punctuation is consumed as a pair within a line, so an escape cannot straddle
// a resume boundary. No growing multiline title is scanned from its opener twice.
func resumeReferenceTitle(ctx context.Context, s string, pending *referenceTitleScan) referenceScan {
	r := pending.syntax
	i := pending.offset
	for i < len(s) && s[i] != pending.close {
		if i%256 == 0 && interruption(ctx) != "" {
			return referenceScan{}
		}
		if pending.open == '(' && s[i] == '(' {
			break
		}
		if escapedReferenceByte(s, i) {
			i++
		}
		i++
	}
	if i < len(s) && s[i] == pending.close {
		r.titleEnd = i
		end := skipReferenceSpaces(s, i+1)
		if end == len(s) || referenceEnding(s[end]) {
			r.title, r.end = true, skipReferenceEnding(s, end)
			return referenceScan{syntax: r, valid: true}
		}
	}
	result := referenceScan{}
	if i == len(s) {
		pending.offset = i
		result.more, result.title = true, pending
	}
	if pending.destinationOnly {
		r.end = pending.fallbackEnd
		result.syntax, result.valid = r, true
	}
	return result
}

func (r *referenceParagraph) definition(ctx context.Context, index LineIndex, syntax referenceSyntax) (*ReferenceDefinition, error) {
	span := func(a, b int) Span { return index.span(r.position(a), r.position(b)) }
	first := sort.Search(len(r.parts), func(i int) bool { return r.parts[i].start >= syntax.start })
	spec := ReferenceSpec{
		Envelope:        index.span(r.parts[first].line.start, r.position(syntax.end-1)+1),
		Label:           r.value(index, syntax.start+1, syntax.labelEnd),
		Destination:     decodeValue(ctx, r.value(index, syntax.destinationStart, syntax.destinationEnd).Segments, true),
		NormalizedLabel: Some(normalizeReferenceLabel(r.text.String()[syntax.start+1 : syntax.labelEnd])),
		Markers: []Marker{
			{Role: "label-open", Source: span(syntax.start, syntax.start+1)},
			{Role: "label-close", Source: span(syntax.labelEnd, syntax.labelEnd+1)},
			{Role: "definition-colon", Source: span(syntax.labelEnd+1, syntax.labelEnd+2)},
		},
	}
	if syntax.angle {
		spec.Markers = append(spec.Markers, Marker{"destination-open", span(syntax.destinationStart-1, syntax.destinationStart)}, Marker{"destination-close", span(syntax.destinationEnd, syntax.destinationEnd+1)})
	}
	if syntax.title {
		v := decodeValue(ctx, r.value(index, syntax.titleStart, syntax.titleEnd).Segments, true)
		spec.Title = &v
		spec.Markers = append(spec.Markers, Marker{"title-open", span(syntax.titleStart-1, syntax.titleStart)}, Marker{"title-close", span(syntax.titleEnd, syntax.titleEnd+1)})
	}
	return NewReferenceDefinition(spec)
}

// stripReferences runs only at closure/promotion. Mutable registry and boundary
// accumulation stay private until the entire block scan succeeds.
func (p *blockParser) stripReferences() error {
	block := &p.stack[len(p.stack)-1]
	r := block.reference
	if r == nil {
		return nil
	}
	s, offset := r.text.String(), 0
	for offset < len(s) {
		if interruption(p.ctx) != "" {
			p.referenceInterrupted = true
			return nil
		}
		syntax, ok := scanReference(p.ctx, s, offset)
		if !ok {
			break
		}
		if r.firstLazy.Present && syntax.end > r.firstLazy.Value {
			// Defensive provenance gate: paragraph-only laziness can never
			// make an outside line disappear into definition metadata.
			break
		}
		definition, err := r.definition(p.ctx, p.index, syntax)
		if err != nil {
			return err
		}
		p.definitions = append(p.definitions, definition)
		if !definition.spec.Destination.Semantic.Present || definition.spec.Title != nil && !definition.spec.Title.Semantic.Present {
			p.referenceSemanticsPending = true
		}
		if len(p.stack) > 1 && p.stack[len(p.stack)-2].kind == ListItem {
			parent := &p.stack[len(p.stack)-2]
			parent.definitions = append(parent.definitions, definition.spec.Envelope)
		}
		offset = syntax.end
	}
	if interruption(p.ctx) != "" {
		p.referenceInterrupted = true
		return nil
	}
	block.reference = nil
	if offset == 0 {
		return nil
	}
	if offset == len(s) {
		p.stack = p.stack[:len(p.stack)-1]
		return nil
	}
	first := sort.Search(len(r.parts), func(i int) bool { return r.parts[i].start >= offset })
	block.start = r.parts[first].line.start
	block.content = nil
	for i := first; i+1 < len(r.parts); i++ {
		line := r.parts[i].line
		block.content = append(block.content, p.index.semanticText(line.contentStart, line.next).Segments...)
	}
	return nil
}

// paragraphAllowsLazy decides using the complete candidate, not just whether
// its currently accumulated prefix happens to parse. An incomplete definition
// may still be ordinary prose; look ahead only until recognition settles or the
// paragraph ends. Pending titles retain their scan offset and delimiter state.
// The decision is cached once proved ordinary, and no lookahead nodes, markers,
// container ends or registry entries are published.
func (p *blockParser) paragraphAllowsLazy(physical physicalLine) bool {
	paragraph := p.stack[len(p.stack)-1].reference
	if paragraph == nil || paragraph.lazyOrdinary {
		return true
	}
	var text strings.Builder
	text.WriteString(paragraph.text.String())
	boundary := text.Len()
	offset := 0
	var title *referenceTitleScan
	// settle returns (decided, allow). Before the boundary a complete leading
	// sequence has no paragraph to continue; beyond it any successful definition
	// would consume a missing container prefix, which is also forbidden.
	settle := func() (bool, bool) {
		for {
			if interruption(p.ctx) != "" {
				p.referenceInterrupted = true
				return true, false
			}
			var result referenceScan
			if title != nil {
				result = resumeReferenceTitle(p.ctx, text.String(), title)
			} else {
				result = probeReference(p.ctx, text.String(), offset)
			}
			if result.more {
				title = result.title
				return false, false
			}
			title = nil
			if !result.valid {
				return true, true
			}
			if result.syntax.end > boundary || result.syntax.end == text.Len() {
				return true, false
			}
			offset = result.syntax.end
		}
	}
	if decided, allow := settle(); decided {
		paragraph.lazyOrdinary = allow
		return allow
	}
	first := sort.Search(len(p.index.lines), func(i int) bool { return p.index.lines[i].start >= physical.start })
	for _, next := range p.index.lines[first:] {
		if interruption(p.ctx) != "" {
			p.referenceInterrupted = true
			return false
		}
		line, continues := p.referenceContinuationLine(next)
		if !continues {
			break
		}
		text.WriteString(p.index.source.content[line.contentStart:line.next])
		if decided, allow := settle(); decided {
			paragraph.lazyOrdinary = allow
			return allow
		}
	}
	// No definition can complete inside this paragraph. Preserve ordinary lazy
	// text, but the extraction provenance gate still forbids consuming its lines.
	paragraph.lazyOrdinary = true
	return true
}

// referenceContinuationLine mirrors container-prefix matching without changing
// the live builders. Blank lines and real block/Setext boundaries end lookahead.
func (p *blockParser) referenceContinuationLine(physical physicalLine) (blockLine, bool) {
	cursor := Indentation{index: p.index, offset: physical.start, end: physical.end}
	depth, matched := p.containerDepth(), 1
	for matched < depth {
		block := p.stack[matched]
		if block.kind == BlockQuote {
			next, _, ok := p.quotePrefix(cursor)
			if !ok {
				break
			}
			cursor = next
		} else {
			next := cursor
			consumed, _ := next.Consume(block.list.ContentIndent.Value)
			if consumed.Columns != block.list.ContentIndent.Value {
				break
			}
			cursor = next
		}
		matched++
	}
	line := p.logicalLine(physical, cursor)
	if line.contentStart == line.end || matched < depth && p.siblingItem(matched, line) || p.interruptsParagraph(line) {
		return line, false
	}
	if matched == depth {
		if _, level := p.setextUnderline(line); level != 0 {
			return line, false
		}
	}
	return line, true
}
