package parser

import "slices"

// TaskProperties describe syntax on a ListItem and its first direct Paragraph.
// Paragraph identifies that child without a mutable node backpointer. Marker is
// an envelope: Segments, unlike Marker, exclude intervening container prefixes.
// Check and Separator each own one logical character (a CRLF owns both bytes).
// Checked is semantic state; CheckSpelling retains the exact original spelling.
// List marker and ancestor context belong to the existing list/container nodes.
type TaskProperties struct {
	Paragraph, Marker, Opening, Check, Closing, Separator Span
	Segments                                              []Span
	Checked                                               bool
	CheckSpelling                                         string
}

func taskWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

func taskToken(span Span) bool {
	if !span.Present() {
		return false
	}
	raw := span.source.content[span.Start():span.End()]
	if len(raw) == 1 {
		if raw[0] == '\n' && span.Start() > 0 && span.source.content[span.Start()-1] == '\r' ||
			raw[0] == '\r' && span.End() < span.source.Len() && span.source.content[span.End()] == '\n' {
			return false
		}
		return taskWhitespace(raw[0])
	}
	return raw == "\r\n"
}

func sameTask(a, b *TaskProperties) bool {
	return a != nil && b != nil && a.Paragraph == b.Paragraph && a.Marker == b.Marker &&
		a.Opening == b.Opening && a.Check == b.Check && a.Closing == b.Closing &&
		a.Separator == b.Separator && a.Checked == b.Checked &&
		a.CheckSpelling == b.CheckSpelling && slices.Equal(a.Segments, b.Segments)
}

func validateTaskSpec(s NodeSpec) error {
	t := s.Properties.Task
	if t == nil {
		if s.Kind == ListItem {
			for _, child := range s.Children {
				if child != nil && child.spec.Properties.Task != nil {
					return ErrModel
				}
			}
		}
		return nil
	}
	if s.Kind != Paragraph && s.Kind != ListItem || !contains(s.Envelope, t.Paragraph) ||
		!contains(t.Paragraph, t.Marker) || !contains(t.Paragraph, t.Separator) ||
		!contains(t.Marker, t.Opening) || !contains(t.Marker, t.Check) || !contains(t.Marker, t.Closing) {
		return ErrModel
	}
	raw := func(span Span) string { return span.source.content[span.Start():span.End()] }
	if raw(t.Opening) != "[" || raw(t.Closing) != "]" ||
		t.Opening.End() > t.Check.Start() || t.Check.End() > t.Closing.Start() ||
		t.Marker.Start() != t.Opening.Start() || t.Marker.End() != t.Closing.End() ||
		t.Separator.Start() != t.Closing.End() || !taskToken(t.Separator) ||
		t.CheckSpelling != raw(t.Check) || t.Checked != (t.CheckSpelling == "x" || t.CheckSpelling == "X") ||
		(!t.Checked && !taskToken(t.Check)) {
		return ErrModel
	}
	// Canonical original contributions are the three tokens, coalescing adjacent
	// spans. Fixed-size validation never scans a potentially large raw gap.
	expected := [3]Span{t.Opening, t.Check, t.Closing}
	n := 3
	if t.Opening.End() == t.Check.Start() {
		expected[0].end = t.Check.End()
		expected[1] = t.Closing
		n = 2
		if t.Check.End() == t.Closing.Start() {
			expected[0].end = t.Closing.End()
			n = 1
		}
	} else if t.Check.End() == t.Closing.Start() {
		expected[1].end = t.Closing.End()
		n = 2
	}
	if !slices.Equal(t.Segments, expected[:n]) {
		return ErrModel
	}
	if s.Kind == Paragraph {
		if s.Envelope != t.Paragraph {
			return ErrModel
		}
		for _, part := range s.Content {
			if part.Source.Present() && part.Source.Start() < t.Separator.End() {
				return ErrModel
			}
		}
	} else {
		if len(s.Children) == 0 || s.Children[0] == nil || s.Children[0].Kind() != Paragraph ||
			s.Children[0].Span() != t.Paragraph || !sameTask(t, s.Children[0].spec.Properties.Task) {
			return ErrModel
		}
		for _, child := range s.Children[1:] {
			if child != nil && child.spec.Properties.Task != nil {
				return ErrModel
			}
		}
	}
	return nil
}

// recognizeTask runs only after core block/reference classification proves the
// first direct child is a Paragraph. Read at most four logical original tokens;
// later container prefixes are gaps in the paragraph's existing source mapping.
// The final untrimmed source span supplies a terminal separator omitted from
// Content. No suffix is reparsed into blocks, references or a second inline AST.
func (p *blockParser) recognizeTask(s *NodeSpec, block openBlock) {
	meterWork(p.ctx, 1)
	parts := block.content
	partIndex, offset := 0, 0
	meterRecords(p.ctx, 1) // provisional untrimmed final-line mapping
	last := p.index.span(block.last.contentStart, block.last.next)
	next := func() Span {
		for partIndex <= len(parts) {
			meterWork(p.ctx, 1)
			part := last
			if partIndex < len(parts) {
				part = parts[partIndex].Source
			}
			if offset == 0 {
				offset = part.Start()
			}
			if offset < part.End() {
				start := offset
				offset++
				if p.index.source.content[start] == '\r' && offset < part.End() && p.index.source.content[offset] == '\n' {
					offset++
				}
				meterRecords(p.ctx, 1) // provisional token, including rejected candidates
				return p.index.span(start, offset)
			}
			partIndex++
			offset = 0
		}
		return Span{}
	}
	opening := next()
	if !opening.Present() || opening.End()-opening.Start() != 1 || p.index.source.content[opening.Start()] != '[' {
		return
	}
	check := next()
	if !check.Present() {
		return
	}
	spelling := p.index.source.content[check.Start():check.End()]
	checked := spelling == "x" || spelling == "X"
	if !checked && !taskToken(check) {
		return
	}
	closing := next()
	if !closing.Present() || closing.End()-closing.Start() != 1 || p.index.source.content[closing.Start()] != ']' {
		return
	}
	separator := next()
	if !taskToken(separator) || separator.Start() != closing.End() {
		return
	}
	meterRecords(p.ctx, 7)
	t := &TaskProperties{Paragraph: s.Envelope, Marker: p.index.span(opening.Start(), closing.End()),
		Opening: opening, Check: check, Closing: closing, Separator: separator, Checked: checked, CheckSpelling: spelling}
	for _, part := range [...]Span{opening, check, closing} {
		if n := len(t.Segments); n > 0 && t.Segments[n-1].End() == part.Start() {
			t.Segments[n-1].end = part.End()
		} else {
			t.Segments = meteredAppend(p.ctx, t.Segments, part)
		}
	}
	var content []Segment
	for _, part := range s.Content {
		meterWork(p.ctx, 1)
		if interruption(p.ctx) != "" {
			return
		}
		if part.Source.End() <= separator.End() {
			continue
		}
		if part.Source.Start() < separator.End() {
			// All recognized tokens are original bytes. A split can only fall
			// inside an Exact segment, never inside a normalized contribution.
			start := separator.End()
			part.Source.start = start
			part.Semantic = Some(p.index.source.content[start:part.Source.End()])
		}
		content = meteredAppend(p.ctx, content, part)
	}
	s.Properties.Task, s.Content = t, content
}
