package parser

import "strings"

// All offsets refer to the original logical block, before escape/entity decoding.
// Outer parentheses and optional component delimiters have distinct source facts.
type inlineLinkSyntax struct {
	close, end                       int
	destinationStart, destinationEnd int
	titleStart, titleEnd             int
	angle, title                     bool
}

// bareLinkEnd indexes raw lexical tokens once, then answers overlapping candidate
// queries without rescanning their suffixes. A token is one byte or an escaped
// punctuation pair. Candidate starts follow an unescaped ]( and whitespace, so
// they cannot land inside an escape token, even when other input is opaque.
//
// Backward composition copies the next token's endpoint. A closing parenthesis
// stops an unbalanced destination; a matched opening jumps over its whole balanced
// group. Whitespace/control bytes stop a destination and discard unmatched groups.
// Each token is visited twice and each closer is pushed/popped at most once. No
// nesting limit or speculative skip of later candidates is needed.
func (s *inlineScanner) bareLinkEnd(start int) (int, bool) {
	if s.bareLinkEnds == nil {
		text := s.text
		ends := make([]int, len(text)+1)
		ends[len(text)] = len(text)
		for i := 0; i < len(text); {
			if interruption(s.ctx) != "" {
				return 0, false
			}
			width := 1
			if escapedReferenceByte(text, i) {
				width = 2
			}
			ends[i] = i + width
			i += width
		}
		var closers []int
		for i := len(text) - 1; i >= 0; i-- {
			if interruption(s.ctx) != "" {
				return 0, false
			}
			next := ends[i]
			if next == 0 {
				continue // second byte of an escape token
			}
			c := text[i]
			switch {
			case next == i+2:
				ends[i] = ends[next]
			case c <= ' ' || c == 127:
				ends[i] = i
				closers = closers[:0]
			case c == ')':
				ends[i] = i
				closers = append(closers, i)
			case c == '(':
				ends[i] = -1
				if len(closers) != 0 {
					closing := closers[len(closers)-1]
					closers = closers[:len(closers)-1]
					ends[i] = ends[closing+1]
				}
			default:
				ends[i] = ends[next]
			}
		}
		// Do not retain a partially built index when caller interruption fires.
		s.bareLinkEnds = ends
	}
	end := s.bareLinkEnds[start]
	return end, end >= 0
}

// Components permit spaces/tabs and at most one physical ending between them.
// Advancing by two for CRLF must not skip interruption checkpoints.
func (s *inlineScanner) linkWhitespace(i int) int {
	ending := false
	for i < len(s.text) {
		if interruption(s.ctx) != "" {
			return len(s.text)
		}
		if referenceSpace(s.text[i]) {
			i++
		} else if !ending && referenceEnding(s.text[i]) {
			i = skipReferenceEnding(s.text, i)
			ending = true
		} else {
			break
		}
	}
	return i
}

func (s *inlineScanner) inlineLink(close int) (inlineLinkSyntax, bool) {
	r := inlineLinkSyntax{close: close}
	text := s.text
	if close+1 >= len(text) || text[close+1] != '(' {
		return r, false
	}
	i := s.linkWhitespace(close + 2)
	if i == len(text) {
		return r, false
	}
	r.angle = text[i] == '<'
	if r.angle {
		i++
	}
	r.destinationStart = i
	if r.angle {
		for i < len(text) {
			if interruption(s.ctx) != "" {
				return r, false
			}
			c := text[i]
			if escapedReferenceByte(text, i) {
				i += 2
				continue
			}
			if c == '>' {
				break
			}
			if c == '<' || referenceEnding(c) {
				return r, false
			}
			i++
		}
		r.destinationEnd = i
		if i == len(text) || text[i] != '>' {
			return r, false
		}
		i++
	} else {
		end, valid := s.bareLinkEnd(i)
		if !valid {
			return r, false
		}
		i, r.destinationEnd = end, end
	}
	endDestination := i
	i = s.linkWhitespace(i)
	if i > endDestination && i < len(text) && (text[i] == '"' || text[i] == '\'' || text[i] == '(') {
		open, closeTitle := text[i], text[i]
		if open == '(' {
			closeTitle = ')'
		}
		r.title, r.titleStart = true, i+1
		blank := false
		for i++; i < len(text) && text[i] != closeTitle; {
			if interruption(s.ctx) != "" {
				return r, false
			}
			if escapedReferenceByte(text, i) {
				i += 2
				blank = false
				continue
			}
			if open == '(' && text[i] == '(' {
				return r, false
			}
			if referenceEnding(text[i]) {
				if blank {
					return r, false
				}
				blank = true
				i = skipReferenceEnding(text, i)
				continue
			}
			if !referenceSpace(text[i]) {
				blank = false
			}
			i++
		}
		if i == len(text) {
			return r, false
		}
		r.titleEnd = i
		i = s.linkWhitespace(i + 1)
	}
	if i >= len(text) || text[i] != ')' {
		return r, false
	}
	r.end = i + 1
	return r, true
}

