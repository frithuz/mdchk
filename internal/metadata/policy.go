package metadata

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode"

	"github.com/frithuz/mdchk/internal/parser"
)

// Title distinguishes an observed valid title from authoritative absence.
// Known=false means dependent title conclusions are unavailable. A genuine
// positive string can remain known beside unrelated incomplete metadata.
type Title struct {
	Text           string
	Location       parser.Span
	Present, Known bool
}

func (r Result) Title(ctx context.Context, b Binding) (out Title, err error) {
	if r.owner == nil || r.owner != b.state {
		return Title{}, ErrContract
	}
	if err := b.charge(ctx, 1, 0); err != nil {
		return Title{}, err
	}
	defer func() {
		err = errors.Join(err, b.charge(ctx, 0, 0))
		if err != nil {
			out = Title{}
		}
	}()
	for _, e := range r.root.state.entries {
		if err := b.charge(ctx, int64(len(e.key))+1, 0); err != nil {
			return Title{}, err
		}
		if e.key != "title" {
			continue
		}
		// A duplicate exact key makes even a positive title ambiguous.
		duplicates := 0
		for _, other := range r.root.state.entries {
			if err := b.charge(ctx, int64(len(other.key))+1, 0); err != nil {
				return Title{}, err
			}
			if other.key == "title" {
				duplicates++
			}
		}
		if duplicates != 1 || !e.value.Complete() {
			return Title{}, nil
		}
		text, ok := e.value.Scalar()
		if e.value.Kind() != String || !ok {
			return Title{Known: true}, nil
		}
		if err := b.charge(ctx, int64(len(text))+1, 0); err != nil {
			return Title{}, err
		}
		if strings.TrimFunc(text, unicode.IsSpace) == "" {
			return Title{Known: true}, nil
		}
		return Title{text, e.value.Span(), true, true}, nil
	}
	return Title{Known: r.Complete()}, nil
}

// OrderOptions translates MD072 policy. Its zero value preserves the rule's
// recursive=false and case_sensitive=true defaults. CaseInsensitive enables
// Unicode17 full default folding without normalization or whitespace changes.
type OrderOptions struct{ Recursive, CaseInsensitive bool }

// Order is an immutable source-order comparison and stable desired permutation.
// A violation may be reported even when movement is ineligible. KnownIndex
// inspects retained facts after a stop; it does not start new analysis.
type Order struct {
	mapping   Value
	indices   []int
	violation bool
	move      MoveEligibility
}

