package parser

import (
	"context"
	"strings"
)

// inlineContent borrows immutable logical Content. Its cursor moves forward
// through each original segment once, splitting only identity contributions.
// Nonidentity contributions (NUL/partial tabs) cannot contain a backtick boundary.
type inlineContent struct {
	ctx                   context.Context
	parts                 []Segment
	index, within, offset int
}

func (c *inlineContent) take(end int) ([]Segment, error) {
	var out []Segment
	for c.offset < end {
		if interruption(c.ctx) != "" {
			return nil, nil
		}
		if c.index >= len(c.parts) {
			return nil, ErrModel
		}
		part := c.parts[c.index]
		width := min(end-c.offset, len(part.Semantic.Value)-c.within)
		if part.Transformation == Exact {
			part.Source.start += c.within
			part.Source.end = part.Source.start + width
			part.Semantic = Some(part.Semantic.Value[c.within : c.within+width])
		} else if c.within != 0 || width != len(part.Semantic.Value) {
			return nil, ErrModel
		}
		out = append(out, part)
		c.offset += width
		c.within += width
		if c.within == len(c.parts[c.index].Semantic.Value) {
			c.index++
			c.within = 0
		}
	}
	return out, nil
}

func contentEnvelope(parts []Segment) Span {
	first, last := parts[0].Source, parts[len(parts)-1].Source
	return Span{source: first.source, start: first.start, end: last.end, present: true}
}

func (s *inlineScanner) scan(parts []Segment) ([]*Node, error) {
	cursor := inlineContent{ctx: s.ctx, parts: parts}
	var children []*Node
	appendText := func(end int) error {
		if end == cursor.offset {
			return nil
		}
		raw, err := cursor.take(end)
		if err != nil || interruption(s.ctx) != "" {
			return err
		}
		value := decodeValue(s.ctx, raw, false)
		if interruption(s.ctx) != "" {
			return nil
		}
		node, err := NewNode(NodeSpec{Kind: Text, Envelope: contentEnvelope(raw), Content: value.Segments,
			Properties: Properties{Literal: value.Semantic}})
		if err == nil {
			children = append(children, node)
		}
		return err
	}
	for offset, checkpoint := 0, 0; offset < len(s.text); {
		if offset >= checkpoint {
			if interruption(s.ctx) != "" {
				return nil, nil
			}
			checkpoint = offset + 256
		}
		if s.text[offset] != '`' {
			offset = s.dispatch(offset)
			continue
		}
		end := offset + 1
		for end < len(s.text) && s.text[end] == '`' {
			if (end-offset)%256 == 0 && interruption(s.ctx) != "" {
				return nil, nil
			}
			end++
		}
		width := end - offset
		runs := s.backticks[width]
		next := s.backtickNext[width]
		for next < len(runs) && runs[next] < end {
			if next%256 == 0 && interruption(s.ctx) != "" {
				return nil, nil
			}
			next++
		}
		if s.backtickNext != nil {
			s.backtickNext[width] = next
		}
		if next == len(runs) {
			offset = end // unmatched run stays in the surrounding Text
			continue
		}
		close := runs[next]
		if err := appendText(offset); err != nil || interruption(s.ctx) != "" {
			return nil, err
		}
		opening, err := cursor.take(end)
		if err != nil || interruption(s.ctx) != "" {
			return nil, err
		}
		payload, err := cursor.take(close)
		if err != nil || interruption(s.ctx) != "" {
			return nil, err
		}
		closing, err := cursor.take(close + width)
		if err != nil || interruption(s.ctx) != "" {
			return nil, err
		}
		value := normalizeCodeSpan(s.ctx, payload)
		if interruption(s.ctx) != "" {
			return nil, nil
		}
		envelope := contentEnvelope(opening)
		envelope.end = contentEnvelope(closing).end
		node, err := NewNode(NodeSpec{Kind: CodeSpan, Envelope: envelope, Content: value.Segments,
			Markers:    []Marker{{Role: "opening", Source: contentEnvelope(opening)}, {Role: "closing", Source: contentEnvelope(closing)}},
			Properties: Properties{Literal: value.Semantic, Delimiter: &DelimiterProperties{Marker: '`', Width: width}}})
		if err != nil {
			return nil, err
		}
		children = append(children, node)
		offset = close + width
	}
	if err := appendText(len(s.text)); err != nil {
		return nil, err
	}
	return children, nil
}

// normalizeCodeSpan owns a private segment slice. Raw payload ranges, including
// trimmed bytes, survive; only their semantic contributions change. Existing NUL
// and partial-tab provenance comes from the block scanner, never text decoding.
func normalizeCodeSpan(ctx context.Context, parts []Segment) Value {
	var whole strings.Builder
	for i := range parts {
		if interruption(ctx) != "" {
			return Value{}
		}
		part := &parts[i]
		text := part.Semantic.Value
		var normalized strings.Builder
		for offset, checkpoint := 0, 0; offset < len(text); offset++ {
			if offset >= checkpoint {
				if interruption(ctx) != "" {
					return Value{}
				}
				checkpoint = offset + 256
			}
			c := text[offset]
			if c == '\r' {
				if offset+1 < len(text) && text[offset+1] == '\n' {
					offset++
				}
				c = ' '
			} else if c == '\n' {
				c = ' '
			}
			normalized.WriteByte(c)
		}
		value := normalized.String()
		if value != text {
			part.Transformation = Normalized
			part.Semantic = Some(value)
		}
		whole.WriteString(value)
	}
	literal := whole.String()
	allSpaces := true
	for offset := 0; offset < len(literal); offset++ {
		if offset%256 == 0 && interruption(ctx) != "" {
			return Value{}
		}
		if literal[offset] != ' ' {
			allSpaces = false
			break
		}
	}
	if len(literal) >= 2 && literal[0] == ' ' && literal[len(literal)-1] == ' ' && !allSpaces {
		// Each boundary is one ASCII semantic byte; keep its entire original
		// source range even when it contributes an empty string after trimming.
		for _, first := range []bool{true, false} {
			for j := 0; j < len(parts); j++ {
				if j%256 == 0 && interruption(ctx) != "" {
					return Value{}
				}
				i := j
				if !first {
					i = len(parts) - 1 - j
				}
				part := &parts[i]
				text := part.Semantic.Value
				if text == "" {
					continue
				}
				if first {
					part.Semantic = Some(text[1:])
				} else {
					part.Semantic = Some(text[:len(text)-1])
				}
				if part.Transformation != PartialTab {
					part.Transformation = Normalized
				}
				break
			}
		}
		literal = literal[1 : len(literal)-1]
	}
	return Value{Segments: parts, Semantic: Some(literal)}
}
