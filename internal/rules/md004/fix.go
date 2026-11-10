package md004

import (
	"bytes"
	"context"
	"fmt"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// bulletBaseline is the marker every bullet must use: the configured one, or for
// consistent the first actual bullet in the document, as analysis selects it.
func bulletBaseline(items []parser.ListItemFact, configured byte) byte {
	if configured != 0 {
		return configured
	}
	for _, item := range items {
		if !item.Ordered {
			return item.Bullet.Value
		}
	}
	return 0
}

// safeBulletLists returns, by list start, the unordered lists whose bullet can
// become expected without changing block structure. A list beside another
// unordered list would merge with it, and an item whose first line would read as
// a thematic break stops being an item. The combined-candidate checker remains
// authoritative for every other case.
func safeBulletLists(ctx context.Context, source parser.Source, items []parser.ListItemFact, lists []parser.BulletListFact, expected byte, checkpoint func(context.Context, int64) error) (map[int]bool, error) {
	safe := make(map[int]bool, len(lists))
	for _, l := range lists {
		if err := checkpoint(ctx, 1); err != nil {
			return nil, err
		}
		if !l.Adjacent {
			safe[l.Span.Start()] = true
		}
	}
	for _, item := range items {
		if item.Ordered || item.Bullet.Value == expected || !safe[item.Group.Start()] {
			continue
		}
		thematic, err := becomesThematicBreak(ctx, source, item, expected, checkpoint)
		if err != nil {
			return nil, err
		}
		if thematic {
			safe[item.Group.Start()] = false
		}
	}
	return safe, nil
}

// becomesThematicBreak reports whether the item's first line, from its marker on,
// would consist only of three or more expected bullets and spaces or tabs. The
// scan stops at the first byte that rules this out.
func becomesThematicBreak(ctx context.Context, source parser.Source, item parser.ListItemFact, expected byte, checkpoint func(context.Context, int64) error) (bool, error) {
	span, err := source.Span(item.Marker.Start(), item.Item.End())
	if err != nil {
		return false, err
	}
	text, err := source.Text(span)
	if err != nil {
		return false, err
	}
	count := 1
	for i := 1; i < len(text); i++ {
		if i&255 == 0 {
			if err := checkpoint(ctx, 256); err != nil {
				return false, err
			}
		}
		switch text[i] {
		case '\n', '\r':
			return count >= 3, nil
		case ' ', '\t':
		case expected:
			count++
		default:
			return false, nil
		}
	}
	return count >= 3, checkpoint(ctx, int64(len(text)&255))
}

// Correction regenerates one group per eligible MD004 finding from the
// round's own parse. Analysis grants eligibility to a whole list or none of it, so
// a list never splits.
func Correction(ctx context.Context, c fix.RoundContext, diagnostics []result.Diagnostic) ([]fix.Group, error) {
	if len(diagnostics) == 0 {
		return nil, nil
	}
	items, _, configured, err := c.BulletMarkers(ctx)
	if err != nil {
		return nil, err
	}
	expected := bulletBaseline(items, configured)
	if err := c.Reserve(ctx, int64(len(items)+len(diagnostics))+1, int64(len(items)+len(diagnostics)*12)); err != nil {
		return nil, err
	}
	byMarker := make(map[int]parser.ListItemFact, len(items))
	for _, item := range items {
		byMarker[item.Marker.Start()] = item
	}
	out := make([]fix.Group, 0, len(diagnostics))
	for _, d := range diagnostics {
		item, ok := byMarker[d.ByteRange.Start]
		if !ok || item.Ordered || item.Marker.End() != d.ByteRange.End || item.Bullet.Value == expected {
			return nil, fmt.Errorf("MD004 finding at byte %d has no bullet fact", d.ByteRange.Start)
		}
		out = append(out, fix.Group{ID: fmt.Sprintf("MD004-marker-%d", item.Marker.Start()), Rule: "MD004", Provenance: "MD004 bullet marker", Anchor: item.Marker, Edits: []fix.Edit{{Span: item.Marker, Expected: []byte{item.Bullet.Value}, Replacement: []byte{expected}}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return out, c.Checkpoint(ctx, 0)
}

// markerRound holds the facts every MD004 group of a round shares. It is computed
// once, so proof cost does not grow with the group count.
type markerRound struct {
	before, after map[int]parser.ListItemFact
	expected      byte
	preserved     bool
}

// Checker returns the MD004 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD004", Check: checkBulletMarker}
}

// checkBulletMarker authorizes one group against the before parse, trusting
// neither producer data nor fix flags. Shared protection, binding and directive
// checks remain mandatory in Validator, and a bullet byte outside comments cannot
// create a directive finding, so no ForbidNewFindings is registered.
func checkBulletMarker(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("bullet-markers", func() (any, error) { return bulletMarkerProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(markerRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD004" || len(g.Edits) != 1 {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	before, known := round.before[e.Span.Start()]
	if !known || before.Ordered || before.Marker != e.Span || g.Anchor != e.Span || before.Bullet.Value == round.expected {
		return fix.Unsafe, nil
	}
	mapped, ok, err := c.MapEndpoints(e.Span)
	if err != nil {
		return fix.Unknown, err
	}
	after, present := round.after[mapped.Start()]
	if !ok || !present || after.Ordered || after.Bullet.Value != round.expected || mapped != after.Marker {
		return fix.Unsafe, nil
	}
	if !bytes.Equal(e.Expected, []byte{before.Bullet.Value}) || !bytes.Equal(e.Replacement, []byte{round.expected}) {
		return fix.Unsafe, nil
	}
	return fix.Preserved, nil
}

// bulletMarkerProof recomputes the expected marker from the before parse, proves
// that only the round's edits changed bytes and compares the block structure of
// both parses: list count, item ownership and order, nesting, tight state,
// containers and block kinds.
func bulletMarkerProof(ctx context.Context, c fix.CheckContext) (markerRound, error) {
	failed := markerRound{}
	before, _, configured, err := c.Before.BulletMarkers(ctx)
	if err != nil {
		return failed, err
	}
	after, _, _, err := c.After.BulletMarkers(ctx)
	if err != nil {
		return failed, err
	}
	if len(before) != len(after) {
		return failed, nil
	}
	if err := c.Before.Reserve(ctx, int64(len(before)+len(after))*2+1, int64(len(before)+len(after))+1); err != nil {
		return failed, err
	}
	round := markerRound{before: make(map[int]parser.ListItemFact, len(before)), after: make(map[int]parser.ListItemFact, len(after)), expected: bulletBaseline(before, configured)}
	for _, item := range before {
		round.before[item.Marker.Start()] = item
	}
	for _, item := range after {
		round.after[item.Marker.Start()] = item
	}
	if unchanged, err := c.OnlyEditsChanged(ctx); err != nil || !unchanged {
		return failed, err
	}
	if same, err := c.SameStructureAcrossBullets(ctx); err != nil || !same {
		return failed, err
	}
	round.preserved = true
	return round, c.Before.Checkpoint(ctx, 0)
}
