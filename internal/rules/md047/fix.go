package md047

import (
	"bytes"
	"context"
	"fmt"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/result"
)

// Correction regenerates the MD047 group from the round's actual finding.
func Correction(ctx context.Context, c fix.RoundContext, d result.Diagnostic) (fix.Group, error) {
	span, err := c.Current().Span(d.ByteRange.Start, d.ByteRange.End)
	if err != nil {
		return fix.Group{}, err
	}
	expected, _ := c.Current().Text(span)
	var replacement []byte
	if span.Start() == span.End() {
		replacement, err = fix.InsertionEnding(ctx, c.Current(), c.Lines(), span.Start(), c.Checkpoint)
		if err != nil {
			return fix.Group{}, err
		}
	}
	if err := c.Reserve(ctx, int64(len(expected))+128, 12); err != nil {
		return fix.Group{}, err
	}
	return fix.Group{ID: fmt.Sprintf("MD047-terminal-%d", span.Start()), Rule: "MD047", Provenance: "MD047 physical terminal obligation", Anchor: span, Edits: []fix.Edit{{Span: span, Expected: []byte(expected), Replacement: replacement}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}}, nil
}

// Checker returns the MD047 candidate checker. A new directive finding is never
// accepted from an MD047 edit.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD047", Check: checkTerminal, ForbidNewFindings: []string{"directive"}}
}

// checkTerminal proves an exact minimal suffix transformation, preserving
// all body bytes, every earlier ending, spaces and hard-break markers. Bytes other
// edits in the round change are their own checkers' proof; this one proves that
// nothing else changed. Complete shared protection/binding/directive comparisons
// are additionally mandatory in Validator; a matching AST shape or disappearance
// of MD047 is never this proof.
func checkTerminal(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	span, err := TerminalNewline(ctx, c.Before.Current(), c.Before.Lines(), c.Before.Checkpoint)
	if err != nil {
		return fix.Unknown, err
	}
	if !span.Present() {
		return fix.Unsafe, nil
	}
	eligible, err := c.Before.TerminalEditEligible(ctx, "MD047", span)
	if err != nil {
		return fix.Unknown, err
	}
	if !eligible {
		return fix.Unsafe, nil
	}
	if err := c.Before.Checkpoint(ctx, int64(c.Before.Current().Len()+c.After.Current().Len())*3+32); err != nil {
		return fix.Unknown, err
	}
	g, edits, err := c.BoundedViews(ctx)
	if err != nil {
		return fix.Unknown, err
	}
	if g.Rule != "MD047" || g.Anchor != span || len(g.Edits) != 1 || len(edits) == 0 {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	// Round edits are in source order, so a suffix edit can only be the last one.
	actual := edits[len(edits)-1].Edit
	expected, _ := c.Before.Current().Text(span)
	var replacement []byte
	if span.Start() == span.End() {
		replacement, err = fix.InsertionEnding(ctx, c.Before.Current(), c.Before.Lines(), span.Start(), c.Before.Checkpoint)
		if err != nil {
			return fix.Unknown, err
		}
	}
	if e.Span != span || actual.Span != span || string(e.Expected) != expected || !bytes.Equal(e.Replacement, replacement) || !bytes.Equal(actual.Replacement, replacement) {
		return fix.Unsafe, nil
	}
	unchanged, err := c.OnlyEditsChanged(ctx)
	if err != nil {
		return fix.Unknown, err
	}
	if !unchanged {
		return fix.Unsafe, nil
	}
	// Independently inspect the ACTUAL candidate's task source facts. Neither the
	// producer nor a serialized fix_available flag can authorize task adoption.
	preserved, err := c.TerminalTaskSourcesPreserved(ctx)
	if err != nil {
		return fix.Unknown, err
	}
	if !preserved {
		return fix.Unsafe, nil
	}
	if err := c.Before.Checkpoint(ctx, 0); err != nil {
		return fix.Unknown, err
	}
	return fix.Preserved, nil
}
