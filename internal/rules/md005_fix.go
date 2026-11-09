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

// itemIndentation is one list item's leading indentation: the bytes between the
// end of its container prefix and its marker. Spaces reports that they are only
// ASCII spaces, and single that no other byte of the item follows its first line
// except whitespace, so moving the marker moves nothing else.
type itemIndentation struct {
	span           parser.Span
	spaces, single bool
}

// listItemIndentation reads one item's first line and its remaining lines once.
// The caller's checkpoint is charged for every byte read.
func listItemIndentation(ctx context.Context, source parser.Source, item parser.ListItemFact, checkpoint func(context.Context, int64) error) (itemIndentation, error) {
	if err := checkpoint(ctx, int64(item.Item.End()-item.Prefix.Start())+1); err != nil {
		return itemIndentation{}, err
	}
	span, err := source.Span(item.Prefix.Start(), item.Marker.Start())
	if err != nil {
		return itemIndentation{}, err
	}
	whole, err := source.Span(item.Prefix.Start(), item.Item.End())
	if err != nil {
		return itemIndentation{}, err
	}
	text, err := source.Text(whole)
	if err != nil {
		return itemIndentation{}, err
	}
	indent := text[:span.End()-span.Start()]
	rest := text[item.Prefix.End()-item.Prefix.Start():]
	single := true
	if i := strings.IndexAny(rest, "\r\n"); i >= 0 {
		single = strings.Trim(rest[i+1:], " \t\r\n") == ""
	}
	return itemIndentation{span, strings.Trim(indent, " ") == "", single}, nil
}

// listAlignment is one correctable misaligned item and its replacement indentation.
type listAlignment struct {
	item        parser.ListItemFact
	edit        parser.Span
	replacement []byte
}

// listAlignments mirrors MD005's baselines: the first direct item of each group,
// whatever its directive state. It indexes every item that a spaces-only edit of
// its own indentation could align, by marker start.
func listAlignments(ctx context.Context, source parser.Source, items []parser.ListItemFact, checkpoint func(context.Context, int64) error) (map[int]listAlignment, error) {
	type baseline struct {
		indent int
		spaces bool
	}
	baselines := make(map[[2]int]baseline, len(items))
	out := map[int]listAlignment{}
	for _, item := range items {
		indent := item.ContentIndent - item.MarkerWidth - item.Padding
		key := [2]int{item.Group.Start(), item.Group.End()}
		base, exists := baselines[key]
		if !exists {
			lead, err := listItemIndentation(ctx, source, item, checkpoint)
			if err != nil {
				return nil, err
			}
			baselines[key] = baseline{indent, lead.spaces}
			continue
		}
		if indent == base.indent || !base.spaces {
			continue
		}
		lead, err := listItemIndentation(ctx, source, item, checkpoint)
		if err != nil {
			return nil, err
		}
		if !lead.spaces || !lead.single || lead.span.Start() == lead.span.End() {
			continue
		}
		if err := checkpoint(ctx, int64(base.indent)+1); err != nil {
			return nil, err
		}
		out[item.Marker.Start()] = listAlignment{item, lead.span, []byte(strings.Repeat(" ", base.indent))}
	}
	return out, nil
}

// listAlignmentGroups regenerates one group for each eligible active MD005
// finding from this round's own list facts.
func listAlignmentGroups(ctx context.Context, c fix.RoundContext, findings []result.Diagnostic) ([]fix.Group, error) {
	if len(findings) == 0 {
		return nil, nil
	}
	items, err := c.ListItems(ctx)
	if err != nil {
		return nil, err
	}
	alignments, err := listAlignments(ctx, c.Current(), items, c.Checkpoint)
	if err != nil {
		return nil, err
	}
	groups := make([]fix.Group, 0, len(findings))
	for _, d := range findings {
		if err := c.Reserve(ctx, 128, 12); err != nil {
			return nil, err
		}
		a, ok := alignments[d.ByteRange.Start]
		if !ok || a.item.Marker.End() != d.ByteRange.End {
			continue
		}
		expected, err := c.Current().Text(a.edit)
		if err != nil {
			return nil, err
		}
		groups = append(groups, fix.Group{ID: fmt.Sprintf("MD005-indent-%d", a.edit.Start()), Rule: "MD005", Provenance: "MD005 sibling list marker indentation", Anchor: a.item.Marker, Edits: []fix.Edit{{Span: a.edit, Expected: []byte(expected), Replacement: a.replacement}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return groups, c.Checkpoint(ctx, 0)
}

// listAlignmentRound holds the facts every MD005 group of a round shares.
type listAlignmentRound struct {
	alignments map[int]listAlignment
	aligned    map[int]bool
	preserved  bool
}

// checkListAlignment authorizes one group against the before parse, never against
// producer data. Inserting or deleting spaces before a list marker outside comments
// cannot create a directive finding, so no ForbidNewFindings is registered.
func checkListAlignment(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("list-alignment", func() (any, error) { return listAlignmentProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(listAlignmentRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD005" || len(g.Edits) != 1 {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	a, ok := round.alignments[g.Anchor.Start()]
	if !ok || a.item.Marker != g.Anchor || a.edit != e.Span || !round.aligned[g.Anchor.Start()] || !bytes.Equal(e.Replacement, a.replacement) {
		return fix.Unsafe, nil
	}
	expected, err := c.Before.Current().Text(a.edit)
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

// listAlignmentProof compares the whole candidate with the before parse: only the
// round's edits changed bytes, and every list item keeps its group, parent, item
// envelope, marker, prefix end and content. Aligned records, by original marker
// start, the items whose candidate indentation equals their group's first item.
func listAlignmentProof(ctx context.Context, c fix.CheckContext) (listAlignmentRound, error) {
	failed := listAlignmentRound{}
	unchanged, err := c.OnlyEditsChanged(ctx)
	if err != nil || !unchanged {
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
	if err := c.Before.Reserve(ctx, int64(len(before))*8+1, int64(len(before))*2+1); err != nil {
		return failed, err
	}
	same := func(b, a parser.Span) (bool, error) {
		if !b.Present() || !a.Present() {
			return b.Present() == a.Present(), nil
		}
		mapped, ok, err := c.MapEndpoints(b)
		return ok && mapped == a, err
	}
	round := listAlignmentRound{aligned: make(map[int]bool, len(before)), preserved: true}
	baselines := make(map[[2]int]int, len(after))
	for i, b := range before {
		a := after[i]
		if b.Ordered != a.Ordered || b.Bullet != a.Bullet || b.Start != a.Start || b.Delimiter != a.Delimiter || b.MarkerWidth != a.MarkerWidth || b.Padding != a.Padding {
			return failed, nil
		}
		marker, ok, err := c.MapUnchanged(b.Marker)
		if err != nil || !ok || marker != a.Marker {
			return failed, err
		}
		for _, pair := range [][2]parser.Span{{b.Group, a.Group}, {b.Item, a.Item}, {b.ParentItem, a.ParentItem}, {b.Prefix, a.Prefix}} {
			if ok, err := same(pair[0], pair[1]); err != nil || !ok {
				return failed, err
			}
		}
		indent := a.ContentIndent - a.MarkerWidth - a.Padding
		key := [2]int{a.Group.Start(), a.Group.End()}
		base, exists := baselines[key]
		if !exists {
			baselines[key], base = indent, indent
		}
		round.aligned[b.Marker.Start()] = indent == base
	}
	round.alignments, err = listAlignments(ctx, c.Before.Current(), before, c.Before.Checkpoint)
	if err != nil {
		return failed, err
	}
	return round, c.Before.Checkpoint(ctx, 0)
}
