package rules

import (
	"context"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules/internal/headingcase"
	"github.com/frithuz/mdchk/internal/rules/internal/listspacing"
	"github.com/frithuz/mdchk/internal/rules/internal/orderedprefix"
	"github.com/frithuz/mdchk/internal/rules/md003"
	"github.com/frithuz/mdchk/internal/rules/md004"
	"github.com/frithuz/mdchk/internal/rules/md005"
	"github.com/frithuz/mdchk/internal/rules/md007"
	"github.com/frithuz/mdchk/internal/rules/md009"
	"github.com/frithuz/mdchk/internal/rules/md010"
	"github.com/frithuz/mdchk/internal/rules/md012"
	"github.com/frithuz/mdchk/internal/rules/md019"
	"github.com/frithuz/mdchk/internal/rules/md021"
	"github.com/frithuz/mdchk/internal/rules/md022"
	"github.com/frithuz/mdchk/internal/rules/md023"
	"github.com/frithuz/mdchk/internal/rules/md027"
	"github.com/frithuz/mdchk/internal/rules/md030"
	"github.com/frithuz/mdchk/internal/rules/md032"
	"github.com/frithuz/mdchk/internal/rules/md035"
	"github.com/frithuz/mdchk/internal/rules/md038"
	"github.com/frithuz/mdchk/internal/rules/md044"
	"github.com/frithuz/mdchk/internal/rules/md047"
	"github.com/frithuz/mdchk/internal/rules/md049"
	"github.com/frithuz/mdchk/internal/rules/md050"
	"github.com/frithuz/mdchk/internal/rules/md064"
	"github.com/frithuz/mdchk/internal/rules/md077"
	"github.com/frithuz/mdchk/internal/rules/md085"
	"github.com/frithuz/mdchk/internal/rules/md089"
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
// A document opening with an unrecognized block gets no MD007, MD009, MD012 or MD032 group at all,
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
// MD010 groups wait for a round with no other edit and take it alone, MD047
// included: every other whole-parse or text proof compares the Text a TAB
// replacement changes, and the MD010 proof compares everything else. Their
// deferrals cost nothing, so they share any round without edits.
//
// MD012 groups wait for a round with no MD004, MD019, MD021, MD022, MD032,
// MD023, MD029, MD030, MD009, MD005, MD007 or MD077 edit. MD009 suffixes sit on
// the whitespace-only lines MD012 removes, and a removed line moves what the
// list and whole-parse proofs compare. MD063, MD010, MD003 and MD027 wait for
// MD012 in turn; an MD047 edit that analysis already offers shares the round.
//
// MD085 groups wait for a round with no group from MD004, MD019, MD021, MD022,
// MD023, MD032, MD029 or MD030, and share it with MD009 and MD047: an indentation
// deletion starts after a line start and an MD009 suffix ends at a line end, so
// they never overlap, and both proofs compare whole parses across line-end
// whitespace, which neither edit changes. While MD085 edits remain, MD005, MD007,
// MD077, MD063 and MD003 wait, since their proofs compare columns or whole parses,
// and MD012, MD010, MD027 and MD064 wait as for any other edit. A document opening with an unrecognized
// block gets no MD085 group.
//
// MD004 groups come next and share their round only with MD009, MD019, MD021 and
// MD047; every other group waits for a round without them. MD004 is already
// withheld by its flags in a document opening with an unrecognized block.
//
// MD027 groups go last, after MD010 and MD003, in a round with no other group
// but an MD047 edit that analysis withheld, which waits for them in turn. Their
// checker compares whole parses with every list column and text value fixed,
// which most other edits break; one fix run still applies them all. A document opening with an
// unrecognized block gets no MD027 group at all.
//
// MD035 groups come after MD027 and MD089, in a round with no other group but an MD047
// edit that analysis withheld, which waits for them in turn. Their checker
// compares whole parses and block envelopes, which a replacement of a different
// length moves for every other proof. A document opening with an unrecognized
// block gets no MD035 group at all.
//
// MD064 groups wait for a round in which no other rule proposes an edit, MD010,
// MD012, MD027, MD035 and MD089 included, and every other group waits while they remain. A TAB that MD010
// turns into spaces can lengthen a run, so MD064 sees the converted text.
// Their checker proves that Text values differ only in interior SPACE runs,
// which an edit from any other rule breaks, and deleting interior spaces moves
// text that the whole-parse and heading proofs of MD009 and the other rules compare exactly. Only an MD047 edit
// that analysis already offers shares their round; a withheld one waits for them.
// MD064 is withheld by the interior edit query in a document opening with an
// unrecognized block.
//
// MD089 groups go after MD027, in a round with no other group but an MD047 edit
// that analysis withheld, which waits for them in turn. Their checker compares
// Text values that other edits change, and the MD027, MD010 and heading proofs
// compare Text that an inserted space changes. MD064 waits for MD089 edits
// like any other. A document opening with an unrecognized block gets no MD089
// group at all.
//
// MD038 groups share the MD064 round: they wait for a round in which no other
// rule proposes an edit, and every other group waits while they remain. Their
// checker compares both parses with every Text and code span value exact, so an
// MD064 edit in the same round would fail it; MD064 goes first and MD038 waits
// for a round without MD064 edits. Only an MD047 edit that analysis already
// offers shares their round; a withheld one waits for them. MD038 is withheld
// by the interior edit query in a document opening with an unrecognized block.
//
// MD044 groups go ahead of MD064 and MD038 in that same isolated round, and both
// wait for them, since they count as edits. Their checker lets Text and code
// values differ only in letter case under full folding, which an edit from any
// other rule breaks. Only an MD047 edit that analysis already offers shares their round.
//
// MD049 groups come after MD044 in that same isolated round, ahead of MD064 and
// MD038, which wait for them. Their checker compares both parses with every
// Text value exact and lets only mismatched italics change their marker, which
// an edit from any other rule breaks. Only an MD047 edit that analysis already
// offers shares their round.
//
// MD050 groups go ahead of MD044, MD049, MD064 and MD038 in that same isolated
// round, and all four wait for them. Their checker lets Strong nodes differ only in
// their delimiter marker, which an edit from any other rule can break, and
// their eligibility reparses the whole document with only the markers changed.
// Only an MD047 edit that analysis already offers shares their round.
func Corrections(ctx context.Context, c fix.RoundContext) ([]fix.Group, error) {
	analysis, err := c.BoundedAnalysis(ctx)
	if err != nil {
		return nil, err
	}
	var terminal *result.Diagnostic
	var bold, names, italics, markers, spacing, quotes, pairs, blank, runs, indentation, capitalization, trailing, tabs, prose, padding, styles, lists, alignment, numbering, bullets, continuation, separators, gaps, paragraphs, rules []result.Diagnostic
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
		case "MD038":
			if d.FixAvailable {
				padding = append(padding, d)
			}
		case "MD064":
			if d.FixAvailable {
				prose = append(prose, d)
			}
		case "MD050":
			if d.FixAvailable {
				bold = append(bold, d)
			}
		case "MD044":
			if d.FixAvailable {
				names = append(names, d)
			}
		case "MD049":
			if d.FixAvailable {
				italics = append(italics, d)
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
		case "MD012":
			if !guarded {
				runs = append(runs, d)
			}
		case "MD009":
			if !guarded {
				trailing = append(trailing, d)
			}
		case "MD010":
			if !guarded {
				tabs = append(tabs, d)
			}
		case "MD085":
			if d.FixAvailable && !guarded {
				paragraphs = append(paragraphs, d)
			}
		case "MD032":
			if !guarded && d.FixAvailable {
				lists = append(lists, d)
			}
		case "MD007":
			if d.FixAvailable && !guarded {
				bullets = append(bullets, d)
			}
		case "MD027":
			if !guarded && d.FixAvailable {
				quotes = append(quotes, d)
			}
		case "MD089":
			if !guarded && d.FixAvailable {
				pairs = append(pairs, d)
			}
		case "MD035":
			if !guarded {
				rules = append(rules, d)
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
	groups, err := md004.Correction(ctx, c, markers)
	if err != nil {
		return nil, err
	}
	// Other list and whole-parse proofs compare what MD004 edits, so they wait.
	if len(groups) > 0 {
		styles, blank, indentation, capitalization, lists, alignment, numbering, bullets, continuation, separators = nil, nil, nil, nil, nil, nil, nil, nil, nil, nil
	}
	headings, err := md019.Correction(ctx, c, spacing)
	if err != nil {
		return nil, err
	}
	groups = append(groups, headings...)
	spaced := len(groups)
	insertions, err := md022.Correction(ctx, c, blank)
	if err != nil {
		return nil, err
	}
	groups = append(groups, insertions...)
	if len(insertions) == 0 {
		blanks, err := md032.Correction(ctx, c, lists)
		if err != nil {
			return nil, err
		}
		groups = append(groups, blanks...)
		if len(blanks) == 0 {
			indents, err := md023.Correction(ctx, c, indentation)
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
		markers, err := md030.Correction(ctx, c, separators)
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
	if len(groups) == 0 {
		dedented, err := md085.Correction(ctx, c, paragraphs)
		if err != nil {
			return nil, err
		}
		for _, g := range dedented {
			waiting = waiting || len(g.Edits) > 0
			blocking = blocking || len(g.Edits) > 0
			compared = compared || len(g.Edits) > 0
		}
		suffixes = append(suffixes, dedented...)
	}
	if !compared {
		aligned, err := md005.Correction(ctx, c, alignment)
		if err != nil {
			return nil, err
		}
		groups = append(groups, aligned...)
		waiting = waiting || len(aligned) > 0
		if len(aligned) == 0 {
			shifts, err := md007.Correction(ctx, c, bullets)
			if err != nil {
				return nil, err
			}
			groups = append(groups, shifts...)
			waiting = waiting || len(shifts) > 0
			blocking = blocking || len(shifts) > 0
			if len(shifts) == 0 {
				continued, err := md077.Correction(ctx, c, continuation)
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
	if !waiting {
		removals, err := md012.Correction(ctx, c, runs)
		if err != nil {
			return nil, err
		}
		groups = append(groups, removals...)
		for _, g := range removals {
			waiting = waiting || len(g.Edits) > 0
			blocking = blocking || len(g.Edits) > 0
		}
	}
	if !blocking {
		cased, err := headingcase.Groups(ctx, c, capitalization)
		if err != nil {
			return nil, err
		}
		groups = append(groups, cased...)
		waiting = waiting || len(cased) > 0
	}
	edited, tabbed := false, false
	for _, g := range groups {
		edited = edited || len(g.Edits) > 0
	}
	if !edited {
		replaced, err := md010.Correction(ctx, c, tabs)
		if err != nil {
			return nil, err
		}
		groups = append(groups, replaced...)
		for _, g := range replaced {
			tabbed = tabbed || len(g.Edits) > 0
		}
		waiting = waiting || tabbed
	}
	eligible := terminal != nil && !tabbed && (terminal.FixAvailable || touched)
	if !waiting && !eligible {
		style, err := md003.Correction(ctx, c, styles)
		if err != nil {
			return nil, err
		}
		waiting = len(style) > 0
		groups = append(groups, style...)
		if !waiting {
			spaced, err := md027.Correction(ctx, c, quotes)
			if err != nil {
				return nil, err
			}
			waiting = len(spaced) > 0
			groups = append(groups, spaced...)
		}
		if !waiting {
			inserted, err := md089.Correction(ctx, c, pairs)
			if err != nil {
				return nil, err
			}
			waiting = len(inserted) > 0
			groups = append(groups, inserted...)
		}
		if !waiting {
			breaks, err := md035.Correction(ctx, c, rules)
			if err != nil {
				return nil, err
			}
			waiting = len(breaks) > 0
			groups = append(groups, breaks...)
		}
	}
	editing := false
	for _, g := range groups {
		editing = editing || len(g.Edits) > 0
	}
	if !editing {
		rewritten, err := md050.Correction(ctx, c, bold)
		if err != nil {
			return nil, err
		}
		waiting = waiting || len(rewritten) > 0
		groups = append(groups, rewritten...)
		for _, g := range rewritten {
			editing = editing || len(g.Edits) > 0
		}
	}
	if !editing {
		named, err := md044.Correction(ctx, c, names)
		if err != nil {
			return nil, err
		}
		waiting = waiting || len(named) > 0
		groups = append(groups, named...)
		for _, g := range named {
			editing = editing || len(g.Edits) > 0
		}
	}
	if !editing {
		swapped, err := md049.Correction(ctx, c, italics)
		if err != nil {
			return nil, err
		}
		waiting = waiting || len(swapped) > 0
		groups = append(groups, swapped...)
		for _, g := range swapped {
			editing = editing || len(g.Edits) > 0
		}
	}
	if !editing {
		spaces, err := md064.Correction(ctx, c, prose)
		if err != nil {
			return nil, err
		}
		waiting = waiting || len(spaces) > 0
		groups = append(groups, spaces...)
		if len(spaces) == 0 {
			trimmed, err := md038.Correction(ctx, c, padding)
			if err != nil {
				return nil, err
			}
			waiting = waiting || len(trimmed) > 0
			groups = append(groups, trimmed...)
		}
	}
	if terminal != nil && !tabbed && (eligible || !waiting) {
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
		md019.Checker(),
		md021.Checker(),
		md023.Checker(),
		{Rule: "MD029", Check: orderedprefix.Check},
		md009.Checker(),
		md010.Checker(),
		md012.Checker(),
		md003.Checker(),
		md022.Checker(),
		{Rule: "MD063", Check: headingcase.Check},
		md032.Checker(),
		md005.Checker(),
		md007.Checker(),
		md077.Checker(),
		md085.Checker(),
		md030.Checker(),
		md038.Checker(),
		{Rule: "MD076", Check: listspacing.Check},
		md004.Checker(),
		md027.Checker(),
		md035.Checker(),
		md044.Checker(),
		md049.Checker(),
		md050.Checker(),
		md064.Checker(),
		md089.Checker(),
	}
}
