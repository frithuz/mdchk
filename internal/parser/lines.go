package parser

import (
	"context"
	"errors"
	"math/bits"
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
	ctx     context.Context
	source  Source
	lines   []physicalLine
	invalid Optional[int]
}

func NewLineIndex(source Source) LineIndex {
	return newLineIndex(nil, source) //nolint:staticcheck // SA1012: nil marks an unmetered index and preserves its empty zero value.
}

func newLineIndex(ctx context.Context, source Source) LineIndex {
	meterWork(ctx, 1)
	index := LineIndex{source: source, ctx: ctx}
	text := source.content
	meterWork(ctx, len(text)) // reserve the complete physical-line byte scan
	for start := 0; start < len(text); {
		meterWork(ctx, 1)
		end := start
		for end < len(text) && text[end] != '\r' && text[end] != '\n' {
			if end > start && (end-start)&255 == 0 && scannerInterrupted(ctx) {
				return index
			}
			end++
		}
		next := end
		if next < len(text) {
			next++
			if text[end] == '\r' && next < len(text) && text[next] == '\n' {
				next++
			}
		}
		index.lines = meteredAppend(ctx, index.lines, physicalLine{start, end, next})
		start = next
	}
	if offset := invalidUTF8OffsetContext(ctx, text); offset >= 0 {
		index.invalid = Some(offset)
	}
	return index
}

func (index LineIndex) Source() Source { meterWork(index.ctx, 1); return index.source }

// LineCount counts selectable content lines, including actual empty lines.
// Empty input has none; an ending at EOF adds no extra selectable line.
func (index LineIndex) LineCount() int { meterWork(index.ctx, 1); return len(index.lines) }

// Line returns one-based physical-line spans, even for malformed UTF-8.
func (index LineIndex) Line(number int) (PhysicalLine, error) {
	meterWork(index.ctx, 1)
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
	meterWork(index.ctx, 1)
	return Span{source: index.source, start: start, end: end, present: true}
}

// EncodingFailure returns a fresh typed failure at the first invalid scalar's
// original leading byte, or nil. Mutating the result cannot alter the index.
func (index LineIndex) EncodingFailure() *Failure {
	meterWork(index.ctx, 1)
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
	meterWork(index.ctx, 1)
	if !index.invalid.Present {
		return Position{}, ErrPosition
	}
	return index.positionAt(index.invalid.Value)
}

// InvalidSubpart locates one invalid maximal subpart: the half-open byte span and
// the one-based point of its leading byte. Each valid scalar and each earlier
// invalid subpart on the physical line counts as one column.
type InvalidSubpart struct {
	Start, End int
	Position   Position
}

// InvalidSubparts returns the first retain invalid maximal subparts in source
// order and the exact number found. Boundaries follow Unicode U+FFFD substitution
// of maximal subparts. It is one forward pass: O(N) time for N source bytes and
// O(retain) storage, whatever the error count. Valid sources return nil, 0.
func (index LineIndex) InvalidSubparts(retain int) ([]InvalidSubpart, int) {
	meterWork(index.ctx, 1)
	if !index.invalid.Present {
		return nil, 0
	}
	text := index.source.content
	meterWork(index.ctx, len(text))
	var parts []InvalidSubpart
	total := 0
	for n, line := range index.lines {
		column := 1
		for offset := line.start; offset < line.end; column++ {
			r, size := utf8.DecodeRuneInString(text[offset:line.end])
			if r == utf8.RuneError && size == 1 {
				// CR and LF are never continuation bytes, so a subpart cannot
				// cross the physical line that holds its leading byte.
				size = maximalSubpart(text[offset:line.end])
				if total < retain {
					parts = append(parts, InvalidSubpart{Start: offset, End: offset + size, Position: Position{Line: n + 1, Column: column}})
				}
				total++
			}
			offset += size
		}
	}
	return parts, total
}

