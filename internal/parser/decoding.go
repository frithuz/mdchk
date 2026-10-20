package parser

import (
	"context"
	"strings"
	"unicode/utf8"
)

// decodeToken consumes original ASCII spelling once. In particular, neither an
// escaped ampersand nor entity-produced punctuation is rescanned as Markdown.
// Lookahead is bounded by the numeric grammar or the longest retained name.
func decodeTokenContext(ctx context.Context, text string) (string, int, Transformation) {
	meterWork(ctx, 1)
	if len(text) >= 2 && text[0] == '\\' && IsASCIIPunctuation(rune(text[1])) {
		return text[1:2], 2, Escape
	}
	if len(text) < 3 || text[0] != '&' {
		return "", 0, ""
	}
	if text[1] == '#' {
		i, base, limit := 2, 10, 7
		if i < len(text) && (text[i] == 'x' || text[i] == 'X') {
			i, base, limit = i+1, 16, 6
		}
		start, value := i, rune(0)
		for i < len(text) && i-start < limit {
			meterWork(ctx, 1)
			c := text[i]
			digit := -1
			switch {
			case c >= '0' && c <= '9':
				digit = int(c - '0')
			case base == 16 && c >= 'a' && c <= 'f':
				digit = int(c-'a') + 10
			case base == 16 && c >= 'A' && c <= 'F':
				digit = int(c-'A') + 10
			}
			if digit < 0 {
				break
			}
			value = value*rune(base) + rune(digit)
			i++
		}
		if i == start || i >= len(text) || text[i] != ';' {
			return "", 0, ""
		}
		if value == 0 || !utf8.ValidRune(value) {
			value = utf8.RuneError
		}
		return string(value), i + 1, Entity
	}
	for i := 1; i < len(text) && i < maxEntityToken; i++ {
		meterWork(ctx, 1)
		if text[i] == ';' {
			if value, ok := namedEntities[text[:i+1]]; ok {
				return value, i + 1, Entity
			}
			break
		}
		if !asciiLetter(text[i]) && (text[i] < '0' || text[i] > '9') {
			break
		}
	}
	return "", 0, ""
}

// decodeValue borrows parser-owned source segments and returns private segment
// storage. Tokens cannot cross physical endings, NUL or partial-tab contributions;
// the block/reference producers split only at those boundaries. Each ordinary
// run and each transformation retains its own original-byte range, never an
// offset inferred from semantic length. References also normalize physical CRs.
// An interrupted value is withheld; callers discard their unfinished phase.
func decodeValue(ctx context.Context, parts []Segment, normalizeEndings bool) Value {
	meterWork(ctx, 1)
	var out Value
	var semantic strings.Builder
	for _, part := range parts {
		meterWork(ctx, 1)
		if interruption(ctx) != "" {
			return Value{}
		}
		if part.Transformation == PartialTab {
			out.Segments = meteredAppend(ctx, out.Segments, part)
			meteredWriteString(ctx, &semantic, part.Semantic.Value)
			continue
		}
		span := part.Source
		raw := span.source.content[span.start:span.end]
		appendPart := func(a, b int, kind Transformation, text string) {
			out.Segments = meteredAppend(ctx, out.Segments, Segment{
				Source:         Span{source: span.source, start: span.start + a, end: span.start + b, present: true},
				Transformation: kind, Semantic: Some(text),
			})
			meteredWriteString(ctx, &semantic, text)
		}
		run := 0
		for offset, checkpoint := 0, 0; offset < len(raw); {
			meterWork(ctx, 1)
			if offset >= checkpoint {
				if interruption(ctx) != "" {
					return Value{}
				}
				checkpoint = offset + 256
			}
			text, width, kind := decodeTokenContext(ctx, raw[offset:])
			if raw[offset] == 0 {
				text, width, kind = "\ufffd", 1, Normalized
			} else if normalizeEndings && raw[offset] == '\r' {
				text, width, kind = "\n", 1, Normalized
				if offset+1 < len(raw) && raw[offset+1] == '\n' {
					width++
				}
			}
			if width == 0 {
				offset++
				continue
			}
			if run < offset {
				appendPart(run, offset, Exact, raw[run:offset])
			}
			appendPart(offset, offset+width, kind, text)
			offset += width
			run = offset
		}
		if run < len(raw) || len(raw) == 0 {
			appendPart(run, len(raw), Exact, raw[run:])
		}
	}
	out.Semantic = Some(semantic.String())
	return out
}

func decodeToken(text string) (string, int, Transformation) { return decodeTokenContext(nil, text) }
