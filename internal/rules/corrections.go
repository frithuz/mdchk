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
// withheld waits until no heading edit remains. It is then proposed alone, and its
// deferral costs nothing but itself. An eligible one shares the first round.
func Corrections(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
	analysis, err := c.BoundedAnalysis(ctx)
	if err != nil {
		return nil, err
	}
	var terminal *result.Diagnostic
	var spacing []result.Diagnostic
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
		}
	}
	groups, err := headingSpacingGroups(ctx, c, spacing)
	if err != nil {
		return nil, err
	}
	if terminal != nil && (terminal.FixAvailable || len(groups) == 0) {
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
	}
}
