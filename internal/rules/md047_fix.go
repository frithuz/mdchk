package rules

import (
	"bytes"
	"context"
	"fmt"
	"github.com/frithuz/mdchk/internal/fix"
)

// Corrections regenerates the first production correction from each round's
// actual MD047 findings, never from saved input coordinates or private analysis.
func Corrections(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
	analysis, err := c.BoundedAnalysis(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range analysis.Diagnostics {
		if err := c.Checkpoint(ctx, 1); err != nil {
			return nil, err
		}
		if d.Rule != "MD047" {
			continue
		}
		span, err := c.Current().Span(d.ByteRange.Start, d.ByteRange.End)
		if err != nil {
			return nil, err
		}
		expected, _ := c.Current().Text(span)
		var replacement []byte
		if span.Start() == span.End() {
			replacement, err = fix.InsertionEnding(ctx, c.Current(), c.Lines(), span.Start(), c.Checkpoint)
			if err != nil {
				return nil, err
			}
		}
		if err := c.Reserve(ctx, int64(len(expected))+128, 12); err != nil {
			return nil, err
		}
		return []fix.Group{{ID: fmt.Sprintf("MD047-terminal-%d", span.Start()), Rule: "MD047", Provenance: "MD047 physical terminal obligation", Anchor: span, Edits: []fix.Edit{{Span: span, Expected: []byte(expected), Replacement: replacement}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}}}, nil
	}
	return nil, c.Checkpoint(ctx, 0)
}

// CorrectionCheckers registers trusted proof separately from producer data.
func CorrectionCheckers() []fix.CheckerRegistration {
	return []fix.CheckerRegistration{{Rule: "MD047", Check: checkTerminalNewline, ForbidNewFindings: []string{"directive"}}}
}

// checkTerminalNewline proves an exact minimal suffix transformation, preserving
// all body bytes, every earlier ending, spaces and hard-break markers. Complete
// shared protection/binding/directive comparisons are additionally mandatory in
// Validator; a matching AST shape or disappearance of MD047 is never this proof.
func checkTerminalNewline(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
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
	if g.Rule != "MD047" || g.Anchor != span || len(g.Edits) != 1 || len(edits) != 1 {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	actual := edits[0].Edit
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
	prefix, _ := c.Before.Current().Span(0, span.Start())
	body, _ := c.Before.Current().Text(prefix)
	all, _ := c.After.Current().Span(0, c.After.Current().Len())
	after, _ := c.After.Current().Text(all)
	if len(after) != len(body)+len(replacement) || after[:len(body)] != body || after[len(body):] != string(replacement) {
		return fix.Unsafe, nil
	}
	if err := c.Before.Checkpoint(ctx, 0); err != nil {
		return fix.Unknown, err
	}
	return fix.Preserved, nil
}
