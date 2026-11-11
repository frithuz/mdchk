package md044

import (
	"context"
	"fmt"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// occurrence is one unambiguous configured-name occurrence of a parse.
type occurrence struct {
	span        parser.Span
	replacement string
}

// Correction regenerates one replacement group per fix-available MD044 finding
// from the round's own analysis and parse. Each group replaces exactly the
// reported occurrence with its configured spelling; a finding without a matching
// unambiguous occurrence of this parse gets none.
func Correction(ctx context.Context, c fix.RoundContext, diagnostics []result.Diagnostic) ([]fix.Group, error) {
	if len(diagnostics) == 0 {
		return nil, nil
	}
	found, err := occurrences(ctx, c)
	if err != nil {
		return nil, err
	}
	groups := make([]fix.Group, 0, len(diagnostics))
	for _, d := range diagnostics {
		if err := c.Reserve(ctx, 128, 12); err != nil {
			return nil, err
		}
		o, ok := found[d.ByteRange.Start]
		if !d.FixAvailable || !ok || o.span.End() != d.ByteRange.End {
			continue
		}
		expected, err := c.Current().Text(o.span)
		if err != nil {
			return nil, err
		}
		groups = append(groups, fix.Group{ID: fmt.Sprintf("MD044-name-%d", o.span.Start()), Rule: "MD044", Provenance: "MD044 configured name capitalization", Anchor: o.span, Edits: []fix.Edit{{Span: o.span, Expected: []byte(expected), Replacement: []byte(o.replacement)}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return groups, c.Checkpoint(ctx, 0)
}

// occurrences indexes the round's unambiguous occurrences by start.
func occurrences(ctx context.Context, c fix.RoundContext) (map[int]occurrence, error) {
	spans, replacements, err := c.ConfiguredNameCorrections(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Reserve(ctx, int64(len(spans))*2+1, int64(len(spans))+1); err != nil {
		return nil, err
	}
	out := make(map[int]occurrence, len(spans))
	for i, s := range spans {
		if err := c.Checkpoint(ctx, int64(s.End()-s.Start()+len(replacements[i]))+1); err != nil {
			return nil, err
		}
		if replacements[i] == "" {
			continue
		}
		out[s.Start()] = occurrence{s, replacements[i]}
	}
	return out, nil
}

// nameRound holds the facts every MD044 group of a round shares. It is computed
// once, so proof cost does not grow with the group count.
type nameRound struct {
	found     map[int]occurrence
	preserved bool
}

// Checker returns the MD044 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD044", Check: checkConfiguredNames}
}

// checkConfiguredNames authorizes one group against the before parse, trusting
// neither producer data nor fix flags. Whole-candidate proof runs once for the
// round. Directive structure is compared by the validator's shared preservation,
// and changing letter case outside comments cannot create a directive finding, so
// no ForbidNewFindings is registered.
func checkConfiguredNames(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("configured-names", func() (any, error) { return configuredNameProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(nameRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD044" || len(g.Edits) != 1 {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	o, ok := round.found[e.Span.Start()]
	if !ok || o.span != e.Span || g.Anchor != o.span || string(e.Replacement) != o.replacement {
		return fix.Unsafe, nil
	}
	expected, err := c.Before.Current().Text(o.span)
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

// configuredNameProof recomputes the occurrences from the before parse, proves
// the combined candidate byte for byte and then compares both parses, letting
// Text and code values differ only where their case folds match.
func configuredNameProof(ctx context.Context, c fix.CheckContext) (nameRound, error) {
	failed := nameRound{}
	found, err := occurrences(ctx, c.Before)
	if err != nil {
		return failed, err
	}
	if unchanged, err := c.OnlyEditsChanged(ctx); err != nil || !unchanged {
		return failed, err
	}
	if same, err := c.MeaningPreservedAcrossNameCase(ctx); err != nil || !same {
		return failed, err
	}
	return nameRound{found, true}, c.Before.Checkpoint(ctx, 0)
}
