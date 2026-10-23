package parser

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (p *blockParser) pandoc() bool {
	c, ok := p.ctx.(*parserContext)
	return ok && c.profile == pandocBlocksProfile
}

func (p *blockParser) classifyListStart(line blockLine, paragraph bool) (listStart, bool) {
	if !p.pandoc() {
		return classifyListStart(line, paragraph)
	}
	if paragraph && !p.inPandocItem() {
		return listStart{}, false
	}
	if previous := p.lastPandocBlock(); previous != nil && previous.Kind() == ListItem {
		x := previous.spec.Properties.PandocList
		if x != nil && x.MarkerColumn == line.cursor.column+line.indent {
			if start, ok := p.pandocActualListStart(line, x, false); ok {
				start.sibling = true
				return start, true
			}
		}
	}
	return p.pandocActualListStart(line, nil, false)
}
func (p *blockParser) inPandocItem() bool {
	for i := len(p.stack) - 1; i > 0; i-- {
		meterWork(p.ctx, 1)
		if p.stack[i].kind == ListItem || p.stack[i].kind == DefinitionBody {
			return true
		}
	}
	return false
}

// Marker classification is read-only. In particular speculative lookahead never
// advances the document-wide example counter or changes an established label.
// pandocListStart is the permissive boundary probe used by extraction/endline.
func (p *blockParser) pandocListStart(line blockLine, expected *PandocListProperties, definition bool) (listStart, bool) {
	return p.pandocMarkerStart(line, expected, definition, false)
}

