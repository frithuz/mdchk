package parser

import "errors"

// ErrRange means a range is absent, out of bounds, or belongs to another snapshot.
var ErrRange = errors.New("invalid source range or snapshot")

// Span addresses original bytes, never decoded text. Its zero value is absent;
// a present [0,0) range is different. Copies retain the owning snapshot.
type Span struct {
	source     Source
	start, end int
	present    bool
}

// Len returns the original byte count, without copying the snapshot.
func (s Source) Len() int { return len(s.content) }

// Span constructs a bounded, zero-based, half-open range. EOF is [Len(),Len()).
// Copies of Source share ownership; separately acquired identical bytes do not.
func (s Source) Span(start, end int) (Span, error) {
	if start < 0 || end < start || end > s.Len() {
		return Span{}, ErrRange
	}
	return Span{source: s, start: start, end: end, present: true}, nil
}

// Text returns an immutable original-byte view. It rejects absent/foreign spans.
// The view retains the snapshot backing storage. Bytes continues to return a copy.
func (s Source) Text(span Span) (string, error) {
	if !span.present || span.source != s || span.start < 0 || span.end < span.start || span.end > s.Len() {
		return "", ErrRange
	}
	return s.content[span.start:span.end], nil
}

func (s Span) Present() bool { return s.present }
func (s Span) Start() int    { return s.start }
func (s Span) End() int      { return s.end }

func contains(outer, inner Span) bool {
	return outer.present && inner.present && outer.source == inner.source && inner.start >= outer.start && inner.end <= outer.end
}

// Optional distinguishes an unknown/absent property from a known zero value.
type Optional[T any] struct {
	Value   T
	Present bool
}

func Some[T any](value T) Optional[T] { return Optional[T]{Value: value, Present: true} }

// Position is a public one-based line/code-point coordinate, with exclusive range
// ends and tabs counting as one column. Source-to-position mapping is delivery #4;
// Span access never guesses positions from bytes or semantic string lengths.
type Position struct{ Line, Column int }
