package parser

// validateStrikeSpec checks supplied source facts, not flanking or recognition.
// Content is the logical original payload: gaps from containers remain allowed.
// Only the fixed-width markers are inspected; a large gapped envelope is never
// scanned merely to validate one or two marker bytes.
func validateStrikeSpec(spec NodeSpec) error {
	if spec.Kind != Strike {
		return nil
	}
	p := spec.Properties.Delimiter
	if p == nil || p.Marker != '~' || p.Width < 1 || p.Width > 2 || spec.Properties.Literal.Present || len(spec.Markers) != 2 {
		return ErrModel
	}
	open, close := spec.Markers[0], spec.Markers[1]
	if open.Role != "opening" || close.Role != "closing" || open.Source.Start() != spec.Envelope.Start() || close.Source.End() != spec.Envelope.End() || open.Source.End() > close.Source.Start() {
		return ErrModel
	}
	for _, marker := range spec.Markers {
		if marker.Source.End()-marker.Source.Start() != p.Width {
			return ErrModel
		}
		text, err := spec.Envelope.source.Text(marker.Source)
		if err != nil || text != "~" && text != "~~" {
			return ErrModel
		}
	}
	interior := spec.Envelope
	interior.start, interior.end = open.Source.End(), close.Source.Start()
	if interior.start != interior.end && (len(spec.Content) == 0 || spec.Content[0].Source.Start() != interior.start || spec.Content[len(spec.Content)-1].Source.End() != interior.end) {
		return ErrModel
	}
	previous := interior.start
	for _, part := range spec.Content {
		if !contains(interior, part.Source) || part.Source.Start() < previous {
			return ErrModel
		}
		previous = part.Source.End()
	}
	for _, child := range spec.Children {
		if child == nil || !contains(interior, child.Span()) {
			return ErrModel
		}
	}
	return nil
}
