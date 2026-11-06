package fix

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

// walkOffset and walkSpan restate the mapping contract as a walk over every edit.
func walkOffset(edits []SharedEdit, at int, right bool) (int, bool) {
	delta := 0
	for _, e := range edits {
		a, b := e.Edit.Span.Start(), e.Edit.Span.End()
		if at < a {
			break
		}
		if a == b {
			if at > a || right {
				delta += len(e.Edit.Replacement)
			}
			continue
		}
		if at > a && at < b {
			return 0, false
		}
		if at >= b {
			delta += len(e.Edit.Replacement) - (b - a)
		}
	}
	return at + delta, true
}

func TestEditMappingMatchesFullWalk(t *testing.T) {
	source := parser.NewSource([]byte("0123456789abcdefghij"))
	span := func(a, b int) parser.Span {
		s, err := source.Span(a, b)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	edit := func(a, b int, replacement string) SharedEdit {
		return SharedEdit{Edit: Edit{Span: span(a, b), Replacement: []byte(replacement)}}
	}
	for name, edits := range map[string][]SharedEdit{
		"none":       nil,
		"insertions": {edit(0, 0, "xx"), edit(5, 5, "y"), edit(20, 20, "zzz")},
		"removals":   {edit(1, 3, ""), edit(10, 15, "")},
		"growth":     {edit(2, 4, "wwwwwww"), edit(9, 10, "vv")},
		"adjacent":   {edit(3, 5, "a"), edit(5, 7, "bcd"), edit(7, 8, "")},
		"mixed":      {edit(0, 1, ""), edit(4, 4, "qq"), edit(6, 9, "r"), edit(12, 12, "s"), edit(15, 20, "")},
	} {
		t.Run(name, func(t *testing.T) {
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			m := meter{context.Background(), budget}
			after := parser.NewSource(make([]byte, 64))
			mapping, err := newEditMapping(source, after, edits, m)
			if err != nil {
				t.Fatal(err)
			}
			for at := 0; at <= source.Len(); at++ {
				for _, right := range []bool{false, true} {
					want, wantOK := walkOffset(edits, at, right)
					got, ok, err := mapping.offset(at, right)
					if err != nil || ok != wantOK || got != want {
						t.Fatalf("offset(%d, %v) = %d %v %v, want %d %v", at, right, got, ok, err, want, wantOK)
					}
				}
			}
			for a := 0; a <= source.Len(); a++ {
				for b := a; b <= source.Len(); b++ {
					s := span(a, b)
					hit := false
					for _, e := range edits {
						hit = hit || intersects(e.Edit.Span, s)
					}
					start, startOK := walkOffset(edits, a, true)
					end, endOK := walkOffset(edits, b, false)
					wantOK := !hit && startOK && endOK && end >= start
					got, ok, err := mapping.span(s)
					if err != nil || ok != wantOK {
						t.Fatalf("span(%d, %d) = %v %v, want %v", a, b, ok, err, wantOK)
					}
					if ok && (got.Start() != start || got.End() != end) {
						t.Fatalf("span(%d, %d) = [%d, %d), want [%d, %d)", a, b, got.Start(), got.End(), start, end)
					}
				}
			}
		})
	}
}

func TestEditMappingLookupIsLogarithmic(t *testing.T) {
	const n = 1 << 14
	source := parser.NewSource(make([]byte, 2*n))
	edits := make([]SharedEdit, n)
	for i := range edits {
		s, _ := source.Span(2*i, 2*i+1)
		edits[i] = SharedEdit{Edit: Edit{Span: s}}
	}
	// The budget admits building the index and little more; a full walk would exhaust it.
	limits := parser.DefaultLimits()
	limits.WorkUnits = n + 1 + 32
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	m := meter{context.Background(), budget}
	mapping, err := newEditMapping(source, source, edits, m)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := mapping.offset(2*n-1, true); err != nil {
		t.Fatal(err)
	}
}
