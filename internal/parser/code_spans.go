package parser

import (
	"context"
	"strings"
)

// inlineContent borrows immutable logical Content. Its cursor moves forward
// through each original segment once, splitting only identity contributions.
// Nonidentity contributions (NUL, partial tabs and table pipe escapes) are atomic.
type inlineContent struct {
	ctx                   context.Context
	parts                 []Segment
	index, within, offset int
}

func (c *inlineContent) take(end int) ([]Segment, error) {
	meterWork(c.ctx, 1)
	var out []Segment
	for c.offset < end {
		meterWork(c.ctx, 1)
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
		out = meteredAppend(c.ctx, out, part)
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
	meterWork(s.ctx, 1)
	cursor := inlineContent{ctx: s.ctx, parts: parts}
	content := emphasisContent{scanner: s, parts: parts, starts: meteredSlice[int](s.ctx,

		len(parts)+1)}
	for i, part := range parts {
		meterWork(s.ctx, 1)
		if interruption(s.ctx) != "" {
			return nil, nil
		}
		content.starts[i+1] = content.starts[i] + len(part.Semantic.Value)
	}
	var children []*Node
	appendText := func(end, trim int) error {
		if end == cursor.offset {
			return nil
		}
		raw, err := cursor.take(end - trim)
		if err != nil || interruption(s.ctx) != "" {
			return err
		}
		value := decodeValue(s.ctx, raw, false)
		if trim != 0 {
			// Retain the soft break's discarded original space as an explicit
			// empty semantic contribution, separate from escape/entity tokens.
			spaces, err := cursor.take(end)
			if err != nil {
				return err
			}
			for i := range spaces {
				meterWork(s.ctx, 1)
				spaces[i].Transformation = Normalized
				spaces[i].Semantic = Some("")
			}
			raw = meteredAppend(s.ctx, raw, spaces...)
			value.Segments = meteredAppend(s.ctx, value.Segments, spaces...)
		}
		if interruption(s.ctx) != "" {
			return nil
		}
		node, err := meteredNode(s.ctx, NodeSpec{Kind: Text, Envelope: contentEnvelope(raw), Content: value.Segments,
			Properties: Properties{Literal: value.Semantic}})
		if err == nil {
			children = meteredAppend(s.ctx, children, node)
		}
		return err
	}
	for offset, checkpoint := 0, 0; offset < len(s.text); {
		meterWork(s.ctx, 1)
		if offset >= checkpoint {
			if interruption(s.ctx) != "" {
				return nil, nil
			}
			checkpoint = offset + 256
		}
		start, ending, origin, trim := offset, offset, "line-ending", 0
		switch s.text[offset] {
		case ' ':
			for ending < len(s.text) && s.text[ending] == ' ' {
				meterWork(s.ctx, 1)
				if (ending-start)%256 == 0 && interruption(s.ctx) != "" {
					return nil, nil
				}
				ending++
			}
			if ending-start >= 2 {
				origin = "spaces"
			} else {
				trim = 1
			}
		case '\\':
			ending++
			origin = "backslash"
		}
		if ending < len(s.text) && referenceEnding(s.text[ending]) {
			textEnd := start
			if trim != 0 {
				textEnd = ending
			}
			if err := appendText(textEnd, trim); err != nil || interruption(s.ctx) != "" {
				return nil, err
			}
			end := ending + 1
			if s.text[ending] == '\r' && end < len(s.text) && s.text[end] == '\n' {
				end++
			}
			node, err := scanBreak(&cursor, ending, end, origin)
			if err != nil || interruption(s.ctx) != "" {
				return nil, err
			}
			children = meteredAppend(s.ctx, children, node)
			offset = end
			continue
		}
		if s.text[offset] == ' ' {
			offset = ending // inspect each ordinary whitespace run only once
			continue
		}
		if s.text[offset] == '<' {
			if end, email := s.autolinkEnd(offset); end != 0 {
				if err := appendText(offset, 0); err != nil || interruption(s.ctx) != "" {
					return nil, err
				}
				node, err := scanAutolink(&cursor, offset, end, s.text[offset+1:end-1], email)
				if err != nil || interruption(s.ctx) != "" {
					return nil, err
				}
				children = meteredAppend(s.ctx, children, node)
				s.deactivateLinks(offset)
				offset = end
				continue
			}
			if end, closing := s.inlineHTMLEnd(offset); end != 0 {
				if err := appendText(offset, 0); err != nil || interruption(s.ctx) != "" {
					return nil, err
				}
				node, err := scanInlineHTML(&cursor, offset, end, closing)
				if err != nil || interruption(s.ctx) != "" {
					return nil, err
				}
				children = meteredAppend(s.ctx, children, node)
				offset = end
				continue
			}
		}
		if s.text[offset] == '<' && s.extendedEnabled() {
			if s.failedAngles == nil {
				s.failedAngles = meteredSlice[bool](s.ctx, len(s.text))
			}
			s.failedAngles[offset] = true
		}
		if s.text[offset] == '[' || s.text[offset] == '!' && offset+1 < len(s.text) && s.text[offset+1] == '[' {
			image := s.text[offset] == '!'
			if image {
				offset++
			}
			s.openBracket(offset, len(children), image)
			offset++
			continue
		}
		if s.text[offset] == ']' && len(s.brackets) != 0 {
			bracket := s.brackets[len(s.brackets)-1]
			s.brackets = s.brackets[:len(s.brackets)-1]
			if bracket.image || bracket.offset >= s.inactiveBefore {
				if syntax, valid := s.inlineLink(offset); valid {
					if err := appendText(offset, 0); err != nil || interruption(s.ctx) != "" {
						return nil, err
					}
					before, link, err := s.buildLink(content, bracket, syntax, children[bracket.node:])
					if err != nil || interruption(s.ctx) != "" {
						return nil, err
					}
					children = meteredAppend(s.ctx, children[:bracket.node], before...)
					children = meteredAppend(s.ctx, children, link)
					if _, err := cursor.take(syntax.end); err != nil || interruption(s.ctx) != "" {
						return nil, err
					}
					if !bracket.image {
						s.deactivateLinks(bracket.offset)
					}
					offset = syntax.end
					continue
				}
				if syntax, valid := s.referenceLink(bracket.offset, offset); valid {
					if err := appendText(offset, 0); err != nil || interruption(s.ctx) != "" {
						return nil, err
					}
					before, link, err := s.buildReferenceLink(content, bracket, syntax, children[bracket.node:])
					if err != nil || interruption(s.ctx) != "" {
						return nil, err
					}
					children = meteredAppend(s.ctx, children[:bracket.node], before...)
					children = meteredAppend(s.ctx, children, link)
					if _, err := cursor.take(syntax.end); err != nil || interruption(s.ctx) != "" {
						return nil, err
					}
					if !bracket.image {
						s.deactivateLinks(bracket.offset)
					}
					offset = syntax.end
					continue
				}
			}
			offset++
			continue
		}
		if s.text[offset] != '`' {
			offset = s.dispatch(offset)
			continue
		}
		end := offset + 1
		for end < len(s.text) && s.text[end] == '`' {
			meterWork(s.ctx, 1)
			if (end-offset)%256 == 0 && interruption(s.ctx) != "" {
				return nil, nil
			}
			end++
		}
		width := end - offset
		runs := s.backticks[width]
		next := s.backtickNext[width]
		for next < len(runs) && runs[next] < end {
			meterWork(s.ctx, 1)
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
		if err := appendText(offset, 0); err != nil || interruption(s.ctx) != "" {
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
		node, err := meteredNode(s.ctx, NodeSpec{Kind: CodeSpan, Envelope: envelope, Content: value.Segments,
			Markers:    []Marker{{Role: "opening", Source: contentEnvelope(opening)}, {Role: "closing", Source: contentEnvelope(closing)}},
			Properties: Properties{Literal: value.Semantic, Delimiter: &DelimiterProperties{Marker: '`', Width: width}}})
		if err != nil {
			return nil, err
		}
		children = meteredAppend(s.ctx, children, node)
		offset = close + width
	}
	if err := appendText(len(s.text), 0); err != nil {
		return nil, err
	}
	return children, nil
}

// normalizeCodeSpan owns a private segment slice. Raw payload ranges, including
// trimmed bytes, survive; only their semantic contributions change. Existing NUL
// and partial-tab provenance comes from the block scanner, never text decoding.
func normalizeCodeSpan(ctx context.Context, parts []Segment) Value {
	meterWork(ctx, 1)
	var whole strings.Builder
	for i := range parts {
		meterWork(ctx, 1)
		if interruption(ctx) != "" {
			return Value{}
		}
		part := &parts[i]
		text := part.Semantic.Value
		var normalized strings.Builder
		for offset, checkpoint := 0, 0; offset < len(text); offset++ {
			meterWork(ctx, 1)
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
			meteredWriteByte(ctx, &normalized, c)
		}
		value := normalized.String()
		if value != text {
			part.Transformation = Normalized
			part.Semantic = Some(value)
		}
		meteredWriteString(ctx, &whole, value)
	}
	literal := whole.String()
	allSpaces := true
	for offset := 0; offset < len(literal); offset++ {
		meterWork(ctx, 1)
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
			meterWork(ctx, 1)
			for j := 0; j < len(parts); j++ {
				meterWork(ctx, 1)
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
