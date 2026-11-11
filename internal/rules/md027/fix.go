package md027

import (
	"context"
	"fmt"
	"strings"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// spacesNotProven is the deferral reason for an excess range holding anything
// but ASCII SPACEs, which stays manual.
const spacesNotProven = "meaning-not-proven"

// Correction regenerates one deletion group per fixable MD027 finding from the
// round's own parse. Each edit removes exactly the excess spaces after the
// prose-facing quote marker, so one SPACE stays. A finding the round's facts no
// longer report gets no group.
func Correction(ctx context.Context, c fix.RoundContext, diagnostics []result.Diagnostic) ([]fix.Group, error) {
	if len(diagnostics) == 0 {
		return nil, nil
	}
	spans, err := c.BlockquoteSpaces(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Reserve(ctx, int64(len(spans))*2+1, int64(len(spans))+int64(len(diagnostics))*12); err != nil {
		return nil, err
	}
	byStart := make(map[int]parser.Span, len(spans))
	for _, s := range spans {
		byStart[s.Start()] = s
	}
	out := make([]fix.Group, 0, len(diagnostics))
	for _, d := range diagnostics {
		s, ok := byStart[d.ByteRange.Start]
		if !ok || s.End() != d.ByteRange.End {
			continue
		}
		expected, err := excessSpaces(ctx, c, s)
		if err != nil {
			return nil, err
		}
		group := fix.Group{ID: fmt.Sprintf("MD027-line-%d", d.Range.Start.Line), Rule: "MD027", Provenance: "MD027 excess blockquote marker spaces", Anchor: s, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}}
		if expected == "" {
			group.DeferReason = spacesNotProven
		} else {
			group.Edits = []fix.Edit{{Span: s, Expected: []byte(expected)}}
		}
		out = append(out, group)
	}
	return out, c.Checkpoint(ctx, 0)
}

// excessSpaces returns the span's bytes, or "" when they are not all ASCII SPACEs.
func excessSpaces(ctx context.Context, c fix.RoundContext, s parser.Span) (string, error) {
	if err := c.Reserve(ctx, int64(s.End()-s.Start())+1, 1); err != nil {
		return "", err
	}
	text, err := c.Current().Text(s)
	if err != nil {
		return "", err
	}
	if text == "" || strings.Trim(text, " ") != "" {
		return "", nil
	}
	return text, nil
}

// blockquoteRound holds the facts every MD027 group of a round shares. It is
// computed once, so proof cost does not grow with the group count.
type blockquoteRound struct {
	spans     map[int]parser.Span
	preserved bool
}

// Checker returns the MD027 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD027", Check: checkBlockquoteSpaces}
}

// checkBlockquoteSpaces authorizes one group against the before parse, trusting
// neither producer data nor fix flags. Whole-candidate proof runs once for the
// round. Shared protection, binding and directive checks remain mandatory in
// Validator, and deleting spaces outside comments cannot create a directive
// finding, so no ForbidNewFindings is registered.
func checkBlockquoteSpaces(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("blockquote-spaces", func() (any, error) { return blockquoteSpacesProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(blockquoteRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD027" || len(g.Edits) != 1 || len(g.Edits[0].Replacement) != 0 {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	s, ok := round.spans[e.Span.Start()]
	if !ok || s != e.Span || g.Anchor != s {
		return fix.Unsafe, nil
	}
	expected, err := excessSpaces(ctx, c.Before, s)
	if err != nil {
		return fix.Unknown, err
	}
	if expected == "" || string(e.Expected) != expected {
		return fix.Unsafe, nil
	}
	return fix.Preserved, nil
}

// blockquoteSpacesProof recomputes the excess ranges from the before parse,
// proves the combined candidate byte for byte and then compares both parses:
// block kinds, nesting, quote and list structure, list content columns and every
// text value must match, so a continuation never becomes code or the reverse.
func blockquoteSpacesProof(ctx context.Context, c fix.CheckContext) (blockquoteRound, error) {
	failed := blockquoteRound{}
	spans, err := c.Before.BlockquoteSpaces(ctx)
	if err != nil {
		return failed, err
	}
	if err := c.Before.Reserve(ctx, int64(len(spans))*2+1, int64(len(spans))+1); err != nil {
		return failed, err
	}
	round := blockquoteRound{spans: make(map[int]parser.Span, len(spans))}
	for _, s := range spans {
		round.spans[s.Start()] = s
	}
	if unchanged, err := c.OnlyEditsChanged(ctx); err != nil || !unchanged {
		return failed, err
	}
	if same, err := c.MeaningPreservedAcrossLineEndWhitespace(ctx); err != nil || !same {
		return failed, err
	}
	round.preserved = true
	return round, c.Before.Checkpoint(ctx, 0)
}