func (o Order) Mapping() Value { return o.mapping }
func (o Order) Count() int     { return len(o.indices) }
func (o Order) KnownIndex(i int) (int, bool) {
	if i < 0 || i >= len(o.indices) {
		return 0, false
	}
	return o.indices[i], true
}
func (o Order) Violation() bool                  { return o.violation }
func (o Order) MoveEligibility() MoveEligibility { return o.move }
func (r Result) Ordering(ctx context.Context, b Binding, options OrderOptions) ([]Order, error) {
	if r.owner == nil || r.owner != b.state {
		return nil, ErrContract
	}
	var out []Order
	var walk func(Value) error
	walk = func(v Value) error {
		if err := b.charge(ctx, 1, 0); err != nil {
			return err
		}
		if v.Kind() == Mapping && v.Complete() {
			if err := b.charge(ctx, int64(len(v.state.entries))+1, 2*int64(len(v.state.entries))+1); err != nil {
				return err
			}
			keys := make([]string, len(v.state.entries))
			indices := make([]int, len(keys))
			for i, e := range v.state.entries {
				keys[i], indices[i] = e.key, i
				if options.CaseInsensitive {
					var err error
					if err := b.charge(ctx, 0, 3*int64(len(e.key))+1); err != nil {
						return err
					}
					keys[i], err = parser.FoldCase(foldContext{ctx, b.state.operation}, b.state.budget, e.key)
					if err != nil {
						return err
					}
					if err := b.charge(ctx, 0, 0); err != nil {
						return err
					}
				}
			}
			// Stable sorting preserves original order when full-folded keys tie.
			// Reserve stable-sort swaps; charge byte comparisons at each checkpoint.
			levels := int64(1)
			for n := len(keys); n > 1; n = (n + 1) / 2 {
				levels++
			}
			if err := b.charge(ctx, int64(len(keys))*levels*levels, 0); err != nil {
				return err
			}
			if err := sortIndices(ctx, b, keys, indices); err != nil {
				return err
			}
			violation := false
			for i, index := range indices {
				violation = violation || i != index
			}
			eligibility, err := moveEligibility(ctx, b, v)
			if err != nil {
				return err
			}
			out = append(out, Order{v, indices, violation, eligibility})
		}
		if options.Recursive {
			for _, e := range v.state.entries {
				if err := walk(e.value); err != nil {
					return err
				}
			}
			for _, e := range v.state.elements {
				if err := walk(e); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(r.root); err != nil {
		return nil, err
	}
	return out, b.charge(ctx, 0, 0)
}

// MoveRestrictions are producer-established syntax facts. They are not caller
// switches: only the private source-validating format builder sets them.
type MoveRestrictions uint8

const (
	StandaloneComments MoveRestrictions = 1 << iota
	AnchorsOrAliases
	TOMLTables
	TOMLDottedKeys
	TOMLArrayTables
)

type MoveEligibility string

const (
	IndependentEntries MoveEligibility = "independent-entries"
	ReportOnly         MoveEligibility = "report-only"
)

// moveEligibility is a necessary whole-entry policy check only. It issues no
// edit permission; the future candidate must independently prove unchanged
// native values, sequence order, spelling, comments, delimiters and untouched
// bytes. Current ordinary fix protection still rejects metadata changes.
func moveEligibility(ctx context.Context, b Binding, v Value) (MoveEligibility, error) {
	if v.Kind() != Mapping || !v.Complete() || v.state.restrictions != 0 || !v.Span().Present() || len(v.state.entries) == 0 {
		return ReportOnly, nil
	}
	end := v.Span().Start()
	for _, e := range v.state.entries {
		if !e.Span().Present() || !e.KeySpan().Present() || e.Span().Start() < end || e.Span().End() > v.Span().End() {
			return ReportOnly, nil
		}
		gap, _ := b.Source().Span(end, e.Span().Start())
		text, _ := b.Source().Text(gap)
		if err := b.charge(ctx, int64(len(text)+e.Span().End()-e.Span().Start())+5, 0); err != nil {
			return ReportOnly, err
		}
		if strings.Trim(text, " \t\r\n") != "" {
			return ReportOnly, nil
		}
		// A moveable entry must own complete physical lines, including its
		// original ending. Flow members and partial-line slices are report-only.
		if e.Span().Start() == e.Span().End() || !physicalLineBoundary(b.Source(), e.Span().Start()) || !physicalLineBoundary(b.Source(), e.Span().End()) {
			return ReportOnly, nil
		}
		end = e.Span().End()
	}
	tail, _ := b.Source().Span(end, v.Span().End())
	text, _ := b.Source().Text(tail)
	if err := b.charge(ctx, int64(len(text))+1, 0); err != nil {
		return ReportOnly, err
	}
	if strings.Trim(text, " \t\r\n") != "" {
		return ReportOnly, nil
	}
	return IndependentEntries, nil
}

// Stop sorting immediately at an actual comparison checkpoint, without hiding
// unrelated panics or publishing a partially sorted permutation.
type sortStop struct{ err error }

func sortIndices(ctx context.Context, b Binding, keys []string, indices []int) (err error) {
	defer func() {
		if caught := recover(); caught != nil {
			if stop, ok := caught.(sortStop); ok {
				err = stop.err
			} else {
				panic(caught)
			}
		}
	}()
	sort.SliceStable(indices, func(i, j int) bool {
		if e := b.charge(ctx, int64(len(keys[indices[i]]))+int64(len(keys[indices[j]]))+1, 0); e != nil {
			panic(sortStop{e})
		}
		return keys[indices[i]] < keys[indices[j]]
	})
	return nil
}

// FoldCase checks Err at bounded checkpoints; combine operation and caller
// lifetime without a goroutine or a new cancellation/deadline allowance.
type foldContext struct {
	context.Context
	operation context.Context
}

func (c foldContext) Err() error { return errors.Join(c.Context.Err(), c.operation.Err()) }

// A physical boundary follows a complete LF, CR or CRLF ending. Test absolute
// source positions for both endpoints; a mapping start supplies no exemption.
func physicalLineBoundary(source parser.Source, offset int) bool {
	if offset == 0 {
		return true
	}
	pair, _ := source.Span(offset-1, min(offset+1, source.Len()))
	text, _ := source.Text(pair)
	if text == "" || text[0] != '\r' && text[0] != '\n' {
		return false
	}
	return text != "\r\n"
}
