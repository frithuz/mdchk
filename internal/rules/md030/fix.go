package md030

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// correctableSeparators indexes this round's MD030 separators whose rewrite to
// spaces spaces is safe, by separator start. The separator bytes are kept so
// producers and checkers compare against the same expected text.
func correctableSeparators(ctx context.Context, c fix.RoundContext, facts []parser.ListSeparatorFact, spaces int) (map[int]parser.ListSeparatorFact, error) {
	if err := c.Reserve(ctx, int64(len(facts))+1, int64(len(facts))); err != nil {
		return nil, err
	}
	out := make(map[int]parser.ListSeparatorFact, len(facts))
	for _, f := range facts {
		if !listSeparatorShape(f, spaces) {
			continue
		}
		if err := c.Checkpoint(ctx, int64(f.Separator.End()-f.Separator.Start())+1); err != nil {
			return nil, err
		}
		text, err := c.Current().Text(f.Separator)
		if err != nil {
			return nil, err
		}
		if strings.Trim(text, " ") == "" {
			out[f.Separator.Start()] = f
		}
	}
	return out, nil
}

// Correction regenerates one group for each eligible active MD030
// finding from this round's parse, replacing the separator with spaces spaces.
func Correction(ctx context.Context, c fix.RoundContext, findings []result.Diagnostic) ([]fix.Group, error) {
	if len(findings) == 0 {
		return nil, nil
	}
	facts, spaces, err := c.ListSeparators(ctx)
	if err != nil {
		return nil, err
	}
	separators, err := correctableSeparators(ctx, c, facts, spaces)
	if err != nil {
		return nil, err
	}
	groups := make([]fix.Group, 0, len(findings))
	for _, d := range findings {
		if err := c.Reserve(ctx, int64(spaces)+128, 12); err != nil {
			return nil, err
		}
		f, ok := separators[d.ByteRange.Start]
		if !ok || f.Separator.End() != d.ByteRange.End {
			continue
		}
		expected, err := c.Current().Text(f.Separator)
		if err != nil {
			return nil, err
		}
		groups = append(groups, fix.Group{ID: fmt.Sprintf("MD030-separator-%d", f.Separator.Start()), Rule: "MD030", Provenance: "MD030 list marker separator", Anchor: f.Separator, Edits: []fix.Edit{{Span: f.Separator, Expected: []byte(expected), Replacement: bytes.Repeat([]byte(" "), spaces)}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return groups, c.Checkpoint(ctx, 0)
}

// listSeparatorRound holds the facts every MD030 group of a round shares. It is
// computed once, so proof cost does not grow with the group count.
type listSeparatorRound struct {
	separators map[int]parser.ListSeparatorFact
	spaces     int
	preserved  bool
}

// Checker returns the MD030 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD030", Check: checkListSeparator}
}

// checkListSeparator authorizes one group against the before parse, never against
// producer data. Whole-candidate proof runs once for the round. Rewriting spaces
// after a list marker cannot create a directive finding, so no ForbidNewFindings
// is registered.
func checkListSeparator(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("list-separators", func() (any, error) { return listSeparatorProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(listSeparatorRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD030" || len(g.Edits) != 1 {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	f, ok := round.separators[e.Span.Start()]
	if !ok || f.Separator != e.Span || g.Anchor != e.Span {
		return fix.Unsafe, nil
	}
	expected, err := c.Before.Current().Text(f.Separator)
	if err != nil {
		return fix.Unknown, err
	}
	if err := c.Before.Checkpoint(ctx, int64(len(expected)+len(e.Replacement))+1); err != nil {
		return fix.Unknown, err
	}
	if string(e.Expected) != expected || len(e.Replacement) != round.spaces || strings.Trim(string(e.Replacement), " ") != "" {
		return fix.Unsafe, nil
	}
	return fix.Preserved, nil
}

// listSeparatorProof compares the whole candidate with the before parse: only the
// round's edits changed bytes, the document means the same apart from content
// indentation, and every list item keeps its kind, marker, list, parent and order.
func listSeparatorProof(ctx context.Context, c fix.CheckContext) (listSeparatorRound, error) {
	failed := listSeparatorRound{}
	unchanged, err := c.OnlyEditsChanged(ctx)
	if err != nil || !unchanged {
		return failed, err
	}
	if same, err := c.MeaningPreservedAcrossIndentation(ctx); err != nil || !same {
		return failed, err
	}
	before, err := c.Before.ListItems(ctx)
	if err != nil {
		return failed, err
	}
	after, err := c.After.ListItems(ctx)
	if err != nil {
		return failed, err
	}
	if len(before) != len(after) {
		return failed, nil
	}
	for i, b := range before {
		a := after[i]
		if err := c.Before.Checkpoint(ctx, 8); err != nil {
			return failed, err
		}
		if b.Ordered != a.Ordered || b.Bullet != a.Bullet || b.Start != a.Start || b.Delimiter != a.Delimiter || b.MarkerWidth != a.MarkerWidth || b.ParentItem.Present() != a.ParentItem.Present() {
			return failed, nil
		}
		marker, ok, err := c.MapUnchanged(b.Marker)
		if err != nil || !ok || marker != a.Marker {
			return failed, err
		}
		for _, pair := range [][2]parser.Span{{b.Group, a.Group}, {b.Item, a.Item}, {b.ParentItem, a.ParentItem}} {
			if !pair[0].Present() {
				continue
			}
			mapped, ok, err := c.MapEndpoints(pair[0])
			if err != nil || !ok || mapped != pair[1] {
				return failed, err
			}
		}
	}
	facts, spaces, err := c.Before.ListSeparators(ctx)
	if err != nil {
		return failed, err
	}
	separators, err := correctableSeparators(ctx, c.Before, facts, spaces)
	if err != nil {
		return failed, err
	}
	return listSeparatorRound{separators, spaces, true}, c.Before.Checkpoint(ctx, 0)
}
