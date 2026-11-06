package rules

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// meaningNotProven is the deferral reason for suffixes known to carry meaning.
const meaningNotProven = "meaning-not-proven"

// expansionLimit is the deferral reason for a hard break that would grow past
// parser.MaxGrownHardBreakSpaces.
const expansionLimit = "expansion-limit"

// trailingWhitespaceGroups regenerates one atomic group per active MD009 finding
// from the round's own analysis and parse. Line numbers identify groups because no
// production edit moves an earlier line. Literal code gets no group; suffixes known
// to carry meaning or to grow the file too far are withheld through the planner.
func trailingWhitespaceGroups(ctx context.Context, c fix.RoundContext, diagnostics []result.Diagnostic) ([]fix.Group, error) {
	findings := len(diagnostics)
	if findings == 0 {
		return nil, nil
	}
	suffixes, spaces, err := c.TrailingWhitespace(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Reserve(ctx, int64(len(suffixes))*2+1, int64(len(suffixes))+int64(findings)*12); err != nil {
		return nil, err
	}
	byStart := make(map[int]parser.TrailingSuffix, len(suffixes))
	for _, s := range suffixes {
		byStart[s.Start()] = s
	}
	out := make([]fix.Group, 0, findings)
	for _, d := range diagnostics {
		s, ok := byStart[d.ByteRange.Start]
		if !ok || s.End() != d.ByteRange.End {
			return nil, fmt.Errorf("MD009 finding at byte %d has no suffix fact", d.ByteRange.Start)
		}
		if s.Literal {
			continue
		}
		group := fix.Group{ID: fmt.Sprintf("MD009-line-%d", d.Range.Start.Line), Rule: "MD009", Provenance: "MD009 physical line suffix", Anchor: s.Span, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}}
		if s.Withheld || s.Oversized {
			group.DeferReason = meaningNotProven
			if s.Oversized {
				group.DeferReason = expansionLimit
			}
			out = append(out, group)
			continue
		}
		expected, replacement, err := trailingEdit(ctx, c, s, spaces)
		if err != nil {
			return nil, err
		}
		group.Edits = []fix.Edit{{Span: s.Span, Expected: expected, Replacement: replacement}}
		out = append(out, group)
	}
	return out, c.Checkpoint(ctx, 0)
}

// trailingEdit derives one suffix's bytes. A real break normalizes to exactly the
// configured spaces and anything else is removed. The cost is charged first because
// a shrinking suffix sets the size only through hard_break_spaces.
func trailingEdit(ctx context.Context, c fix.RoundContext, s parser.TrailingSuffix, spaces int) ([]byte, []byte, error) {
	size := 0
	if s.HardBreak {
		size = spaces
	}
	if err := c.Reserve(ctx, int64(size), 0); err != nil {
		return nil, nil, err
	}
	if err := c.Reserve(ctx, int64(s.End()-s.Start())+1, 2); err != nil {
		return nil, nil, err
	}
	text, err := c.Current().Text(s.Span)
	if err != nil {
		return nil, nil, err
	}
	var replacement []byte
	if size > 0 {
		replacement = []byte(strings.Repeat(" ", size))
	}
	return []byte(text), replacement, nil
}

// trailingRound holds the facts every MD009 group of a round shares. It is computed
// once, so proof cost does not grow with the group count.
type trailingRound struct {
	suffixes  map[int]parser.TrailingSuffix
	edits     map[int]fix.Edit
	spaces    int
	preserved bool
}

// checkTrailingWhitespace authorizes one group against the before parse, trusting
// neither producer data nor fix flags. Whole-candidate proof runs once for the round.
// Shared protection, binding and directive checks remain mandatory in Validator, and
// changing line-end whitespace outside comments cannot create a directive finding, so
// no ForbidNewFindings is registered.
func checkTrailingWhitespace(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("trailing-whitespace", func() (any, error) { return trailingWhitespaceProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(trailingRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD009" || len(g.Edits) != 1 {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	s, ok := round.suffixes[e.Span.Start()]
	actual, applied := round.edits[e.Span.Start()]
	if !ok || !applied || s.Literal || s.Withheld || s.Oversized || s.Span != e.Span || g.Anchor != s.Span || actual.Span != s.Span {
		return fix.Unsafe, nil
	}
	expected, replacement, err := trailingEdit(ctx, c.Before, s, round.spaces)
	if err != nil {
		return fix.Unknown, err
	}
	if !bytes.Equal(e.Expected, expected) || !bytes.Equal(e.Replacement, replacement) || !bytes.Equal(actual.Replacement, replacement) {
		return fix.Unsafe, nil
	}
	return fix.Preserved, nil
}

// trailingWhitespaceProof recomputes the expected suffixes from the before parse,
// proves the combined candidate byte for byte and then compares both parses.
func trailingWhitespaceProof(ctx context.Context, c fix.CheckContext) (trailingRound, error) {
	failed := trailingRound{}
	suffixes, spaces, err := c.Before.TrailingWhitespace(ctx)
	if err != nil {
		return failed, err
	}
	_, edits, err := c.BoundedViews(ctx)
	if err != nil {
		return failed, err
	}
	if err := c.Before.Reserve(ctx, int64(len(suffixes)+len(edits))*2+1, int64(len(suffixes)+len(edits))+1); err != nil {
		return failed, err
	}
	round := trailingRound{suffixes: make(map[int]parser.TrailingSuffix, len(suffixes)), edits: make(map[int]fix.Edit, len(edits)), spaces: spaces}
	for _, s := range suffixes {
		round.suffixes[s.Start()] = s
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