func (s *inlineScanner) openBracket(offset, node int, image bool) {
	bottom := len(s.delimiters) - 1
	if s.delimitersLinked {
		bottom = s.delimiterTail
	}
	s.brackets = append(s.brackets, inlineBracket{offset: offset, node: node, bottom: bottom, image: image})
}

// Successful inline links/autolinks invalidate earlier link openers. A monotonic
// source boundary avoids repeatedly revisiting an already inactive prefix.
func (s *inlineScanner) deactivateLinks(offset int) {
	s.inactiveBefore = max(s.inactiveBefore, offset)
}

// Empty components retain an exact zero-width range at the following delimiter.
// Indexed logical slicing never uses a decoded semantic offset as a source byte.
func (c emphasisContent) linkValue(start, end int, decode bool) (Value, error) {
	stop := end
	if start == end {
		stop++
	}
	parts, err := c.slice(start, stop)
	if err != nil || interruption(c.scanner.ctx) != "" {
		return Value{}, err
	}
	if start == end {
		parts[0].Source.end = parts[0].Source.start
		parts[0].Semantic = Some("")
	}
	if decode {
		return decodeValue(c.scanner.ctx, parts, true), nil
	}
	var text strings.Builder
	for _, part := range parts {
		if interruption(c.scanner.ctx) != "" {
			return Value{}, nil
		}
		text.WriteString(part.Semantic.Value)
	}
	return Value{Segments: parts, Semantic: Some(text.String())}, nil
}

func (s *inlineScanner) buildLink(content emphasisContent, bracket inlineBracket, syntax inlineLinkSyntax, flat []*Node) (before []*Node, link *Node, err error) {
	marker := func(role string, offset int) (Marker, error) {
		parts, err := content.slice(offset, offset+1)
		if err != nil || interruption(s.ctx) != "" {
			return Marker{}, err
		}
		return Marker{Role: role, Source: contentEnvelope(parts)}, nil
	}
	var markers []Marker
	for _, m := range []struct {
		role   string
		offset int
	}{{"label-open", bracket.offset}, {"label-close", syntax.close}, {"link-open", syntax.close + 1}} {
		m, err := marker(m.role, m.offset)
		if err != nil || interruption(s.ctx) != "" {
			return nil, nil, err
		}
		markers = append(markers, m)
	}
	if syntax.angle {
		for _, m := range []struct {
			role   string
			offset int
		}{{"destination-open", syntax.destinationStart - 1}, {"destination-close", syntax.destinationEnd}} {
			m, err := marker(m.role, m.offset)
			if err != nil || interruption(s.ctx) != "" {
				return nil, nil, err
			}
			markers = append(markers, m)
		}
	}
	label, err := content.linkValue(bracket.offset+1, syntax.close, false)
	if err != nil || interruption(s.ctx) != "" {
		return nil, nil, err
	}
	destination, err := content.linkValue(syntax.destinationStart, syntax.destinationEnd, true)
	if err != nil || interruption(s.ctx) != "" {
		return nil, nil, err
	}
	properties := &LinkProperties{Origin: "inline", Resolution: Available, Label: label, Destination: destination}
	if syntax.title {
		title, err := content.linkValue(syntax.titleStart, syntax.titleEnd, true)
		if err != nil || interruption(s.ctx) != "" {
			return nil, nil, err
		}
		properties.Title = &title
		for _, m := range []struct {
			role   string
			offset int
		}{{"title-open", syntax.titleStart - 1}, {"title-close", syntax.titleEnd}} {
			m, err := marker(m.role, m.offset)
			if err != nil || interruption(s.ctx) != "" {
				return nil, nil, err
			}
			markers = append(markers, m)
		}
	}
	closing, err := marker("link-close", syntax.end-1)
	if err != nil || interruption(s.ctx) != "" {
		return nil, nil, err
	}
	markers = append(markers, closing)
	return s.finishLink(bracket, label, properties, markers, flat)
}

// Both link forms share the same label/emphasis boundary and immutable source
// construction. Definition provenance never enters this use-site envelope.
func (s *inlineScanner) finishLink(bracket inlineBracket, label Value, properties *LinkProperties, markers []Marker, flat []*Node) (before []*Node, link *Node, err error) {
	cursor := inlineNodes{scanner: s, nodes: flat}
	before, err = cursor.take(markers[0].Source.start)
	if err != nil || interruption(s.ctx) != "" {
		return nil, nil, err
	}
	if _, err = cursor.take(markers[0].Source.end); err != nil || interruption(s.ctx) != "" {
		return nil, nil, err
	}
	children, err := cursor.take(markers[1].Source.start)
	if err != nil || interruption(s.ctx) != "" {
		return nil, nil, err
	}
	children, err = s.emphasisAbove(label.Segments, children, bracket.bottom, bracket.offset+1)
	if err != nil || interruption(s.ctx) != "" {
		return nil, nil, err
	}
	envelope := markers[0].Source
	envelope.end = markers[len(markers)-1].Source.end
	link, err = NewNode(NodeSpec{Kind: Link, Envelope: envelope, Content: label.Segments, Markers: markers, Properties: Properties{Link: properties}, Children: children})
	return before, link, err
}
