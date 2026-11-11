package md010

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// expansionLimit is the deferral reason for a TAB whose replacement would grow
// past parser.MaxTabSpaces.
const expansionLimit = "expansion-limit"

// Correction regenerates one atomic group per active MD010 finding in inline
// paragraph, heading or table-cell text from the round's own analysis and parse.
// Byte offsets identify groups. A TAB in any other context gets no group; one
// whose replacement would grow the file too far is withheld through the planner.
func Correction(ctx context.Context, c fix.RoundContext, diagnostics []result.Diagnostic) ([]fix.Group, error) {
	findings := len(diagnostics)
	if findings == 0 {
		return nil, nil
	}
	tabs, spaces, err := c.TabCharacters(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Reserve(ctx, int64(len(tabs))*2+1, int64(len(tabs))+int64(findings)*12); err != nil {
		return nil, err
	}
	byStart := make(map[int]engine.TabCharacterItem, len(tabs))
	for _, t := range tabs {
		byStart[t.Start()] = t
	}
	out := make([]fix.Group, 0, findings)
	for _, d := range diagnostics {
		t, ok := byStart[d.ByteRange.Start]
		if !ok || t.End() != d.ByteRange.End {
			return nil, fmt.Errorf("MD010 finding at byte %d has no TAB fact", d.ByteRange.Start)
		}
		if !t.Text {
			continue
		}
		group := fix.Group{ID: fmt.Sprintf("MD010-byte-%d", t.Start()), Rule: "MD010", Provenance: "MD010 inline text TAB", Anchor: t.Span, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}}
		if t.Oversized {
			group.DeferReason = expansionLimit
			out = append(out, group)
			continue
		}
		expected, replacement, err := tabEdit(ctx, c, t, spaces)
		if err != nil {
			return nil, err
		}
		group.Edits = []fix.Edit{{Span: t.Span, Expected: expected, Replacement: replacement}}
		out = append(out, group)
	}
	return out, c.Checkpoint(ctx, 0)
}

// tabEdit derives one TAB's replacement. The cost is charged before the spaces
// are built, and an oversized TAB never reaches here.
func tabEdit(ctx context.Context, c fix.RoundContext, t engine.TabCharacterItem, spaces int) ([]byte, []byte, error) {
	if t.Oversized {
		return nil, nil, fmt.Errorf("MD010 TAB at byte %d exceeds the expansion limit", t.Start())
	}
	if err := c.Reserve(ctx, int64(spaces)+2, 2); err != nil {
		return nil, nil, err
	}
	text, err := c.Current().Text(t.Span)
	if err != nil {
		return nil, nil, err
	}
	return []byte(text), []byte(strings.Repeat(" ", spaces)), nil
}

// tabRound holds the facts every MD010 group of a round shares. It is computed
// once, so proof cost does not grow with the group count.
type tabRound struct {
	tabs      map[int]engine.TabCharacterItem
	edits     map[int]fix.Edit
	spaces    int
	preserved bool
}

// Checker returns the MD010 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD010", Check: checkTabCharacters}
}

// checkTabCharacters authorizes one group against the before parse, trusting
// neither producer data nor fix flags. Whole-candidate proof runs once for the
// round. Shared protection, binding and directive checks remain mandatory in
// Validator, and replacing a TAB in inline text outside comments cannot create a
// directive finding, so no ForbidNewFindings is registered.
func checkTabCharacters(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("tab-characters", func() (any, error) { return tabCharactersProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(tabRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD010" || len(g.Edits) != 1 {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	t, ok := round.tabs[e.Span.Start()]
	actual, applied := round.edits[e.Span.Start()]
	if !ok || !applied || !t.Text || t.Oversized || t.Span != e.Span || g.Anchor != t.Span || actual.Span != t.Span {
		return fix.Unsafe, nil
	}
	expected, replacement, err := tabEdit(ctx, c.Before, t, round.spaces)
	if err != nil {
		return fix.Unknown, err
	}
	if !bytes.Equal(e.Expected, expected) || !bytes.Equal(e.Replacement, replacement) || !bytes.Equal(actual.Replacement, replacement) {
		return fix.Unsafe, nil
	}
	return fix.Preserved, nil
}

// tabCharactersProof recomputes the expected TABs from the before parse, proves
// the combined candidate byte for byte and then compares both parses, reading
// each TAB in Text as spaces_per_tab spaces.
func tabCharactersProof(ctx context.Context, c fix.CheckContext) (tabRound, error) {
	failed := tabRound{}
	tabs, spaces, err := c.Before.TabCharacters(ctx)
	if err != nil {
		return failed, err
	}
	_, edits, err := c.BoundedViews(ctx)
	if err != nil {
		return failed, err
	}
	if err := c.Before.Reserve(ctx, int64(len(tabs)+len(edits))*2+1, int64(len(tabs)+len(edits))+1); err != nil {
		return failed, err
	}
	round := tabRound{tabs: make(map[int]engine.TabCharacterItem, len(tabs)), edits: make(map[int]fix.Edit, len(edits)), spaces: spaces}
	for _, t := range tabs {
		round.tabs[t.Start()] = t
	}
	for _, e := range edits {
		round.edits[e.Edit.Span.Start()] = e.Edit
	}
	if spaces > parser.MaxTabSpaces {
		return failed, nil
	}
	if unchanged, err := c.OnlyEditsChanged(ctx); err != nil || !unchanged {
		return failed, err
	}
	if same, err := c.MeaningPreservedAcrossTabs(ctx, spaces); err != nil || !same {
		return failed, err
	}
	round.preserved = true
	return round, c.Before.Checkpoint(ctx, 0)
}