// Actual item consumption has stricter delimiter spelling than the initial probe.
func (p *blockParser) pandocActualListStart(line blockLine, expected *PandocListProperties, definition bool) (listStart, bool) {
	return p.pandocMarkerStart(line, expected, definition, true)
}
func (p *blockParser) pandocMarkerStart(line blockLine, expected *PandocListProperties, definition, actual bool) (listStart, bool) {
	meterWork(p.ctx, 1)
	if line.indent >= 4 {
		return listStart{}, false
	}
	c := line.cursor
	for c.offset < line.contentStart {
		advanceListSpace(&c)
	}
	marker := c
	raw := p.index.source.content
	meterRecords(p.ctx, 1) // provisional typed classification facts, including lookahead
	props := &PandocListProperties{MarkerColumn: c.column, ContainerColumn: line.cursor.column, LeadingColumns: c.column - line.cursor.column}
	core := ListProperties{}
	end := c.offset
	if definition {
		if line.first != ':' && line.first != '~' {
			return listStart{}, false
		}
		end++
		props.Style = "definition"
		props.MarkerStyle = "definition"
		props.Delimiter = "definition"
	} else if strings.ContainsRune("*+-", rune(line.first)) {
		if len(p.thematicMarkers(line)) != 0 {
			return listStart{}, false
		}
		end++
		props.Style = "bullet"
		props.MarkerStyle = "bullet"
		props.Delimiter = "bullet"
		core.Bullet = Some(line.first)
	} else {
		// The page-reference exception is an original-character guard, before
		// optional marker parentheses or logical separator expansion.
		meterWork(p.ctx, 4)
		if end+3 < line.end && raw[end] == 'p' && raw[end+1] == '.' &&
			(raw[end+2] == ' ' || raw[end+2] == '\t') && raw[end+3] >= '0' && raw[end+3] <= '9' {
			return listStart{}, false
		}
		limit := end
		for limit < line.end && raw[limit] != ' ' && raw[limit] != '\t' {
			meterWork(p.ctx, 1)
			limit++
		}
		if limit == end {
			return listStart{}, false
		}
		token := raw[end:limit]
		body := token
		delimiter := ""
		opened := strings.HasPrefix(body, "(")
		if opened {
			body = body[1:]
		}
		if strings.HasSuffix(body, ".") {
			if actual && opened {
				return listStart{}, false
			}
			delimiter = "period"
			body = body[:len(body)-1]
		} else if strings.HasSuffix(body, ")") {
			delimiter = "one-paren"
			body = body[:len(body)-1]
			if opened {
				delimiter = "two-parens"
			}
		} else {
			return listStart{}, false
		}
		// Only the broad probe accepts an opening parenthesis with a period;
		// selected-style actual consumption rejects it before numbering/source facts.
		if body == "" {
			return listStart{}, false
		}
		// Reserve the pure classifier's bounded repeated byte/rune scans, numeric
		// conversion, label hashing and raw metadata classification before doing them.
		meterPandocWork(p.ctx, pandocWork(64, int64(len(body)), 32))
		style, value, label, decimal, ok := pandocNumber(body, expected)
		if !ok {
			return listStart{}, false
		}
		if expected != nil && expected.Style != "bullet" && (delimiter != expected.Delimiter && !(delimiter == "period" && expected.Delimiter == "default")) {
			return listStart{}, false
		}
		props.MarkerStyle = style
		if expected != nil && expected.Style != "bullet" {
			style = expected.Style
		}
		if style == "default" && delimiter == "period" {
			delimiter = "default"
		}

		props.Style, props.Delimiter, props.Start = style, delimiter, value
		if style == "example" && strings.Contains(body, "@") {
			at := strings.IndexByte(body, '@')
			if at > 0 {
				if reset, err := strconv.Atoi(body[:at]); err == nil {
					props.Reset = Some(reset)
				}
			}
			labelText := body[at+1:]
			next := p.exampleNext
			if !p.exampleStarted {
				next = 1
			}
			if props.Reset.Present {
				next = props.Reset.Value
			}
			if number, exists := p.examples[labelText]; labelText != "" && exists {
				next = number
			}
			props.Start = next
			value = next
		}
		if decimal {
			start := end
			if opened {
				start++
			}
			props.Decimal = p.index.span(start, start+len(body))
		}
		if label >= 0 {
			start := end
			if opened {
				start++
			}
			at := strings.IndexByte(body, '@')
			props.Label = p.index.span(start+at+1, start+len(body))
		}
		core.Ordered, core.Start = true, Some(value)
		if delimiter == "period" || delimiter == "default" {
			core.Delimiter = Some(byte('.'))
		} else {
			core.Delimiter = Some(byte(')'))
		}
		end = limit
	}
	for c.offset < end {
		meterWork(p.ctx, 1)
		_, _ = c.AdvanceMarker()
	}
	after := c
	if c.offset < line.end && raw[c.offset] != ' ' && raw[c.offset] != '\t' {
		return listStart{}, false
	}

	if props.Delimiter == "period" && (props.Style == "upper-alpha" || props.Style == "upper-roman" && (props.Start == 1 || props.Start == 5 || props.Start == 10 || props.Start == 50 || props.Start == 100 || props.Start == 500 || props.Start == 1000)) {
		n := c
		n.end = line.end
		if n.offset < n.end {
			_, _ = n.Consume(1)
		}
		if n.tabRemaining == 0 && n.offset < line.end && raw[n.offset] != ' ' && raw[n.offset] != '\t' {
			return listStart{}, false
		}
	}
	// Consume at most four separating columns when a following nonspace exists;
	// a larger run contributes only one column, leaving code indentation payload.
	content := after
	for content.offset < line.end && (raw[content.offset] == ' ' || raw[content.offset] == '\t') {
		meterWork(p.ctx, 1)
		advanceListSpace(&content)
	}
	if content.column-after.column > 4 {
		content = after
		_, _ = content.Consume(1)
	}
	padding := content.column - after.column
	if padding == 0 {
		padding = 1
	}
	props.Marker = p.index.span(marker.offset, after.offset)
	if content.offset > after.offset || content.tabRemaining > 0 {
		end := content.offset
		if content.tabRemaining > 0 {
			end++
		}
		props.Separator = p.index.span(after.offset, end)
	}
	props.SourceMarkerColumn = p.pandocSourceColumn(marker.offset)
	props.SourceContentColumn = p.pandocSourceColumn(content.offset)
	props.ContentColumn = content.column
	props.SeparatorColumns = content.column - after.column
	props.SeparatorPartialTab = content.tabRemaining > 0
	props.ContinuationIndent = marker.column - line.cursor.column + after.column - marker.column + max(1, props.SeparatorColumns)
	if props.ContinuationIndent < 1 {
		props.ContinuationIndent = marker.column - line.cursor.column + after.column - marker.column + 1
	}
	if props.Style == "example" {
		props.ContinuationIndent = 4
	}
	core.MarkerWidth = Some(after.column - marker.column)
	core.Padding = Some(padding)
	core.ContentIndent = Some(props.ContinuationIndent)
	// Prefix contribution count is an allocation upper bound; construction owns
	// the actual source-backed segments and physical tab phase.
	return listStart{line: line, marker: marker, after: after, content: content, properties: core, prefixSegments: marker.offset - line.start + content.offset - after.offset + 3, pandoc: props}, true
}

