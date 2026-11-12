package md050

import (
	"context"
	"fmt"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// Correction regenerates one group per fix-available MD050 finding from the
// round's own analysis and parse. Each group rewrites both markers of the
// reported Strong to the expected marker, so a Strong is never half-corrected;
// a finding without a matching rewritable Strong of this parse gets none.
func Correction(ctx context.Context, c fix.RoundContext, diagnostics []result.Diagnostic) ([]fix.Group, error) {
	if len(diagnostics) == 0 {
		return nil, nil
	}
	found, expected, err := rewritableFacts(ctx, c)
	if err != nil {
		return nil, err
	}
	groups := make([]fix.Group, 0, len(diagnostics))
	for _, d := range diagnostics {
		if err := c.Reserve(ctx, 128, 12); err != nil {
			return nil, err
		}
		f, ok := found[d.ByteRange.Start]
		if !d.FixAvailable || !ok || f.Opening.End() != d.ByteRange.End {
			continue
		}
		edits := make([]fix.Edit, 0, 2)
		for _, s := range [2]parser.Span{f.Opening, f.Closing} {
			text, err := c.Current().Text(s)
			if err != nil {
				return nil, err
			}
			edits = append(edits, fix.Edit{Span: s, Expected: []byte(text), Replacement: []byte{expected, expected}})
		}
		groups = append(groups, fix.Group{ID: fmt.Sprintf("MD050-bold-%d", f.Opening.Start()), Rule: "MD050", Provenance: "MD050 bold delimiter style", Anchor: f.Opening, Edits: edits, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return groups, c.Checkpoint(ctx, 0)
}

// rewritableFacts indexes the round's rewritable Strong facts by opening start.
func rewritableFacts(ctx context.Context, c fix.RoundContext) (map[int]parser.DelimiterFact, byte, error) {
	facts, expected, err := c.BoldMarkerCorrections(ctx)
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
	return out, expected, c.Checkpoint(ctx, 0)
}

// boldRound holds the facts every MD050 group of a round shares. It is computed
// once, so proof cost does not grow with the group count.
type boldRound struct {
	found     map[int]parser.DelimiterFact
	expected  byte
	preserved bool
}

// Checker returns the MD050 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD050", Check: checkBoldMarkers}
}

// checkBoldMarkers authorizes one group against the before parse, trusting
// neither producer data nor fix flags. Whole-candidate proof runs once for the
// round. Directive structure is compared by the validator's shared
// preservation, and rewriting Strong markers outside comments cannot create a
// directive finding, so no ForbidNewFindings is registered.
func checkBoldMarkers(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("bold-markers", func() (any, error) { return boldMarkerProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(boldRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD050" || len(g.Edits) != 2 {
		return fix.Unsafe, nil
	}
	f, ok := round.found[g.Edits[0].Span.Start()]
	if !ok || g.Anchor != f.Opening {
		return fix.Unsafe, nil
	}
	for i, s := range [2]parser.Span{f.Opening, f.Closing} {
		e := g.Edits[i]
		if e.Span != s || string(e.Replacement) != string([]byte{round.expected, round.expected}) {
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

// boldMarkerProof recomputes the rewritable facts from the before parse, proves
// the combined candidate byte for byte and then compares both parses, letting
// Strong nodes differ only in their marker.
func boldMarkerProof(ctx context.Context, c fix.CheckContext) (boldRound, error) {
	failed := boldRound{}
	found, expected, err := rewritableFacts(ctx, c.Before)
	if err != nil {
		return failed, err
	}
	if unchanged, err := c.OnlyEditsChanged(ctx); err != nil || !unchanged {
		return failed, err
	}
	if same, err := c.MeaningPreservedAcrossBoldMarkers(ctx); err != nil || !same {
		return failed, err
	}
	return boldRound{found, expected, true}, c.Before.Checkpoint(ctx, 0)
}