// maximalSubpart measures the longest prefix of a well-formed sequence at the
// start of text, which must not begin with a valid scalar. The ranges are those
// of Unicode table 3-7. C0, C1, F5..FF and lone continuation bytes stand alone.
func maximalSubpart(text string) int {
	need, low, high := 0, byte(0x80), byte(0xBF)
	switch lead := text[0]; {
	case lead >= 0xC2 && lead <= 0xDF:
		need = 1
	case lead == 0xE0:
		need, low = 2, 0xA0
	case lead == 0xED:
		need, high = 2, 0x9F
	case lead >= 0xE1 && lead <= 0xEF:
		need = 2
	case lead == 0xF0:
		need, low = 3, 0x90
	case lead == 0xF4:
		need, high = 3, 0x8F
	case lead >= 0xF1 && lead <= 0xF3:
		need = 3
	}
	size := 1
	for size <= need && size < len(text) && text[size] >= low && text[size] <= high {
		size++
		low, high = 0x80, 0xBF
	}
	return size
}

// Position converts a scalar boundary in a strict-valid UTF-8 source, rejecting
// continuation-byte and CRLF interiors. Terminator start is the preceding line's
// end; terminator end is next-line column 1, including virtual EOF after a final
// ending. Lookup costs O(log L + B), where B is the line-prefix byte count.
func (index LineIndex) Position(offset int) (Position, error) {
	meterWork(index.ctx, 1)
	if failure := index.EncodingFailure(); failure != nil {
		return Position{}, failure
	}
	return index.positionAt(offset)
}

func (index LineIndex) positionAt(offset int) (Position, error) {
	meterWork(index.ctx, 1)
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
	n := sort.Search(len(index.lines), func(i int) bool { meterWork(index.ctx, 1); return index.lines[i].start > offset }) - 1
	line := index.lines[n]
	return Position{Line: n + 1, Column: utf8.RuneCountInString(text[line.start:offset]) + 1}, nil
}

// PositionWork is the cost PositionBounded charges for one conversion: both binary
// searches plus the line-prefix bytes it decodes. Callers meter Position with it.
func (index LineIndex) PositionWork(offset int) int64 {
	work := int64(2*bits.Len(uint(len(index.lines))) + 16)
	if len(index.lines) > 0 && offset >= 0 && offset <= index.source.Len() {
		if n := sort.Search(len(index.lines), func(i int) bool { return index.lines[i].start > offset }) - 1; n >= 0 {
			work += int64(offset - index.lines[n].start)
		}
	}
	return work
}

// PositionBounded performs the same conversion as Position, charging the caller's
// existing operation budget before binary searches and Unicode prefix decoding.
// The charge is O(log line-count + actual line-prefix bytes), not source size.
// No operation budget is created and no parser-private metering context escapes.
func (index LineIndex) PositionBounded(ctx context.Context, budget *OperationBudget, offset int) (Position, error) {
	if ctx == nil || budget == nil {
		return Position{}, errors.New("nil coordinate context or budget")
	}
	if err := ctx.Err(); err != nil {
		return Position{}, err
	}
	// Reserve both this locating search and the canonical conversion's search,
	// validity checks and constant-size result construction before doing either.
	if err := budget.ChargeWork(int64(2*bits.Len(uint(len(index.lines))) + 16)); err != nil {
		return Position{}, err
	}
	if offset < 0 || offset > index.source.Len() {
		return Position{}, ErrPosition
	}
	if len(index.lines) > 0 {
		n := sort.Search(len(index.lines), func(i int) bool { return index.lines[i].start > offset }) - 1
		if n >= 0 {
			if err := budget.ChargeWork(int64(offset - index.lines[n].start)); err != nil {
				return Position{}, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return Position{}, err
	}
	// The source/index are immutable. Use the canonical conversion without its
	// parse-local context; all work has just been reserved on this caller's meter.
	index.ctx = nil
	position, err := index.Position(offset)
	if stop := ctx.Err(); stop != nil {
		return Position{}, stop
	}
	if stop := budget.ChargeWork(0); stop != nil {
		return Position{}, stop
	}
	return position, err
}

// Offset converts a one-based physical-line/code-point position to original
// bytes. Tabs and combining code points each count as one diagnostic column.
// Nonexistent requests are rejected, never clamped or aliased. Lookup decodes
// at most the selected line prefix: O(B) time, O(1) additional storage.
func (index LineIndex) Offset(position Position) (int, error) {
	meterWork(index.ctx, 1)
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
		meterWork(index.ctx, 1)
		if offset == line.end {
			return 0, ErrPosition
		}
		_, size := utf8.DecodeRuneInString(index.source.content[offset:line.end])
		offset += size
	}
	return offset, nil
}