func pandocNumber(body string, expected *PandocListProperties) (style string, value, label int, decimal, ok bool) {
	if body == "#" {
		return "default", 1, -1, false, true
	}
	if at := strings.IndexByte(body, '@'); at >= 0 {
		if expected != nil && expected.Style != "example" {
			return "", 0, 0, false, false
		}
		prefix := body[:at]
		if prefix != "" && !asciiDigits(prefix) {
			return "", 0, 0, false, false
		}
		if !pandocExampleLabel(body[at+1:]) {
			return "", 0, 0, false, false
		}
		value = 0
		if prefix != "" {
			if n, err := strconv.Atoi(prefix); err == nil {
				value = n
			}
		}
		return "example", value, at, false, true
	}
	if asciiDigits(body) {
		if expected != nil && expected.Style != "decimal" && expected.Style != "default" {
			return "", 0, 0, false, false
		}
		return "decimal", safePandocInt(body), -1, true, true
	}
	if expected != nil {
		switch expected.Style {
		case "lower-alpha":
			if len(body) == 1 && body[0] >= 'a' && body[0] <= 'z' {
				return expected.Style, int(body[0]-'a') + 1, -1, false, true
			}
		case "upper-alpha":
			if len(body) == 1 && body[0] >= 'A' && body[0] <= 'Z' {
				return expected.Style, int(body[0]-'A') + 1, -1, false, true
			}
		case "lower-roman", "upper-roman":
			n, yes := pandocRoman(body, expected.Style == "upper-roman")
			return expected.Style, n, -1, false, yes
		}
		return "", 0, 0, false, false
	}
	if body == "i" {
		return "lower-roman", 1, -1, false, true
	}
	if body == "I" {
		return "upper-roman", 1, -1, false, true
	}
	if len(body) == 1 {
		if body[0] >= 'a' && body[0] <= 'z' {
			return "lower-alpha", int(body[0]-'a') + 1, -1, false, true
		}
		if body[0] >= 'A' && body[0] <= 'Z' {
			return "upper-alpha", int(body[0]-'A') + 1, -1, false, true
		}
	}
	if n, yes := pandocRoman(body, false); yes {
		return "lower-roman", n, -1, false, true
	}
	if n, yes := pandocRoman(body, true); yes {
		return "upper-roman", n, -1, false, true
	}
	return "", 0, 0, false, false
}
func asciiDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
func safePandocInt(s string) int {
	n, e := strconv.Atoi(s)
	if e != nil {
		return 1
	}
	return n
}
func pandocExampleLabel(s string) bool {
	for len(s) > 0 {
		r, n := utf8.DecodeRuneInString(s)
		if r == '_' || r == '-' {
			s = s[n:]
			if s == "" {
				return false
			}
			r, n = utf8.DecodeRuneInString(s)
		}
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			return false
		}
		s = s[n:]
	}
	return true
}
func pandocRoman(s string, upper bool) (int, bool) {
	chars := []string{"m", "cm", "d", "cd", "c", "xc", "l", "xl", "x", "ix", "v", "iv", "i"}
	values := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	n := 0
	for i, ch := range chars {
		if upper {
			ch = strings.ToUpper(ch)
		}
		for strings.HasPrefix(s, ch) {
			if n > int(^uint(0)>>1)-values[i] {
				return 0, false
			}
			n += values[i]
			s = s[len(ch):]
			if len(ch) == 2 || i == 2 || i == 6 || i == 10 {
				break
			}
		}
	}
	return n, s == "" && n > 0
}

