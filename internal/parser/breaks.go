package parser

// scanBreak consumes original marker/ending contributions from the same advancing
// cursor as Text and CodeSpan. Block producers already omit terminal endings and
// continuation indentation. Content owns only the ending; the optional hard-break
// marker owns its complete original spelling, never a decoded character.
func scanBreak(cursor *inlineContent, ending, end int, origin string) (*Node, error) {
	var markers []Marker
	kind := SoftBreak
	if origin != "line-ending" {
		marker, err := cursor.take(ending)
		if err != nil || interruption(cursor.ctx) != "" {
			return nil, err
		}
		markers = []Marker{{Role: "hard-break-marker", Source: contentEnvelope(marker)}}
		kind = HardBreak
	}
	content, err := cursor.take(end)
	if err != nil || interruption(cursor.ctx) != "" {
		return nil, err
	}
	envelope := contentEnvelope(content)
	if len(markers) != 0 {
		envelope.start = markers[0].Source.start
	}
	return NewNode(NodeSpec{Kind: kind, Envelope: envelope, Content: content, Markers: markers,
		Properties: Properties{Break: &BreakProperties{Origin: origin}}})
}
