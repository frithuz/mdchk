package md049

import (
	"context"
	"fmt"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// Correction regenerates one replacement group per fix-available MD049 finding
// whose opening marker matches a mismatched italic of the round's own parse.
// Each group rewrites both one-byte markers to the expected marker, so an
// italic is never half-corrected; a finding without a matching italic of this
// parse gets none.
func Correction(ctx context.Context, c fix.RoundContext, diagnostics []result.Diagnostic) ([]fix.Group, error) {
	if len(diagnostics) == 0 {
		return nil, nil
	}
	italics, marker, err := mismatched(ctx, c)
	if err != nil {
		return nil, err
	}
	groups := make([]fix.Group, 0, len(diagnostics))
	for _, d := range diagnostics {
		if err := c.Reserve(ctx, 128, 12); err != nil {
			return nil, err
		}
		f, ok := italics[d.ByteRange.Start]
		if !d.FixAvailable || !ok || f.Opening.End() != d.ByteRange.End {
			continue
		}
		edits := make([]fix.Edit, 0, 2)
		for _, s := range [2]parser.Span{f.Opening, f.Closing} {
			edits = append(edits, fix.Edit{Span: s, Expected: []byte{f.Marker}, Replacement: []byte{marker}})
		}
		groups = append(groups, fix.Group{ID: fmt.Sprintf("MD049-italic-%d", f.Opening.Start()), Rule: "MD049", Provenance: "MD049 italic delimiter style", Anchor: f.Opening, Edits: edits, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return groups, c.Checkpoint(ctx, 0)
}

// mismatched indexes the round's mismatched italics by the start of their
// opening marker and returns the expected marker.
func mismatched(ctx context.Context, c fix.RoundContext) (map[int]parser.DelimiterFact, byte, error) {
	facts, marker, err := c.ItalicStyleCorrections(ctx)
	if err != nil {
		return nil, 0, err
	}
	if err := c.Reserve(ctx, int64(len(facts))+1, int64(len(facts))+1); err != nil {
		return nil, 0, err
	}
	out := make(map[int]parser.DelimiterFact, len(facts))
	for _, f := range facts {
		out[f.Opening.Start()] = f
	}
	return out, marker, c.Checkpoint(ctx, 0)
}

// italicRound holds the facts every MD049 group of a round shares. It is
// computed once, so proof cost does not grow with the group count.
type italicRound struct {
	italics   map[int]parser.DelimiterFact
	marker    byte
	preserved bool
}

// Checker returns the MD049 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD049", Check: checkItalicStyle}
}

// checkItalicStyle authorizes one group against the before parse, trusting
// neither producer data nor fix flags. Whole-candidate proof runs once for the
// round: the candidate differs only in the edits, and both parses compare
// equal with every Text value exact and every emphasis node at the same
// markers, only mismatched italics changing their marker. Directive structure
// is compared by the validator's shared preservation, and rewriting emphasis
// markers outside comments cannot create a directive finding, so no
// ForbidNewFindings is registered.
func checkItalicStyle(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("italic-style", func() (any, error) { return italicStyleProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(italicRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD049" || len(g.Edits) != 2 {
		return fix.Unsafe, nil
	}
	f, ok := round.italics[g.Edits[0].Span.Start()]
	if !ok || g.Anchor != f.Opening {
		return fix.Unsafe, nil
	}
	for i, s := range [2]parser.Span{f.Opening, f.Closing} {
		e := g.Edits[i]
		if e.Span != s || string(e.Replacement) != string(round.marker) {
			return fix.Unsafe, nil
		}
		expected, err := c.Before.Current().Text(s)
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

// italicStyleProof recomputes the mismatched italics from the before parse,
// proves the combined candidate byte for byte and then compares both parses.
func italicStyleProof(ctx context.Context, c fix.CheckContext) (italicRound, error) {
	failed := italicRound{}
	italics, marker, err := mismatched(ctx, c.Before)
	if err != nil {
		return failed, err
	}
	if unchanged, err := c.OnlyEditsChanged(ctx); err != nil || !unchanged {
		return failed, err
	}
	allowed := make(map[int]bool, len(italics))
	for start := range italics {
		allowed[start] = true
	}
	if same, err := c.MeaningPreservedAcrossItalicMarkers(ctx, allowed, marker); err != nil || !same {
		return failed, err
	}
	return italicRound{italics, marker, true}, c.Before.Checkpoint(ctx, 0)
}
