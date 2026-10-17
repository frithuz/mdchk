package parser

import (
	"errors"
	"sort"
	"unicode/utf8"
)

// ErrPosition identifies an invalid text position, byte boundary or line request.
var ErrPosition = errors.New("invalid source position or line")

type physicalLine struct{ start, end, next int }

// PhysicalLine separates original content bytes from their LF, CRLF or CR ending.
// Ending is a present empty span at EOF when the content has no terminator.
type PhysicalLine struct{ Content, Ending Span }

// LineIndex retains an immutable Source and reusable physical-line boundaries.
// Copies share immutable storage and are safe for concurrent readers. The zero
// value indexes the empty zero Source. Construction takes O(N) time and O(L)
// storage for N bytes and L content lines, without copying the source bytes.
// Arbitrary bytes can be indexed; decoding-dependent operations require strict
// UTF-8. A terminal ending creates a coordinate endpoint, not a content line.
type LineIndex struct {
	source  Source
	lines   []physicalLine
	invalid Optional[int]
}

func NewLineIndex(source Source) LineIndex {
	index := LineIndex{source: source}
	text := source.content
	for start := 0; start < len(text); {
		end := start
		for end < len(text) && text[end] != '\r' && text[end] != '\n' {
			end++
		}
		next := end
		if next < len(text) {
			next++
			if text[end] == '\r' && next < len(text) && text[next] == '\n' {
				next++
			}
		}
		index.lines = append(index.lines, physicalLine{start, end, next})
		start = next
	}
	if offset := invalidUTF8Offset(text); offset >= 0 {
		index.invalid = Some(offset)
	}
	return index
}

func (index LineIndex) Source() Source { return index.source }

// LineCount counts selectable content lines, including actual empty lines.
// Empty input has none; an ending at EOF adds no extra selectable line.
func (index LineIndex) LineCount() int { return len(index.lines) }

// Line returns one-based physical-line spans, even for malformed UTF-8.
func (index LineIndex) Line(number int) (PhysicalLine, error) {
	if number <= 0 || number > len(index.lines) {
		return PhysicalLine{}, ErrPosition
	}
	l := index.lines[number-1]
	return PhysicalLine{
		Content: index.span(l.start, l.end),
		Ending:  index.span(l.end, l.next),
	}, nil
}

func (index LineIndex) span(start, end int) Span {
	return Span{source: index.source, start: start, end: end, present: true}
}

// EncodingFailure returns a fresh typed failure at the first invalid scalar's
// original leading byte, or nil. Mutating the result cannot alter the index.
func (index LineIndex) EncodingFailure() *Failure {
	if !index.invalid.Present {
		return nil
	}
	offset := index.invalid.Value
	return &Failure{Code: InvalidUTF8, Message: "original snapshot is not strict UTF-8", Location: index.span(offset, offset)}
}

// FailurePosition derives only the first encoding failure's diagnostic point
// from the valid prefix. It does not make whole-document text mapping available.
// A valid source has no encoding failure and returns ErrPosition.
func (index LineIndex) FailurePosition() (Position, error) {
	if !index.invalid.Present {
		return Position{}, ErrPosition
	}
	return index.positionAt(index.invalid.Value)
}

// Position converts a scalar boundary in a strict-valid UTF-8 source, rejecting
// continuation-byte and CRLF interiors. Terminator start is the preceding line's
// end; terminator end is next-line column 1, including virtual EOF after a final
// ending. Lookup costs O(log L + B), where B is the line-prefix byte count.
func (index LineIndex) Position(offset int) (Position, error) {
	if failure := index.EncodingFailure(); failure != nil {
		return Position{}, failure
	}
	return index.positionAt(offset)
}

func (index LineIndex) positionAt(offset int) (Position, error) {
	text := index.source.content
	if offset < 0 || offset > len(text) {
		return Position{}, ErrPosition
	}
	if offset < len(text) && !utf8.RuneStart(text[offset]) && (!index.invalid.Present || offset != index.invalid.Value) {
		return Position{}, ErrPosition
	}
	if offset > 0 && offset < len(text) && text[offset-1] == '\r' && text[offset] == '\n' {
		return Position{}, ErrPosition
	}
	if len(index.lines) == 0 {
		return Position{Line: 1, Column: 1}, nil
	}
	last := index.lines[len(index.lines)-1]
	if offset == len(text) && last.next > last.end {
		return Position{Line: len(index.lines) + 1, Column: 1}, nil
	}
	n := sort.Search(len(index.lines), func(i int) bool { return index.lines[i].start > offset }) - 1
	line := index.lines[n]
	return Position{Line: n + 1, Column: utf8.RuneCountInString(text[line.start:offset]) + 1}, nil
}

// Offset converts a one-based physical-line/code-point position to original
// bytes. Tabs and combining code points each count as one diagnostic column.
// Nonexistent requests are rejected, never clamped or aliased. Lookup decodes
// at most the selected line prefix: O(B) time, O(1) additional storage.
func (index LineIndex) Offset(position Position) (int, error) {
	if failure := index.EncodingFailure(); failure != nil {
		return 0, failure
	}
	if position.Line <= 0 || position.Column <= 0 {
		return 0, ErrPosition
	}
	if position.Line == len(index.lines)+1 && position.Column == 1 {
		if len(index.lines) == 0 || index.lines[len(index.lines)-1].next > index.lines[len(index.lines)-1].end {
			return index.source.Len(), nil
		}
	}
	if position.Line > len(index.lines) {
		return 0, ErrPosition
	}
	line := index.lines[position.Line-1]
	offset := line.start
	for column := 1; column < position.Column; column++ {
		if offset == line.end {
			return 0, ErrPosition
		}
		_, size := utf8.DecodeRuneInString(index.source.content[offset:line.end])
		offset += size
	}
	return offset, nil
}
