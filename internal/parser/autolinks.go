package parser

// autolinkEnd reads original logical content, before Text decoding. Each scan
// stops at the first forbidden byte (including another '<'), so repeated failed
// openers cannot repeatedly scan the same suffix. Scheme lookahead is bounded;
// unbounded URI/local-part scans retain caller interruption checkpoints.
func (s *inlineScanner) autolinkEnd(start int) (end int, email bool) {
	text := s.text
	i := start + 1
	if i < len(text) && asciiLetter(text[i]) {
		for i < len(text) && i-start <= 32 && (asciiLetter(text[i]) || asciiDigit(text[i]) || text[i] == '+' || text[i] == '.' || text[i] == '-') {
			i++
		}
		if width := i - start - 1; width >= 2 && width <= 32 && i < len(text) && text[i] == ':' {
			for i++; i < len(text); i++ {
				if (i-start)%256 == 0 && interruption(s.ctx) != "" {
					return 0, false
				}
				if text[i] == '>' {
					return i + 1, false
				}
				if text[i] <= ' ' || text[i] == 0x7f || text[i] == '<' {
					return 0, false
				}
			}
			return 0, false
		}
	}
	i = start + 1
	for i < len(text) && emailLocal(text[i]) {
		if (i-start)%256 == 0 && interruption(s.ctx) != "" {
			return 0, false
		}
		i++
	}
	if i == start+1 || i >= len(text) || text[i] != '@' {
		return 0, false
	}
	for {
		if interruption(s.ctx) != "" {
			return 0, false
		}
		i++ // '@' or '.'
		label := i
		if i >= len(text) || !asciiAlphanumeric(text[i]) {
			return 0, false
		}
		for i < len(text) && (asciiAlphanumeric(text[i]) || text[i] == '-') {
			if (i-start)%256 == 0 && interruption(s.ctx) != "" {
				return 0, false
			}
			i++
			if i-label > 63 {
				return 0, false
			}
		}
		if text[i-1] == '-' || i >= len(text) {
			return 0, false
		}
		if text[i] == '>' {
			return i + 1, true
		}
		if text[i] != '.' {
			return 0, false
		}
	}
}

func asciiAlphanumeric(c byte) bool { return asciiLetter(c) || asciiDigit(c) }
func asciiDigit(c byte) bool        { return c >= '0' && c <= '9' }

func emailLocal(c byte) bool {
	if asciiAlphanumeric(c) {
		return true
	}
	switch c {
	case '.', '!', '#', '$', '%', '&', '\'', '*', '+', '/', '=', '?', '^', '_', '`', '{', '|', '}', '~', '-':
		return true
	}
	return false
}

// The cursor borrows immutable block Content. NewNode snapshots the private
// slices at publication; Label/Content/child source facts may safely share the
// borrowed immutable contributions. Destination alone owns a synthetic prefix.
func scanAutolink(cursor *inlineContent, start, end int, literal string, email bool) (*Node, error) {
	opening, err := cursor.take(start + 1)
	if err != nil || interruption(cursor.ctx) != "" {
		return nil, err
	}
	payload, err := cursor.take(end - 1)
	if err != nil || interruption(cursor.ctx) != "" {
		return nil, err
	}
	closing, err := cursor.take(end)
	if err != nil || interruption(cursor.ctx) != "" {
		return nil, err
	}
	label := Value{Segments: payload, Semantic: Some(literal)}
	destination := label
	if email {
		destination = Value{Segments: append([]Segment{{Transformation: Synthetic, Semantic: Some("mailto:")}}, payload...), Semantic: Some("mailto:" + literal)}
	}
	child, err := NewNode(NodeSpec{Kind: Text, Envelope: contentEnvelope(payload), Content: payload, Properties: Properties{Literal: label.Semantic}})
	if err != nil || interruption(cursor.ctx) != "" {
		return nil, err
	}
	envelope := contentEnvelope(opening)
	envelope.end = contentEnvelope(closing).end
	return NewNode(NodeSpec{Kind: Link, Envelope: envelope, Content: payload,
		Markers:    []Marker{{Role: "destination-open", Source: contentEnvelope(opening)}, {Role: "destination-close", Source: contentEnvelope(closing)}},
		Properties: Properties{Link: &LinkProperties{Origin: "autolink", Label: label, Destination: destination, Resolution: Available}}, Children: []*Node{child}})
}
