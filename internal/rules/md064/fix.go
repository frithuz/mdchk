package md064

import (
	"context"
	"fmt"
	"strings"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// Correction regenerates one deletion group per fix-available MD064 finding from
// the round's own analysis and parse. Each group deletes exactly the reported
// excess SPACE span; a finding without a matching fact of this parse gets none.
func Correction(ctx context.Context, c fix.RoundContext, diagnostics []result.Diagnostic) ([]fix.Group, error) {
	if len(diagnostics) == 0 {
		return nil, nil
	}
	spans, err := c.RepeatedProseSpaces(ctx)
	if err != nil {
		return nil, err
	}
	excess, err := excessSpans(ctx, c, spans)
	if err != nil {
		return nil, err
	}
	groups := make([]fix.Group, 0, len(diagnostics))
	for _, d := range diagnostics {
		if err := c.Reserve(ctx, 128, 12); err != nil {
			return nil, err
		}
		s, ok := excess[d.ByteRange.Start]
		if !d.FixAvailable || !ok || s.End() != d.ByteRange.End {
			continue
		}
		expected, err := c.Current().Text(s)
		if err != nil {
			return nil, err
		}
		groups = append(groups, fix.Group{ID: fmt.Sprintf("MD064-spaces-%d", s.Start()), Rule: "MD064", Provenance: "MD064 repeated prose spaces", Anchor: s, Edits: []fix.Edit{{Span: s, Expected: []byte(expected)}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return groups, c.Checkpoint(ctx, 0)
}

// excessSpans indexes the round's excess spans by start, keeping only those made
// up entirely of ASCII SPACE bytes.
func excessSpans(ctx context.Context, c fix.RoundContext, spans []parser.Span) (map[int]parser.Span, error) {
	if err := c.Reserve(ctx, int64(len(spans))*2+1, int64(len(spans))+1); err != nil {
		return nil, err
	}
	out := make(map[int]parser.Span, len(spans))
	for _, s := range spans {
		if err := c.Checkpoint(ctx, int64(s.End()-s.Start())+1); err != nil {
			return nil, err
		}
		text, err := c.Current().Text(s)
		if err != nil {
			return nil, err
		}
		if text == "" || strings.Trim(text, " ") != "" {
			continue
		}
		out[s.Start()] = s
	}
	return out, nil
}

// proseSpaceRound holds the facts every MD064 group of a round shares. It is
// computed once, so proof cost does not grow with the group count.
type proseSpaceRound struct {
	excess    map[int]parser.Span
	preserved bool
}

// Checker returns the MD064 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD064", Check: checkProseSpaces}
}

// checkProseSpaces authorizes one group against the before parse, trusting
// neither producer data nor fix flags. Whole-candidate proof runs once for the
// round. Directive structure is compared by the validator's shared preservation,
// and deleting spaces outside comments cannot create a directive finding, so no
// ForbidNewFindings is registered.
func checkProseSpaces(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("repeated-prose-spaces", func() (any, error) { return proseSpaceProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(proseSpaceRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD064" || len(g.Edits) != 1 || len(g.Edits[0].Replacement) != 0 {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	s, ok := round.excess[e.Span.Start()]
	if !ok || s != e.Span || g.Anchor != s {
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
	return fix.Preserved, nil
}

// proseSpaceProof recomputes the expected spans from the before parse, proves
// the combined candidate byte for byte and then compares both parses.
func proseSpaceProof(ctx context.Context, c fix.CheckContext) (proseSpaceRound, error) {
	failed := proseSpaceRound{}
	spans, err := c.Before.RepeatedProseSpaces(ctx)
	if err != nil {
		return failed, err
	}
	excess, err := excessSpans(ctx, c.Before, spans)
	if err != nil {
		return failed, err
	}
	if unchanged, err := c.OnlyEditsChanged(ctx); err != nil || !unchanged {
		return failed, err
	}
	if same, err := c.MeaningPreservedAcrossProseSpaces(ctx); err != nil || !same {
		return failed, err
	}
	return proseSpaceRound{excess, true}, c.Before.Checkpoint(ctx, 0)
}
