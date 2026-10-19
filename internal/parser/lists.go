package parser

import "strings"

// listStart establishes only the marker and first-line indentation. It does not
// establish item continuation/closure (#15), grouping or tightness (#16).
// All cursors borrow the original source and preserve its physical tab phase.
type listStart struct {
	line                   blockLine
	marker, after, content Indentation
	properties             ListProperties
	prefixSegments         int
}

// classifyListStart is shared by ordinary dispatch and quote interruption. It
// scans original bytes without materializing the content suffix or full padding.
// Setext and thematic-break precedence belongs to the caller.
func classifyListStart(line blockLine, paragraphOpen bool) (listStart, bool) {
	if line.indent >= 4 {
		return listStart{}, false
	}
	c := line.cursor
	text := c.index.source.content
	prefixSegments := 1 // The marker is one Exact contribution.
	previous := byte(0)
	// logicalLine has already counted at most three leading columns here.
	for c.offset < line.contentStart {
		if text[c.offset] != ' ' || previous != ' ' {
			prefixSegments++
		}
		previous = text[c.offset]
		advanceListSpace(&c)
	}
	marker := c
	props := ListProperties{}
	switch line.first {
	case '-', '+', '*':
		props.Bullet = Some(line.first)
		_, _ = c.AdvanceMarker()
	default:
		start, digits := 0, 0
		for c.offset < c.end && text[c.offset] >= '0' && text[c.offset] <= '9' {
			digits++
			if digits > 9 {
				return listStart{}, false
			}
			start = start*10 + int(text[c.offset]-'0')
			_, _ = c.AdvanceMarker()
		}
		if digits == 0 || c.offset == c.end || text[c.offset] != '.' && text[c.offset] != ')' {
			return listStart{}, false
		}
		props.Ordered, props.Start, props.Delimiter = true, Some(start), Some(text[c.offset])
		_, _ = c.AdvanceMarker()
	}
	if c.offset < c.end && text[c.offset] != ' ' && text[c.offset] != '\t' {
		return listStart{}, false
	}
	after, content := c, c
	paddingSegments := 0
	previous = 0
	for content.offset < content.end && (text[content.offset] == ' ' || text[content.offset] == '\t') {
		if text[content.offset] != ' ' || previous != ' ' {
			paddingSegments++
		}
		previous = text[content.offset]
		advanceListSpace(&content)
	}
	empty := content.offset == content.end
	if paragraphOpen && (empty || props.Ordered && props.Start.Value != 1) {
		return listStart{}, false
	}
	padding := content.column - after.column
	if empty {
		// Rule 3: effective padding is one, independently of actual trailing
		// whitespace. Keep all those bytes in Prefix, with no invented space.
		padding = 1
	} else if padding > 4 {
		// Rule 2: consume just one column. The residual indentation remains
		// content, possibly sharing the original tab with the prefix.
		padding = 1
		content = after
		_, _ = content.Consume(1)
		paddingSegments = 1
	}
	width := after.column - marker.column
	props.MarkerWidth = Some(width)
	props.Padding = Some(padding)
	props.ContentIndent = Some(marker.column - line.cursor.column + width + padding)
	return listStart{line: line, marker: marker, after: after, content: content, properties: props,
		prefixSegments: prefixSegments + paddingSegments}, true
}

// advanceListSpace consumes one original whitespace byte without allocating
// semantic text. A pending tab contributes only its unconsumed columns.
func advanceListSpace(c *Indentation) {
	if c.index.source.content[c.offset] == '\t' {
		if c.tabRemaining != 0 {
			c.column += c.tabRemaining
			c.tabRemaining = 0
		} else {
			c.column += 4 - c.column%4
		}
	} else {
		c.column++
	}
	c.offset++
}

// firstLine freezes a bounded item prefix, not a completed list/item container.
// Its content is uninterpreted first-line text; no paragraph/inline or list
// grouping facts are fabricated. The caller stops block processing afterward.
func (s listStart) firstLine() (*Node, error) {
	c := s.line.cursor
	// Classification already counted the accepted prefix contributions. Build
	// them once in exact private storage, without a second padding Segment buffer
	// or Consume's unused aggregate padding string. NewNode still snapshots them.
	prefix := Value{Segments: make([]Segment, 0, s.prefixSegments)}
	var text strings.Builder
	text.Grow(s.content.column - c.column)
	if err := appendListPrefixIndentation(&prefix, &text, &c, s.marker.column); err != nil {
		return nil, err
	}
	markerSpan := c.index.span(s.marker.offset, s.after.offset)
	markerText := c.index.source.content[markerSpan.start:markerSpan.end]
	prefix.Segments = append(prefix.Segments, Segment{Source: markerSpan, Transformation: Exact,
		Semantic: Some(markerText)})
	text.WriteString(markerText)
	c = s.after
	if err := appendListPrefixIndentation(&prefix, &text, &c, s.content.column); err != nil {
		return nil, err
	}
	prefix.Semantic = Some(text.String())
	s.properties.Prefix = &prefix
	prefixEnd := prefix.Segments[len(prefix.Segments)-1].Source.End()
	return NewNode(NodeSpec{
		Kind: ListItem, Envelope: c.index.span(s.line.start, s.line.next),
		Markers: []Marker{
			{Role: "item-prefix", Source: c.index.span(s.line.start, prefixEnd)},
			{Role: "item-marker", Source: markerSpan},
		},
		Content: s.content.Remaining().Segments, Properties: Properties{List: &s.properties},
	})
}

// appendListPrefixIndentation writes previously classified whitespace directly
// into the private prefix. Space runs borrow Source; tab contributions borrow
// static spaces. A partly consumed tab retains its original one-byte span.
func appendListPrefixIndentation(prefix *Value, text *strings.Builder, c *Indentation, endColumn int) error {
	for c.column < endColumn && c.offset < c.end {
		start := c.offset
		segment := Segment{}
		switch c.index.source.content[start] {
		case ' ':
			for c.column < endColumn && c.offset < c.end && c.index.source.content[c.offset] == ' ' {
				c.offset++
				c.column++
			}
			segment = Segment{Source: c.index.span(start, c.offset), Transformation: Exact,
				Semantic: Some(c.index.source.content[start:c.offset])}
		case '\t':
			if c.tabRemaining == 0 {
				c.tabRemaining = 4 - c.column%4
			}
			count := min(c.tabRemaining, endColumn-c.column)
			segment = Segment{Source: c.index.span(start, start+1), Transformation: PartialTab,
				Semantic: Some("    "[:count])}
			c.column += count
			c.tabRemaining -= count
			if c.tabRemaining == 0 {
				c.offset++
			}
		default:
			return ErrIndentation
		}
		prefix.Segments = append(prefix.Segments, segment)
		text.WriteString(segment.Semantic.Value)
	}
	if c.column != endColumn {
		return ErrIndentation
	}
	return nil
}
