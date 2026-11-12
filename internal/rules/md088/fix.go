package md088

import (
	"context"
	"fmt"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
)

// Correction regenerates one replacement group per fix-available MD088 finding
// from the round's own analysis and parse. Each group replaces exactly the
// reported scalar with its ASCII spelling; a finding without a matching
// rewritable fact of this parse gets none.
func Correction(ctx context.Context, c fix.RoundContext, diagnostics []result.Diagnostic) ([]fix.Group, error) {
	if len(diagnostics) == 0 {
		return nil, nil
	}
	found, err := rewritableFacts(ctx, c)
	if err != nil {
		return nil, err
	}
	groups := make([]fix.Group, 0, len(diagnostics))
	for _, d := range diagnostics {
		if err := c.Reserve(ctx, 128, 12); err != nil {
			return nil, err
		}
		f, ok := found[d.ByteRange.Start]
		if !d.FixAvailable || !ok || f.Span.End() != d.ByteRange.End {
			continue
		}
		expected, err := c.Current().Text(f.Span)
		if err != nil {
			return nil, err
		}
		groups = append(groups, fix.Group{ID: fmt.Sprintf("MD088-%d", f.Span.Start()), Rule: "MD088", Provenance: "MD088 ASCII quotes and dashes", Anchor: f.Span, Edits: []fix.Edit{{Span: f.Span, Expected: []byte(expected), Replacement: []byte(parser.ASCIIPunctuationReplacement(f.Scalar))}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return groups, c.Checkpoint(ctx, 0)
}

// rewritableFacts indexes the round's rewritable scalar facts by start.
func rewritableFacts(ctx context.Context, c fix.RoundContext) (map[int]parser.ASCIIPunctuationFact, error) {
	facts, err := c.ASCIIPunctuationCorrections(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Reserve(ctx, int64(len(facts))+1, int64(len(facts))+1); err != nil {
		return nil, err
	}
	out := make(map[int]parser.ASCIIPunctuationFact, len(facts))
	for _, f := range facts {
		out[f.Span.Start()] = f
	}
	return out, c.Checkpoint(ctx, 0)
}

// punctuationRound holds the facts every MD088 group of a round shares. It is
// computed once, so proof cost does not grow with the group count.
type punctuationRound struct {
	found     map[int]parser.ASCIIPunctuationFact
	preserved bool
}

// Checker returns the MD088 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD088", Check: checkASCIIPunctuation}
}

// checkASCIIPunctuation authorizes one group against the before parse, trusting
// neither producer data nor fix flags. Whole-candidate proof runs once for the
// round. Directive structure is compared by the validator's shared
// preservation, and rewriting prose scalars outside comments cannot create a
// directive finding, so no ForbidNewFindings is registered.
func checkASCIIPunctuation(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("ascii-punctuation", func() (any, error) { return asciiPunctuationProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(punctuationRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD088" || len(g.Edits) != 1 {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	f, ok := round.found[e.Span.Start()]
	if !ok || f.Span != e.Span || g.Anchor != f.Span || string(e.Replacement) != parser.ASCIIPunctuationReplacement(f.Scalar) {
		return fix.Unsafe, nil
	}
	expected, err := c.Before.Current().Text(f.Span)
	if err != nil {
		return fix.Unknown, err
	}
	if err := c.Before.Checkpoint(ctx, int64(len(expected))+1); err != nil {
		return fix.Unknown, err
	}
	if string(e.Expected) != expected {
		return fix.Unsafe, nil
	}
	return fix.Preserved, nil
}

// asciiPunctuationProof recomputes the rewritable facts from the before parse,
// proves the combined candidate byte for byte, compares both parses, letting
// Text values differ only where a scalar became its ASCII spelling, and requires
// the same heading anchor bases.
func asciiPunctuationProof(ctx context.Context, c fix.CheckContext) (punctuationRound, error) {
	failed := punctuationRound{}
	found, err := rewritableFacts(ctx, c.Before)
	if err != nil {
		return failed, err
	}
	if unchanged, err := c.OnlyEditsChanged(ctx); err != nil || !unchanged {
		return failed, err
	}
	if same, err := c.MeaningPreservedAcrossASCIIPunctuation(ctx); err != nil || !same {
		return failed, err
	}
	if same, err := sameHeadingAnchors(ctx, c); err != nil || !same {
		return failed, err
	}
	return punctuationRound{found, true}, c.Before.Checkpoint(ctx, 0)
}

// sameHeadingAnchors reports whether both round parses give every heading the
// same anchor base.
func sameHeadingAnchors(ctx context.Context, c fix.CheckContext) (bool, error) {
	before, availability, err := c.Before.HeadingAnchors(ctx)
	if err != nil || availability != resource.Available {
		return false, err
	}
	after, availability, err := c.After.HeadingAnchors(ctx)
	if err != nil || availability != resource.Available {
		return false, err
	}
	if len(before) != len(after) {
		return false, nil
	}
	for i, b := range before {
		if err := c.Before.Checkpoint(ctx, int64(len(b.Base.Value)+len(after[i].Base.Value))+1); err != nil {
			return false, err
		}
		if b.Base != after[i].Base {
			return false, nil
		}
	}
	return true, nil
}
