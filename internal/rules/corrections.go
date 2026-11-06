package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/result"
)

// Corrections regenerates every eligible production correction from each round's
// actual findings, never from saved input coordinates or private analysis.
//
// A failing checker defers the whole round, so an MD047 edit that analysis already
// withheld waits until no other edit remains. It is then proposed alone, and its
// deferral costs nothing but itself. An eligible one shares the first round. So does
// one an MD009 suffix touches: the planner defers that pair as a conflict before any
// proof, and the suffix must not be corrected without it.
//
// A document opening with an unrecognized block gets no MD009 group at all, not even
// a deferral. MD019 and MD021 are already withheld by their flags, and MD047 stays.
func Corrections(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
	analysis, err := c.BoundedAnalysis(ctx)
	if err != nil {
		return nil, err
	}
	var terminal *result.Diagnostic
	var spacing, trailing []result.Diagnostic
	guarded := c.Parsed().Document.UnrecognizedBlock()
	for i, d := range analysis.Diagnostics {
		if err := c.Checkpoint(ctx, 1); err != nil {
			return nil, err
		}
		switch d.Rule {
		case "MD047":
			if terminal == nil {
				terminal = &analysis.Diagnostics[i]
			}
		case "MD019", "MD021":
			if d.FixAvailable {
				spacing = append(spacing, d)
			}
		case "MD009":
			if !guarded {
				trailing = append(trailing, d)
			}
		}
	}
	groups, err := headingSpacingGroups(ctx, c, spacing)
	if err != nil {
		return nil, err
	}
	waiting := len(groups) > 0
	touched := false
	suffixes, err := trailingWhitespaceGroups(ctx, c, trailing)
	if err != nil {
		return nil, err
	}
	for _, g := range suffixes {
		waiting = waiting || len(g.Edits) > 0
		touched = touched || terminal != nil && len(g.Edits) > 0 && g.Anchor.End() == terminal.ByteRange.Start
	}
	groups = append(groups, suffixes...)
	if terminal != nil && (terminal.FixAvailable || touched || !waiting) {
		group, err := terminalNewlineGroup(ctx, c, *terminal)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, c.Checkpoint(ctx, 0)
}

// CorrectionCheckers registers trusted proof separately from producer data.
func CorrectionCheckers() []fix.CheckerRegistration {
	return []fix.CheckerRegistration{
		{Rule: "MD047", Check: checkTerminalNewline, ForbidNewFindings: []string{"directive"}},
		{Rule: "MD019", Check: checkHeadingSpacing},
		{Rule: "MD021", Check: checkHeadingSpacing},
		{Rule: "MD009", Check: checkTrailingWhitespace},
	}
}
