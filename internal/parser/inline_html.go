package parser

import (
	"slices"
	"strings"
)

// Index source-logical terminators once. In particular, repeated unterminated
// comments/PI/CDATA and quoted attributes must not search the same suffix at
// every rejected opener. Binary searches also work when a failed tag's quoted
// attribute looked past the next candidate. State belongs to one inline block.
func (s *inlineScanner) indexHTMLEnds() {
	meterWork(s.ctx, 1)
	s.htmlEnds = make(map[string][]int)
	for i := 0; i < len(s.text); i++ {
		meterWork(s.ctx, 1)
		if i%256 == 0 && interruption(s.ctx) != "" {
			return
		}
		switch s.text[i] {
		case '\'', '"':
			key := s.text[i : i+1]
			s.htmlEnds[key] = meteredAppend(s.ctx, s.htmlEnds[key], i)
		case '>':
			s.htmlEnds[">"] = meteredAppend(s.ctx, s.htmlEnds[">"], i)
			for _, token := range []string{"-->", "?>", "]]>"} {
				meterWork(s.ctx, 1)
				start := i + 1 - len(token)
				if start >= 0 && s.text[start:i+1] == token {
					s.htmlEnds[token] = meteredAppend(s.ctx, s.htmlEnds[token], start)
				}
			}
		}
	}
}

func (s *inlineScanner) htmlEnd(token string, from int) int {
	meterWork(s.ctx, 1)
	if s.htmlEnds == nil {
		s.indexHTMLEnds()
	}
	if interruption(s.ctx) != "" {
		return -1
	}
	ends := s.htmlEnds[token]
	i, _ := slices.BinarySearch(ends, from)
	if i == len(ends) {
		return -1
	}
	return ends[i]
}

// inlineHTMLEnd implements CommonMark 0.31.2 Raw HTML on original logical
// spelling, before Text decoding. closing is nonzero only for confirmed comments
// and names their closing boundary in the same logical coordinate space.
func (s *inlineScanner) inlineHTMLEnd(start int) (end, closing int) {
	meterWork(s.ctx, 1)
	text := s.text
	i := start + 1
	if i >= len(text) {
		return 0, 0
	}
	if meteredHasPrefix(s.ctx, text[start:], "<!--") {
		for _, short := range []string{"<!-->", "<!--->"} {
			meterWork(s.ctx, 1)
			if meteredHasPrefix(s.ctx, text[start:], short) {
				return start + len(short), start + 4
			}
		}
		if close := s.htmlEnd("-->", start+4); close >= 0 {
			return close + 3, close
		}
		return 0, 0
	}
	for _, form := range []struct{ open, close string }{{"<?", "?>"}, {"<![CDATA[", "]]>"}} {
		meterWork(s.ctx, 1)
		if meteredHasPrefix(s.ctx, text[start:], form.open) {
			if close := s.htmlEnd(form.close, start+len(form.open)); close >= 0 {
				return close + len(form.close), 0
			}
			return 0, 0
		}
	}
	if text[i] == '!' {
		if i+1 < len(text) && asciiLetter(text[i+1]) {
			if close := s.htmlEnd(">", i+2); close >= 0 {
				return close + 1, 0
			}
		}
		return 0, 0
	}
	closeTag := text[i] == '/'
	if closeTag {
		i++
	}
	if i >= len(text) || !asciiLetter(text[i]) {
		return 0, 0
	}
	for i++; i < len(text) && (asciiAlphanumeric(text[i]) || text[i] == '-'); i++ {
		meterWork(s.ctx, 1)
		if i%256 == 0 && interruption(s.ctx) != "" {
			return 0, 0
		}
	}
	for {
		meterWork(s.ctx, 1)
		if interruption(s.ctx) != "" {
			return 0, 0
		}
		before := i
		var ok bool
		i, ok = s.htmlSpace(i)
		if !ok || i >= len(text) {
			return 0, 0
		}
		if !closeTag && text[i] == '/' {
			i++
			if i >= len(text) || text[i] != '>' {
				return 0, 0
			}
		}
		if i < len(text) && text[i] == '>' {
			return i + 1, 0
		}
		if closeTag || i == before || i >= len(text) || !htmlAttributeStart(text[i]) {
			return 0, 0
		}
		for i++; i < len(text) && (htmlAttributeStart(text[i]) || asciiDigit(text[i]) || text[i] == '.' || text[i] == '-'); i++ {
			meterWork(s.ctx, 1)
			if i%256 == 0 && interruption(s.ctx) != "" {
				return 0, 0
			}
		}
		// Whitespace without '=' belongs to the next attribute or tag ending.
		afterName := i
		i, ok = s.htmlSpace(i)
		if !ok || i >= len(text) || text[i] != '=' {
			i = afterName
			continue
		}
		i, ok = s.htmlSpace(i + 1)
		if !ok || i >= len(text) {
			return 0, 0
		}
		if text[i] == '\'' || text[i] == '"' {
			end := s.htmlEnd(text[i:i+1], i+1)
			if end < 0 {
				return 0, 0
			}
			i = end + 1
			continue
		}
		value := i
		for i < len(text) && !meteredContainsRune(s.ctx, " \t\r\n\"'=<>`", rune(text[i])) {
			meterWork(s.ctx, 1)
			if i%256 == 0 && interruption(s.ctx) != "" {
				return 0, 0
			}
			i++
		}
		if i == value {
			return 0, 0
		}
	}
}

