package md038

import (
	"context"
	"fmt"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// Correction regenerates one deletion group per inline code span whose two
// fix-available MD038 findings match a deletable pair of the round's own parse.
// Each group deletes both reported edge runs, so a span is never half-corrected;
// a finding without a matching pair of this parse gets none.
func Correction(ctx context.Context, c fix.RoundContext, diagnostics []result.Diagnostic) ([]fix.Group, error) {
	if len(diagnostics) == 0 {
		return nil, nil
	}
	pairs, err := deletablePairs(ctx, c)
	if err != nil {
		return nil, err
	}
	available := make(map[int]parser.Span, len(diagnostics))
	for _, d := range diagnostics {
		if err := c.Reserve(ctx, 8, 2); err != nil {
			return nil, err
		}
		if d.FixAvailable {
			s, err := c.Current().Span(d.ByteRange.Start, d.ByteRange.End)
			if err == nil {
				available[s.Start()] = s
			}
		}
	}
	groups := make([]fix.Group, 0, len(diagnostics)/2)
	for _, d := range diagnostics {
		if err := c.Reserve(ctx, 128, 12); err != nil {
			return nil, err
		}
		p, ok := pairs[d.ByteRange.Start]
		if !d.FixAvailable || !ok || p[0].End() != d.ByteRange.End || available[p[1].Start()] != p[1] {
			continue
		}
		edits := make([]fix.Edit, 0, 2)
		for _, s := range p {
			expected, err := c.Current().Text(s)
			if err != nil {
				return nil, err
			}
			edits = append(edits, fix.Edit{Span: s, Expected: []byte(expected)})
		}
		groups = append(groups, fix.Group{ID: fmt.Sprintf("MD038-padding-%d", p[0].Start()), Rule: "MD038", Provenance: "MD038 inline code padding", Anchor: p[0], Edits: edits, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return groups, c.Checkpoint(ctx, 0)
}

// deletablePairs indexes the round's deletable code span edge runs by the start
// of the opening run.
func deletablePairs(ctx context.Context, c fix.RoundContext) (map[int][2]parser.Span, error) {
	spans, err := c.DeletableCodeSpanSpaces(ctx)
	if err != nil {
		return nil, err
	}
	if len(spans)%2 != 0 {
		return nil, parser.ErrModel
	}
	if err := c.Reserve(ctx, int64(len(spans))+1, int64(len(spans))/2+1); err != nil {
		return nil, err
	}
	out := make(map[int][2]parser.Span, len(spans)/2)
	for i := 0; i < len(spans); i += 2 {
		out[spans[i].Start()] = [2]parser.Span{spans[i], spans[i+1]}
	}
	return out, c.Checkpoint(ctx, 0)
}

// codePaddingRound holds the facts every MD038 group of a round shares. It is
// computed once, so proof cost does not grow with the group count.
type codePaddingRound struct {
	pairs     map[int][2]parser.Span
	preserved bool
}

// Checker returns the MD038 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD038", Check: checkCodePadding}
}

// checkCodePadding authorizes one group against the before parse, trusting
// neither producer data nor fix flags. Whole-candidate proof runs once for the
// round: the candidate differs only in the edits, and both parses compare
// equal with every code span value and Text compared exactly. Directive
// structure is compared by the validator's shared preservation, and deleting
// code span padding cannot create a directive finding, so no
// ForbidNewFindings is registered.
func checkCodePadding(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("code-span-padding", func() (any, error) { return codePaddingProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(codePaddingRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD038" || len(g.Edits) != 2 {
		return fix.Unsafe, nil
	}
	p, ok := round.pairs[g.Edits[0].Span.Start()]
	if !ok || g.Anchor != p[0] {
		return fix.Unsafe, nil
	}
	for i, e := range g.Edits {
		if e.Span != p[i] || len(e.Replacement) != 0 {
			return fix.Unsafe, nil
		}
		expected, err := c.Before.Current().Text(e.Span)
		if err != nil {
			return fix.Unknown, err
		}
		if err := c.Before.Checkpoint(ctx, int64(len(expected))+1); err != nil {
			return fix.Unknown, err
		}
		if string(e.Expected) != expected {
			return fix.Unsafe, nil
		}
	}
	return fix.Preserved, nil
}

// codePaddingProof recomputes the deletable pairs from the before parse, proves
// the combined candidate byte for byte and then compares both parses exactly.
func codePaddingProof(ctx context.Context, c fix.CheckContext) (codePaddingRound, error) {
	failed := codePaddingRound{}
	pairs, err := deletablePairs(ctx, c.Before)
	if err != nil {
		return failed, err
	}
	if unchanged, err := c.OnlyEditsChanged(ctx); err != nil || !unchanged {
		return failed, err
	}
	if same, err := c.MeaningPreserved(ctx); err != nil || !same {
		return failed, err
	}
	return codePaddingRound{pairs, true}, c.Before.Checkpoint(ctx, 0)
}
