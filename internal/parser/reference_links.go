package parser

import "unicode/utf8"

type referenceLinkSyntax struct {
	close, end, keyStart, keyEnd int
	origin, normalized           string
	definition                   *ReferenceDefinition // borrowed from the completed registry
}

// referenceLabelEnd bounds work before full folding and uses the same raw label
// grammar/counting as definitions. Escaped punctuation remains two characters;
// CRLF is one preprocessed ending while all offsets still address original bytes.
func (s *inlineScanner) referenceLabelEnd(start int) (int, bool) {
	characters, nonblank := 0, false
	for i := start; i < len(s.text); {
		if interruption(s.ctx) != "" || s.text[i] == '[' {
			return 0, false
		}
		if s.text[i] == ']' {
			return i, nonblank
		}
		r, width := utf8.DecodeRuneInString(s.text[i:])
		if r == '\r' && i+1 < len(s.text) && s.text[i+1] == '\n' {
			width = 2
		}
		characters++
		if r != ' ' && r != '\t' && r != '\r' && r != '\n' {
			nonblank = true
		}
		if escapedReferenceByte(s.text, i) {
			i++
			characters++
		}
		i += width
		if characters > 999 {
			return 0, false
		}
	}
	return 0, false
}

func (s *inlineScanner) referenceLink(open, close int) (referenceLinkSyntax, bool) {
	r := referenceLinkSyntax{close: close, end: close + 1, origin: "shortcut", keyStart: open + 1, keyEnd: close}
	if s.references == nil || s.references.LookupAvailability != Available || len(s.references.Lookup) == 0 {
		return r, false
	}
	// A syntactically valid following identifier precludes shortcut fallback even
	// when its lookup misses. Invalid following syntax leaves shortcut eligible.
	if close+1 < len(s.text) && s.text[close+1] == '[' {
		if close+2 < len(s.text) && s.text[close+2] == ']' {
			r.origin, r.end = "collapsed", close+3
		} else if end, valid := s.referenceLabelEnd(close + 2); valid {
			r.origin, r.end, r.keyStart, r.keyEnd = "full", end+1, close+2, end
		}
	}
	if r.origin != "full" {
		if end, valid := s.referenceLabelEnd(open + 1); !valid || end != close {
			return r, false
		}
	}
	if interruption(s.ctx) != "" {
		return r, false
	}
	r.normalized = normalizeReferenceLabel(s.text[r.keyStart:r.keyEnd])
	if interruption(s.ctx) != "" {
		return r, false
	}
	i, found := s.references.Lookup[r.normalized]
	if !found {
		return r, false
	}
	r.definition = s.references.Definitions[i]
	return r, true
}

func projectReference(v Value) Value {
	return Value{Semantic: v.Semantic, Segments: []Segment{{Transformation: Synthetic, Semantic: v.Semantic}}}
}

func (s *inlineScanner) buildReferenceLink(content emphasisContent, bracket inlineBracket, syntax referenceLinkSyntax, flat []*Node) ([]*Node, *Node, error) {
	label, err := content.linkValue(bracket.offset+1, syntax.close, false)
	if err != nil || interruption(s.ctx) != "" {
		return nil, nil, err
	}
	d := syntax.definition.spec
	p := &LinkProperties{Origin: syntax.origin, Label: label, Destination: projectReference(d.Destination), NormalizedLabel: Some(syntax.normalized), Resolution: Available}
	if d.Title != nil {
		title := projectReference(*d.Title)
		p.Title = &title
	}
	positions := []struct {
		role string
		at   int
	}{{"label-open", bracket.offset}, {"label-close", syntax.close}}
	if syntax.origin != "shortcut" {
		key, err := content.linkValue(syntax.close+2, syntax.end-1, false)
		if err != nil || interruption(s.ctx) != "" {
			return nil, nil, err
		}
		p.ReferenceLabel = &key
		positions = append(positions, struct {
			role string
			at   int
		}{"reference-open", syntax.close + 1}, struct {
			role string
			at   int
		}{"reference-close", syntax.end - 1})
	}
	markers := make([]Marker, 0, len(positions))
	for _, position := range positions {
		parts, err := content.slice(position.at, position.at+1)
		if err != nil || interruption(s.ctx) != "" {
			return nil, nil, err
		}
		markers = append(markers, Marker{Role: position.role, Source: contentEnvelope(parts)})
	}
	return s.finishLink(bracket, label, p, markers, flat)
}