// The grammar permits spaces/tabs and at most one physical line ending in each
// whitespace production. CRLF is one ending, and form feed is not whitespace.
func (s *inlineScanner) htmlSpace(i int) (int, bool) {
	meterWork(s.ctx, 1)
	ending := false
	for i < len(s.text) {
		meterWork(s.ctx, 1)
		if i%256 == 0 && interruption(s.ctx) != "" {
			return i, false
		}
		switch s.text[i] {
		case ' ', '\t':
			i++
		case '\r', '\n':
			if ending {
				return i, false
			}
			ending = true
			if s.text[i] == '\r' && i+1 < len(s.text) && s.text[i+1] == '\n' {
				i++
			}
			i++
		default:
			return i, true
		}
	}
	return i, true
}

func htmlAttributeStart(c byte) bool { return asciiLetter(c) || c == '_' || c == ':' }

// Content is borrowed from the immutable block snapshot. NewNode freezes these
// private slices. Comment markers identify exact original boundaries even when
// the comment's physical envelope includes omitted container prefixes.
func scanInlineHTML(cursor *inlineContent, start, end, closing int) (*Node, error) {
	meterWork(cursor.ctx, 1)
	var content []Segment
	var markers []Marker
	if closing != 0 {
		opening, err := cursor.take(start + 4)
		if err != nil || interruption(cursor.ctx) != "" {
			return nil, err
		}
		payload, err := cursor.take(closing)
		if err != nil || interruption(cursor.ctx) != "" {
			return nil, err
		}
		close, err := cursor.take(end)
		if err != nil || interruption(cursor.ctx) != "" {
			return nil, err
		}
		content = meteredAppend(cursor.ctx, meteredAppend(cursor.ctx, opening, payload...), close...)
		markers = []Marker{{Role: "html-comment-open", Source: contentEnvelope(opening)}, {Role: "html-comment-close", Source: contentEnvelope(close)}}
	} else {
		var err error
		content, err = cursor.take(end)
		if err != nil || interruption(cursor.ctx) != "" {
			return nil, err
		}
	}
	var rawTag string
	rawClosing := false
	if closing == 0 {
		envelope := contentEnvelope(content)
		raw, _ := envelope.source.Text(envelope)
		begin := 1
		if len(raw) > 1 && raw[1] == '/' {
			rawClosing = true
			begin++
		}
		end := htmlTagNameEndContext(cursor.ctx, raw, begin)
		if end > begin && htmlRawTag(raw[begin:end]) {
			rawTag = strings.ToLower(raw[begin:end])
		}
	}
	node, err := meteredNode(cursor.ctx, NodeSpec{Kind: InlineHTML, Envelope: contentEnvelope(content), Content: content,
		Markers: markers, Properties: Properties{HTML: &HTMLProperties{}}})
	if node != nil {
		node.rawTag, node.rawClosing = rawTag, rawClosing
	}
	return node, err
}
