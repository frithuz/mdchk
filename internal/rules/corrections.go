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
	"github.com/frithuz/mdchk/internal/rules/internal/listspacing"
	"github.com/frithuz/mdchk/internal/rules/internal/orderedprefix"
	"github.com/frithuz/mdchk/internal/rules/md009"
	"github.com/frithuz/mdchk/internal/rules/md047"
)

// Corrections regenerates every eligible production correction from each round's
// actual findings, never from saved input coordinates or private analysis.
//
// A failing checker defers the whole round, so an MD047 edit that analysis already
// withheld waits until no other edit remains. It is then proposed alone, and its
// deferral costs nothing but itself. An eligible one shares the first round. So does
// one an MD009 suffix touches: the planner accepts the suffix and the final newline
// waits for the next round, since MD047 loses every conflict.
//
// MD063 waits for every MD009, MD019, MD021, MD022 and MD023 edit that can reach
// proof: their
// checkers prove that visible heading text is unchanged, which a capitalization
// edit breaks. MD063 shares its round with MD047.
//
// A document opening with an unrecognized block gets no MD007, MD009 or MD032 group at all,
// not even a deferral. MD004, MD005, MD019, MD021, MD022, MD023, MD029, MD030 and MD077 are already withheld by their flags, and MD047 stays.
//
// MD003 groups wait for a round with no other group. Their edits touch the same
// heading lines as MD009, MD019, MD021 and MD063, and their checker proves the round's
// edits are exactly the style rewrites, so they share a round only with each other.
// A withheld MD047 edit in turn waits for them.
//
// MD032 groups wait for a round with no MD022 group, and MD023 groups for a round
// with neither. A blank inserted before an indented heading, by MD022 or by MD032
// after a list, starts at the same offset as the indentation deletion, and both
// insertions land at the same offset between a list and a heading, so each pair
// would conflict and one edit of it wait for a later round.
//
// MD009, MD022, MD023 and MD032 prove their round by comparing whole parses across
// line-end whitespace, which a moved list marker breaks, so MD005 groups wait for a
// round with no edit from those rules.
//
// MD029 groups wait for a round with no MD022, MD023 or MD032 group. While any
// remain, MD009 suffixes and MD005 groups wait too: their checkers compare list
// items and require every list number to stay unchanged.
//
// MD007 shifts wait for a round with no MD009, MD022, MD023, MD029, MD032 or MD005
// edit: those checkers compare list items or columns that a shift moves. MD063
// waits for MD007 in turn, since the MD007 checker compares whole parses.
//
// MD077 groups wait for a round with no MD007 shift either, since their checker
// compares whole parses and columns those edits move. MD063 waits for MD077 in turn.
//
// MD030 groups wait for a round with no MD022, MD023, MD032 or MD029 group, whose
// checkers compare list items or whole parses that a separator edit moves. While
// any remain, MD005, MD007, MD077, MD063 and MD003 wait in turn. MD009 shares the
// round: its proof tolerates list content indentation, which only MD030 moves.
//
// MD076 groups go first and every other group waits for a round without them: a
// separator edit changes list tightness, which the other list and whole-parse
// proofs reject. Only an MD047 edit that analysis already offers shares the round.
// A document opening with an unrecognized block gets no MD076 group either.
//
// MD004 groups come next and share their round only with MD009, MD019, MD021 and
// MD047; every other group waits for a round without them. MD004 is already
// withheld by its flags in a document opening with an unrecognized block.
func Corrections(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
	analysis, err := c.BoundedAnalysis(ctx)
	if err != nil {
		return nil, err
	}
	var terminal *result.Diagnostic
	var markers, spacing, blank, indentation, capitalization, trailing, styles, lists, alignment, numbering, bullets, continuation, separators, gaps []result.Diagnostic
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
		case "MD004":
			if d.FixAvailable {
				markers = append(markers, d)
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
		case "MD005":
			if d.FixAvailable {
				alignment = append(alignment, d)
			}
		case "MD029":
			if d.FixAvailable {
				numbering = append(numbering, d)
			}
		case "MD077":
			if d.FixAvailable {
				continuation = append(continuation, d)
			}
		case "MD030":
			if d.FixAvailable && !guarded {
				separators = append(separators, d)
			}
		case "MD009":
			if !guarded {
				trailing = append(trailing, d)
			}
		case "MD032":
			if !guarded && d.FixAvailable {
				lists = append(lists, d)
			}
		case "MD007":
			if d.FixAvailable && !guarded {
				bullets = append(bullets, d)
			}
		case "MD076":
			if !guarded && d.FixAvailable {
				gaps = append(gaps, d)
			}
		}
	}
	gapped, err := listspacing.Groups(ctx, c, gaps)
	if err != nil {
		return nil, err
	}
	if len(gapped) > 0 {
		if terminal != nil && terminal.FixAvailable {
			group, err := md047.Correction(ctx, c, *terminal)
			if err != nil {
				return nil, err
			}
			gapped = append(gapped, group)
		}
		return gapped, c.Checkpoint(ctx, 0)
	}
	groups, err := bulletMarkerGroups(ctx, c, markers)
	if err != nil {
		return nil, err
	}
	// Other list and whole-parse proofs compare what MD004 edits, so they wait.
	if len(groups) > 0 {
		styles, blank, indentation, capitalization, lists, alignment, numbering, bullets, continuation, separators = nil, nil, nil, nil, nil, nil, nil, nil, nil, nil
	}
	headings, err := headingspacing.Groups(ctx, c, spacing)
	if err != nil {
		return nil, err
	}
	groups = append(groups, headings...)
	spaced := len(groups)
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
	if len(groups) == spaced {
		prefixes, err := orderedprefix.Groups(ctx, c, numbering)
		if err != nil {
			return nil, err
		}
		groups = append(groups, prefixes...)
		// MD009 and MD005 proofs compare list numbers, so they wait for MD029.
		if len(prefixes) > 0 {
			trailing = nil
		}
	}
	if len(groups) == spaced {
		markers, err := listSeparatorGroups(ctx, c, separators)
		if err != nil {
			return nil, err
		}
		groups = append(groups, markers...)
	}
	waiting := len(groups) > 0
	touched, blocking, compared := false, waiting, len(groups) > spaced
	suffixes, err := md009.Correction(ctx, c, trailing)
	if err != nil {
		return nil, err
	}
	for _, g := range suffixes {
		final := terminal != nil && len(g.Edits) > 0 && g.Anchor.End() == terminal.ByteRange.Start
		waiting = waiting || len(g.Edits) > 0
		touched = touched || final
		blocking = blocking || len(g.Edits) > 0 && !final
		compared = compared || len(g.Edits) > 0
	}
	if !compared {
		aligned, err := listAlignmentGroups(ctx, c, alignment)
		if err != nil {
			return nil, err
		}
		groups = append(groups, aligned...)
		waiting = waiting || len(aligned) > 0
		if len(aligned) == 0 {
			shifts, err := bulletIndentationGroups(ctx, c, bullets)
			if err != nil {
				return nil, err
			}
			groups = append(groups, shifts...)
			waiting = waiting || len(shifts) > 0
			blocking = blocking || len(shifts) > 0
			if len(shifts) == 0 {
				continued, err := listContinuationGroups(ctx, c, continuation)
				if err != nil {
					return nil, err
				}
				groups = append(groups, continued...)
				waiting = waiting || len(continued) > 0
				blocking = blocking || len(continued) > 0
			}
		}
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
		group, err := md047.Correction(ctx, c, *terminal)
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
		md047.Checker(),
		{Rule: "MD019", Check: headingspacing.Check},
		{Rule: "MD021", Check: headingspacing.Check},
		{Rule: "MD023", Check: headingindent.Check},
		{Rule: "MD029", Check: orderedprefix.Check},
		md009.Checker(),
		{Rule: "MD003", Check: headingstyle.Check},
		{Rule: "MD022", Check: headingblanks.Check},
		{Rule: "MD063", Check: headingcase.Check},
		{Rule: "MD032", Check: checkListBlankLine},
		{Rule: "MD005", Check: checkListAlignment},
		{Rule: "MD007", Check: checkBulletIndentation},
		{Rule: "MD077", Check: checkListContinuation},
		{Rule: "MD030", Check: checkListSeparator},
		{Rule: "MD076", Check: listspacing.Check},
		{Rule: "MD004", Check: checkBulletMarker},
	}
}
