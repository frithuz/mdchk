package parser

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrIndentation = errors.New("invalid indentation request or cursor state")

// SemanticText provides source-backed contributions, replacing valid U+0000
// with U+FFFD semantically, with Normalized provenance. Literal U+FFFD remains
// Exact. It requires a strict-valid whole source and scalar-aligned selected
// bytes, but does not restrict byte spans to the diagnostic CRLF boundary domain.
// Work and output storage are O(selected bytes); unchanged text uses source views.
// Returned slices belong to the caller; subsequent calls are independent.
func (index LineIndex) SemanticText(span Span) (Value, error) {
	raw, err := index.source.Text(span)
	if err != nil {
		return Value{}, err
	}
	if failure := index.EncodingFailure(); failure != nil {
		return Value{}, failure
	}
	text := index.source.content
	if span.start < len(text) && !utf8.RuneStart(text[span.start]) || span.end < len(text) && !utf8.RuneStart(text[span.end]) || !utf8.ValidString(raw) {
		return Value{}, ErrPosition
	}
	return index.semanticText(span.start, span.end), nil
}

func (index LineIndex) semanticText(start, end int) Value {
	raw := index.source.content[start:end]
	value := Value{Semantic: Some(strings.ReplaceAll(raw, "\x00", "\ufffd"))}
	appendExact := func(a, b int) {
		if a < b {
			value.Segments = append(value.Segments, Segment{Source: index.span(a, b), Transformation: Exact, Semantic: Some(index.source.content[a:b])})
		}
	}
	run := start
	for offset := start; offset < end; offset++ {
		if index.source.content[offset] == 0 {
			appendExact(run, offset)
			value.Segments = append(value.Segments, Segment{Source: index.span(offset, offset+1), Transformation: Normalized, Semantic: Some("\ufffd")})
			run = offset + 1
		}
	}
	appendExact(run, end)
	return value
}

// Indentation is a caller-owned structural cursor for one physical content line.
// Its column is zero-based and retains the original tab-stop phase across calls
// and caller-interpreted container markers. Copies advance independently over the
// same immutable snapshot; concurrent mutation of one cursor is not supported.
// The zero cursor is empty. It never recognizes Markdown containers or code.
type Indentation struct {
	index        LineIndex
	offset, end  int
	column       int
	tabRemaining int
}

// Indentation starts a structural cursor at the requested line's first byte.
// Internal tabs remain literal in Remaining unless explicitly consumed here.
func (index LineIndex) Indentation(line int) (Indentation, error) {
	if failure := index.EncodingFailure(); failure != nil {
		return Indentation{}, failure
	}
	if line <= 0 || line > len(index.lines) {
		return Indentation{}, ErrPosition
	}
	l := index.lines[line-1]
	return Indentation{index: index, offset: l.start, end: l.end}, nil
}

// Offset is the next original byte; it remains on a partly consumed tab until
// that tab's final structural column is consumed. Column is not a diagnostic
// code-point column. Neither accessor derives offsets from semantic text lengths.
func (c Indentation) Offset() int { return c.offset }
func (c Indentation) Column() int { return c.column }

// AdvanceMarker consumes one non-space/tab scalar chosen as a marker by the
// caller, advancing one structural column. EOF or a pending partial tab rejects
// the request without changing the cursor. Use Consume for spaces/tabs.
func (c *Indentation) AdvanceMarker() (Span, error) {
	if c.tabRemaining != 0 || c.offset == c.end {
		return Span{}, ErrIndentation
	}
	r, size := utf8.DecodeRuneInString(c.index.source.content[c.offset:c.end])
	if r == ' ' || r == '\t' {
		return Span{}, ErrIndentation
	}
	span := c.index.span(c.offset, c.offset+size)
	c.offset += size
	c.column++
	return span, nil
}

// IndentationConsumption records the number of structural columns removed and
// their source-backed semantic spaces. It can be shorter than the requested
// count when content or EOF is reached.
type IndentationConsumption struct {
	Columns int
	Content Value
}

// Consume removes up to columns ASCII space/tab indentation columns. Tabs use
// four-column stops, with each consumed contribution marked PartialTab, including
// a whole expanded tab. A split tab's successive contributions share its exact
// one-byte span. Negative requests fail without mutation; zero is a no-op.
// Work/storage are O(consumed bytes + columns), without rescanning earlier text.
func (c *Indentation) Consume(columns int) (IndentationConsumption, error) {
	if columns < 0 {
		return IndentationConsumption{}, ErrIndentation
	}
	result := IndentationConsumption{}
	for result.Columns < columns && c.offset < c.end {
		start := c.offset
		switch c.index.source.content[start] {
		case ' ':
			count := 0
			for c.offset < c.end && c.index.source.content[c.offset] == ' ' && result.Columns+count < columns {
				c.offset++
				count++
			}
			result.Content.Segments = append(result.Content.Segments, Segment{Source: c.index.span(start, c.offset), Transformation: Exact, Semantic: Some(c.index.source.content[start:c.offset])})
			result.Columns += count
			c.column += count
		case '\t':
			if c.tabRemaining == 0 {
				c.tabRemaining = 4 - c.column%4
			}
			count := min(c.tabRemaining, columns-result.Columns)
			result.Content.Segments = append(result.Content.Segments, Segment{Source: c.index.span(start, start+1), Transformation: PartialTab, Semantic: Some(strings.Repeat(" ", count))})
			result.Columns += count
			c.column += count
			c.tabRemaining -= count
			if c.tabRemaining == 0 {
				c.offset++
			}
		default:
			result.Content.Semantic = Some(strings.Repeat(" ", result.Columns))
			return result, nil
		}
	}
	result.Content.Semantic = Some(strings.Repeat(" ", result.Columns))
	return result, nil
}

// Remaining returns semantic content without advancing, excluding the original
// line ending. Only a partly consumed leading tab becomes residual spaces;
// internal tabs stay literal. NUL is semantically replaced with explicit original
// provenance. Work/storage are O(remaining bytes); each result owns its slices.
func (c Indentation) Remaining() Value {
	if c.tabRemaining == 0 {
		return c.index.semanticText(c.offset, c.end)
	}
	value := c.index.semanticText(c.offset+1, c.end)
	spaces := strings.Repeat(" ", c.tabRemaining)
	segment := Segment{Source: c.index.span(c.offset, c.offset+1), Transformation: PartialTab, Semantic: Some(spaces)}
	value.Segments = append([]Segment{segment}, value.Segments...)
	value.Semantic = Some(spaces + value.Semantic.Value)
	return value
}