func (p *blockParser) resolvePandocExample(start *listStart) bool {
	x := start.pandoc
	if x == nil || x.Style != "example" || !strings.Contains(p.index.source.content[x.Marker.Start():x.Marker.End()], "@") {
		return true
	}
	if !p.exampleStarted {
		p.exampleStarted = true
		p.exampleNext = 1
	}
	if x.Reset.Present {
		p.exampleNext = x.Reset.Value
		p.exampleOverflow = false
	}
	label := ""
	if x.Label.Present() {
		label = p.index.source.content[x.Label.Start():x.Label.End()]
	}
	n, exists := p.examples[label]
	if label == "" || !exists {
		if p.exampleOverflow {
			return false
		}
		n = p.exampleNext
		if p.exampleNext < int(^uint(0)>>1) {
			p.exampleNext++
		} else {
			p.exampleOverflow = true
		}
		if label != "" {
			meterWork(p.ctx, len(label))
			meterRecords(p.ctx, 1)
			if p.examples == nil {
				p.examples = make(map[string]int)
			}
			p.examples[label] = n
		}
	}
	x.Start = n
	start.properties.Start = Some(n)
	return true
}

// payloadCursor restores the pinned reader's recursive container position while
// borrowing the original snapshot and any residual expanded-tab contribution.
func (s listStart) payloadCursor() Indentation {
	c := s.content
	if s.pandoc != nil {
		c.column = s.line.cursor.column
	}
	return c
}
func (p *blockParser) pandocSourceColumn(offset int) int {
	start := offset
	for start > 0 && p.index.source.content[start-1] != '\r' && p.index.source.content[start-1] != '\n' {
		meterWork(p.ctx, 1)
		start--
	}
	meterWork(p.ctx, offset-start+1)
	return pandocColumn(p.index.source, offset)
}

// Item extraction is decided in the enclosing reader frame, independently of
// the recursively recognized leaf. A blank ends the current raw/continuation
// group; the first nonblank line of the next group needs full indentation.
type pandocItemState struct{ continuation, afterBlank bool }
type pandocItemMatch struct {
	cursor    Indentation
	consumed  IndentationConsumption
	state     pandocItemState
	blank, ok bool
}

func (p *blockParser) pandocItemLine(line blockLine, indent int, state pandocItemState) pandocItemMatch {
	next := line.cursor
	consumed, _ := next.Consume(indent)
	full := consumed.Columns == indent
	result := pandocItemMatch{cursor: next, consumed: consumed, state: state, blank: line.contentStart == line.end}
	if result.blank {
		result.state = pandocItemState{continuation: true, afterBlank: true}
		result.cursor.column = line.cursor.column
		result.ok = true
		return result
	}
	if state.afterBlank && !full {
		return pandocItemMatch{}
	}
	if !state.continuation || !full {
		_, list := p.pandocListStart(line, nil, false)
		if !list {
			_, list = p.pandocListStart(line, nil, true)
		}
		if list && !full {
			return pandocItemMatch{}
		}
		if !state.continuation {
			fence := p.fenceOpening(line) != nil
			if fence && !full {
				return pandocItemMatch{}
			}
			nested := false
			if full && !list && !fence {
				// listLine also stops before an indented nested marker after any residual
				// whitespace. The following continuation group owns the unchanged suffix.
				probe := next
				raw := p.index.source.content
				for probe.offset < probe.end && (raw[probe.offset] == ' ' || raw[probe.offset] == '\t') {
					advanceListSpace(&probe)
				}
				candidate := p.logicalLine(line.physicalLine, probe)
				_, nested = p.pandocListStart(candidate, nil, false)
				if !nested {
					_, nested = p.pandocListStart(candidate, nil, true)
				}
			}
			result.state.continuation = list || fence || nested
		}
	}
	result.state.afterBlank = false
	if !full {
		result.cursor = line.cursor // optional indentation is all-or-nothing
		result.consumed = IndentationConsumption{}
	}
	result.cursor.column = line.cursor.column
	result.ok = true
	return result
}
