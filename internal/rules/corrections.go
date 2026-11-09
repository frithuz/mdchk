package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules/internal/headingblanks"
	"github.com/frithuz/mdchk/internal/rules/internal/headingcase"
	"github.com/frithuz/mdchk/internal/rules/internal/headingindent"
	"github.com/frithuz/mdchk/internal/rules/internal/headingspacing"
	"github.com/frithuz/mdchk/internal/rules/internal/headingstyle"
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
// MD063 waits for every MD009, MD019, MD021, MD022 and MD023 edit that can reach
// proof: their
// checkers prove that visible heading text is unchanged, which a capitalization
// edit breaks. An MD009 suffix touching MD047 does not hold it back, since the
// planner defers that pair as a conflict first. MD063 shares its round with MD047.
//
// A document opening with an unrecognized block gets no MD009 or MD032 group at all,
// not even a deferral. MD019, MD021, MD022 and MD023 are already withheld by their flags, and MD047 stays.
//
// MD003 groups wait for a round with no other group. Their edits touch the same
// heading lines as MD009, MD019, MD021 and MD063, and their checker proves the round's
// edits are exactly the style rewrites, so they share a round only with each other.
// A withheld MD047 edit in turn waits for them.
//
// MD032 groups wait for a round with no MD022 group, and MD023 groups for a round
// with neither. A blank inserted before an indented heading, by MD022 or by MD032
// after a list, starts at the same offset as the indentation deletion, and both
// insertions land at the same offset between a list and a heading, so the planner
// would defer each pair as a conflict in every round.
func Corrections(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
	analysis, err := c.BoundedAnalysis(ctx)
	if err != nil {
		return nil, err
	}
	var terminal *result.Diagnostic
	var spacing, blank, indentation, capitalization, trailing, styles, lists []result.Diagnostic
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
		case "MD003":
			if d.FixAvailable {
				styles = append(styles, d)
			}
		case "MD019", "MD021":
			if d.FixAvailable {
				spacing = append(spacing, d)
			}
		case "MD022":
			if d.FixAvailable {
				blank = append(blank, d)
			}
		case "MD023":
			if d.FixAvailable {
				indentation = append(indentation, d)
			}
		case "MD063":
			if d.FixAvailable {
				capitalization = append(capitalization, d)
			}
		case "MD009":
			if !guarded {
				trailing = append(trailing, d)
			}
		case "MD032":
			if !guarded && d.FixAvailable {
				lists = append(lists, d)
			}
		}
	}
	groups, err := headingspacing.Groups(ctx, c, spacing)
	if err != nil {
		return nil, err
	}
	insertions, err := headingblanks.Groups(ctx, c, blank)
	if err != nil {
		return nil, err
	}
	groups = append(groups, insertions...)
	if len(insertions) == 0 {
		blanks, err := listBlankLineGroups(ctx, c, lists)
		if err != nil {
			return nil, err
		}
		groups = append(groups, blanks...)
		if len(blanks) == 0 {
			indents, err := headingindent.Groups(ctx, c, indentation)
			if err != nil {
				return nil, err
			}
			groups = append(groups, indents...)
		}
	}
	waiting := len(groups) > 0
	touched, blocking := false, waiting
	suffixes, err := trailingWhitespaceGroups(ctx, c, trailing)
	if err != nil {
		return nil, err
	}
	for _, g := range suffixes {
		final := terminal != nil && len(g.Edits) > 0 && g.Anchor.End() == terminal.ByteRange.Start
		waiting = waiting || len(g.Edits) > 0
		touched = touched || final
		blocking = blocking || len(g.Edits) > 0 && !final
	}
	groups = append(groups, suffixes...)
	if !blocking {
		cased, err := headingcase.Groups(ctx, c, capitalization)
		if err != nil {
			return nil, err
		}
		groups = append(groups, cased...)
		waiting = waiting || len(cased) > 0
	}
	eligible := terminal != nil && (terminal.FixAvailable || touched)
	if !waiting && !eligible {
		style, err := headingstyle.Groups(ctx, c, styles)
		if err != nil {
			return nil, err
		}
		waiting = len(style) > 0
		groups = append(groups, style...)
	}
	if terminal != nil && (eligible || !waiting) {
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
		{Rule: "MD019", Check: headingspacing.Check},
		{Rule: "MD021", Check: headingspacing.Check},
		{Rule: "MD023", Check: headingindent.Check},
		{Rule: "MD009", Check: checkTrailingWhitespace},
		{Rule: "MD003", Check: headingstyle.Check},
		{Rule: "MD022", Check: headingblanks.Check},
		{Rule: "MD063", Check: headingcase.Check},
		{Rule: "MD032", Check: checkListBlankLine},
	}
}
