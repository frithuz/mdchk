package md089

import (
	"context"
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// pairNotProven is the deferral reason for an insertion point whose neighbors
// are not a CJK/Latin pair, which stays manual.
const pairNotProven = "meaning-not-proven"

// space is the one ASCII SPACE inserted at each point.
var space = []byte{' '}

// Correction regenerates one insertion group per fixable MD089 finding from the
// round's own parse. Each edit inserts exactly one ASCII SPACE at the zero-width
// point before the right-hand scalar. A finding the round's facts no longer
// report gets no group.
func Correction(ctx context.Context, c fix.RoundContext, diagnostics []result.Diagnostic) ([]fix.Group, error) {
	if len(diagnostics) == 0 {
		return nil, nil
	}
	points, err := c.CJKSpacing(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Reserve(ctx, int64(len(points))*2+1, int64(len(points))+int64(len(diagnostics))*12); err != nil {
		return nil, err
	}
	byStart := make(map[int]parser.Span, len(points))
	for _, p := range points {
		byStart[p.Start()] = p
	}
	out := make([]fix.Group, 0, len(diagnostics))
	for _, d := range diagnostics {
		p, ok := byStart[d.ByteRange.Start]
		if !ok || p.End() != d.ByteRange.End {
			continue
		}
		pair, err := cjkLatinPair(ctx, c, p)
		if err != nil {
			return nil, err
		}
		group := fix.Group{ID: fmt.Sprintf("MD089-offset-%d", p.Start()), Rule: "MD089", Provenance: "MD089 missing CJK/Latin space", Anchor: p, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}}
		if !pair {
			group.DeferReason = pairNotProven
		} else {
			group.Edits = []fix.Edit{{Span: p, Replacement: space}}
		}
		out = append(out, group)
	}
	return out, c.Checkpoint(ctx, 0)
}

// cjkLatinPair reports whether the scalars directly around the zero-width point
// are one Han ideograph and one Latin letter or ASCII digit, in either order.
func cjkLatinPair(ctx context.Context, c fix.RoundContext, p parser.Span) (bool, error) {
	if err := c.Reserve(ctx, 2*utf8.UTFMax+1, 1); err != nil {
		return false, err
	}
	src := c.Current()
	if p.Start() != p.End() {
		return false, nil
	}
	left, err := src.Span(max(0, p.Start()-utf8.UTFMax), p.Start())
	if err != nil {
		return false, err
	}
	right, err := src.Span(p.Start(), min(src.Len(), p.Start()+utf8.UTFMax))
	if err != nil {
		return false, err
	}
	before, err := src.Text(left)
	if err != nil {
		return false, err
	}
	after, err := src.Text(right)
	if err != nil {
		return false, err
	}
	l, lw := utf8.DecodeLastRuneInString(before)
	r, rw := utf8.DecodeRuneInString(after)
	if l == utf8.RuneError && lw <= 1 || r == utf8.RuneError && rw <= 1 {
		return false, nil
	}
	a, b := class(l), class(r)
	return a != 0 && b != 0 && a != b, nil
}

// class is 1 for Han Ideographic, 2 for Latin letters and ASCII digits, else 0.
func class(r rune) byte {
	switch {
	case unicode.Is(unicode.Han, r) && unicode.Is(unicode.Properties["Ideographic"], r):
		return 1
	case r >= '0' && r <= '9' || unicode.Is(unicode.Latin, r) && unicode.IsLetter(r):
		return 2
	}
	return 0
}

// spacingRound holds the facts every MD089 group of a round shares. It is
// computed once, so proof cost does not grow with the group count.
type spacingRound struct {
	points    map[int]parser.Span
	preserved bool
}

// Checker returns the MD089 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD089", Check: checkCJKSpacing}
}

// checkCJKSpacing authorizes one group against the before parse, trusting
// neither producer data nor fix flags. Whole-candidate proof runs once for the
// round. Shared protection, binding and directive checks remain mandatory in
// Validator, and inserting a space in prose outside comments cannot create a
// directive finding, so no ForbidNewFindings is registered.
func checkCJKSpacing(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("cjk-spacing", func() (any, error) { return cjkSpacingProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(spacingRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD089" || len(g.Edits) != 1 || len(g.Edits[0].Expected) != 0 || string(g.Edits[0].Replacement) != " " {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	p, ok := round.points[e.Span.Start()]
	if !ok || p != e.Span || g.Anchor != p {
		return fix.Unsafe, nil
	}
	pair, err := cjkLatinPair(ctx, c.Before, p)
	if err != nil {
		return fix.Unknown, err
	}
	if !pair {
		return fix.Unsafe, nil
	}
	return fix.Preserved, nil
}

// cjkSpacingProof recomputes the insertion points from the before parse,
// requires every round edit to be one SPACE at one of them, proves the combined
// candidate byte for byte and then compares both parses: node kinds, nesting and
// properties must match, and Text values may differ only by inserted SPACEs.
func cjkSpacingProof(ctx context.Context, c fix.CheckContext) (spacingRound, error) {
	failed := spacingRound{}
	points, err := c.Before.CJKSpacing(ctx)
	if err != nil {
		return failed, err
	}
	if err := c.Before.Reserve(ctx, int64(len(points))*2+1, int64(len(points))+1); err != nil {
		return failed, err
	}
	round := spacingRound{points: make(map[int]parser.Span, len(points))}
	for _, p := range points {
		round.points[p.Start()] = p
	}
	_, edits, err := c.BoundedViews(ctx)
	if err != nil {
		return failed, err
	}
	for _, e := range edits {
		if p, ok := round.points[e.Edit.Span.Start()]; !ok || p != e.Edit.Span || len(e.Edit.Expected) != 0 || string(e.Edit.Replacement) != " " {
			return failed, nil
		}
	}
	if unchanged, err := c.OnlyEditsChanged(ctx); err != nil || !unchanged {
		return failed, err
	}
	if same, err := c.MeaningPreservedAcrossInsertedSpaces(ctx); err != nil || !same {
		return failed, err
	}
	round.preserved = true
	return round, c.Before.Checkpoint(ctx, 0)
}
