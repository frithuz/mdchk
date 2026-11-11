package md035

import (
	"bytes"
	"context"
	"fmt"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// Correction regenerates one atomic group per active MD035 finding from the
// round's own analysis and parse. Each group replaces only the finding's
// first-to-last marker envelope, so indentation, container prefixes, trailing
// whitespace and line endings are kept. Directive intervals and protection are
// enforced through the planner.
func Correction(ctx context.Context, c fix.RoundContext, diagnostics []result.Diagnostic) ([]fix.Group, error) {
	findings := len(diagnostics)
	if findings == 0 {
		return nil, nil
	}
	envelopes, replacement, err := c.ThematicStyle(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Reserve(ctx, int64(len(envelopes))*2+1, int64(len(envelopes))+int64(findings)*12); err != nil {
		return nil, err
	}
	byStart := make(map[int]parser.Span, len(envelopes))
	for _, s := range envelopes {
		byStart[s.Start()] = s
	}
	out := make([]fix.Group, 0, findings)
	for _, d := range diagnostics {
		envelope, ok := byStart[d.ByteRange.Start]
		if !ok || envelope.End() != d.ByteRange.End {
			return nil, fmt.Errorf("MD035 finding at byte %d has no thematic style fact", d.ByteRange.Start)
		}
		edit, err := envelopeEdit(ctx, c, envelope, replacement)
		if err != nil {
			return nil, err
		}
		out = append(out, fix.Group{ID: fmt.Sprintf("MD035-line-%d", d.Range.Start.Line), Rule: "MD035", Provenance: "MD035 horizontal rule marker", Anchor: envelope, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}, Edits: []fix.Edit{edit}})
	}
	return out, c.Checkpoint(ctx, 0)
}

// envelopeEdit replaces one original marker envelope with the round's
// replacement.
func envelopeEdit(ctx context.Context, c fix.RoundContext, envelope parser.Span, replacement []byte) (fix.Edit, error) {
	text, err := c.Current().Text(envelope)
	if err != nil {
		return fix.Edit{}, err
	}
	if err := c.Reserve(ctx, int64(len(text)+len(replacement))+1, 1); err != nil {
		return fix.Edit{}, err
	}
	return fix.Edit{Span: envelope, Expected: []byte(text), Replacement: bytes.Clone(replacement)}, nil
}

// styleRound holds the facts every MD035 group of a round shares. It is
// computed once, so proof cost does not grow with the group count.
type styleRound struct {
	envelopes   map[int]parser.Span
	edits       map[int]fix.Edit
	replacement []byte
	preserved   bool
}

// Checker returns the MD035 candidate checker. A replaced envelope can turn a
// break into a Setext underline or a list item, so a candidate that changes
// any block is rejected and the finding stays manual.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD035", Check: checkThematicStyle}
}

// checkThematicStyle authorizes one group against the before parse, trusting
// neither producer data nor fix flags. Whole-candidate proof runs once a round.
func checkThematicStyle(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("thematic-style", func() (any, error) { return thematicStyleProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(styleRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD035" || len(g.Edits) != 1 {
		return fix.Unsafe, nil
	}
	envelope, ok := round.envelopes[g.Edits[0].Span.Start()]
	if !ok || g.Anchor != envelope {
		return fix.Unsafe, nil
	}
	want, err := envelopeEdit(ctx, c.Before, envelope, round.replacement)
	if err != nil {
		return fix.Unknown, err
	}
	actual, applied := round.edits[want.Span.Start()]
	got := g.Edits[0]
	if !applied || got.Span != want.Span || actual.Span != want.Span || !bytes.Equal(got.Expected, want.Expected) || !bytes.Equal(got.Replacement, want.Replacement) || !bytes.Equal(actual.Replacement, want.Replacement) {
		return fix.Unsafe, nil
	}
	return fix.Preserved, nil
}

// thematicStyleProof recomputes the envelopes from the before parse, proves the
// combined candidate byte for byte and then compares the block structure and
// meaning of both parses, so every edited line is still the same thematic break.
func thematicStyleProof(ctx context.Context, c fix.CheckContext) (styleRound, error) {
	failed := styleRound{}
	envelopes, replacement, err := c.Before.ThematicStyle(ctx)
	if err != nil {
		return failed, err
	}
	_, edits, err := c.BoundedViews(ctx)
	if err != nil {
		return failed, err
	}
	if err := c.Before.Reserve(ctx, int64(len(envelopes)+len(edits))*2+1, int64(len(envelopes)+len(edits))+1); err != nil {
		return failed, err
	}
	round := styleRound{envelopes: make(map[int]parser.Span, len(envelopes)), edits: make(map[int]fix.Edit, len(edits)), replacement: replacement}
	for _, s := range envelopes {
		round.envelopes[s.Start()] = s
	}
	for _, e := range edits {
		round.edits[e.Edit.Span.Start()] = e.Edit
	}
	if unchanged, err := c.OnlyEditsChanged(ctx); err != nil || !unchanged {
		return failed, err
	}
	if same, err := c.MeaningPreservedAcrossLineEndWhitespace(ctx); err != nil || !same {
		return failed, err
	}
	if same, err := c.SameStructureAcrossBullets(ctx); err != nil || !same {
		return failed, err
	}
	round.preserved = true
	return round, c.Before.Checkpoint(ctx, 0)
}
