package md085

import (
	"bytes"
	"context"
	"fmt"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// Correction regenerates one atomic group per active MD085 finding from the
// round's own parse. Each group deletes exactly the reported surplus SPACE/TAB
// indentation of one paragraph continuation line. Byte offsets identify groups.
func Correction(ctx context.Context, c fix.RoundContext, diagnostics []result.Diagnostic) ([]fix.Group, error) {
	findings := len(diagnostics)
	if findings == 0 {
		return nil, nil
	}
	spans, err := c.ParagraphIndentation(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Reserve(ctx, int64(len(spans))*2+1, int64(len(spans))+int64(findings)*12); err != nil {
		return nil, err
	}
	byStart := make(map[int]parser.Span, len(spans))
	for _, s := range spans {
		byStart[s.Start()] = s
	}
	out := make([]fix.Group, 0, findings)
	for _, d := range diagnostics {
		s, ok := byStart[d.ByteRange.Start]
		if !ok || s.End() != d.ByteRange.End {
			return nil, fmt.Errorf("MD085 finding at byte %d has no paragraph indentation fact", d.ByteRange.Start)
		}
		expected, err := indentationText(ctx, c, s)
		if err != nil {
			return nil, err
		}
		out = append(out, fix.Group{
			ID: fmt.Sprintf("MD085-byte-%d", s.Start()), Rule: "MD085", Provenance: "MD085 paragraph continuation indentation", Anchor: s,
			Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}},
			Edits:        []fix.Edit{{Span: s, Expected: expected, Replacement: []byte{}}},
		})
	}
	return out, c.Checkpoint(ctx, 0)
}

// indentationText returns the current bytes of one surplus span, charging them
// first.
func indentationText(ctx context.Context, c fix.RoundContext, s parser.Span) ([]byte, error) {
	if err := c.Reserve(ctx, int64(s.End()-s.Start())+2, 2); err != nil {
		return nil, err
	}
	text, err := c.Current().Text(s)
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

// indentationRound holds the facts every MD085 group of a round shares. It is
// computed once, so proof cost does not grow with the group count.
type indentationRound struct {
	spans     map[int]parser.Span
	edits     map[int]fix.Edit
	preserved bool
}

// Checker returns the MD085 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD085", Check: checkParagraphIndentation}
}

// checkParagraphIndentation authorizes one group against the before parse,
// trusting neither producer data nor fix flags. Whole-candidate proof runs once
// for the round. Shared protection, binding and directive checks remain
// mandatory in Validator, and deleting continuation indentation inside a
// paragraph cannot create a directive finding, so no ForbidNewFindings is
// registered.
func checkParagraphIndentation(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("paragraph-indentation", func() (any, error) { return paragraphIndentationProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(indentationRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD085" || len(g.Edits) != 1 {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	s, ok := round.spans[e.Span.Start()]
	actual, applied := round.edits[e.Span.Start()]
	if !ok || !applied || s != e.Span || g.Anchor != s || actual.Span != s {
		return fix.Unsafe, nil
	}
	expected, err := indentationText(ctx, c.Before, s)
	if err != nil {
		return fix.Unknown, err
	}
	if !bytes.Equal(e.Expected, expected) || len(e.Replacement) != 0 || len(actual.Replacement) != 0 {
		return fix.Unsafe, nil
	}
	return fix.Preserved, nil
}

// paragraphIndentationProof recomputes the expected spans from the before parse,
// proves the combined candidate byte for byte and then compares both parses:
// paragraph structure and visible text must match across the deletion.
func paragraphIndentationProof(ctx context.Context, c fix.CheckContext) (indentationRound, error) {
	failed := indentationRound{}
	spans, err := c.Before.ParagraphIndentation(ctx)
	if err != nil {
		return failed, err
	}
	_, edits, err := c.BoundedViews(ctx)
	if err != nil {
		return failed, err
	}
	if err := c.Before.Reserve(ctx, int64(len(spans)+len(edits))*2+1, int64(len(spans)+len(edits))+1); err != nil {
		return failed, err
	}
	round := indentationRound{spans: make(map[int]parser.Span, len(spans)), edits: make(map[int]fix.Edit, len(edits))}
	for _, s := range spans {
		round.spans[s.Start()] = s
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
	round.preserved = true
	return round, c.Before.Checkpoint(ctx, 0)
}
