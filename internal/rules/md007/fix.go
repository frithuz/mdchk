package md007

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// maxBulletGrowth is the widest shift to the right a correction may make. A wider
// one stays manual, so a large configured indent cannot multiply a file's size.
const maxBulletGrowth = 16

// bulletShift returns the starts of the lines that move with an item: its marker
// line and every later nonblank line up to the item's end, split at LF, CRLF or
// CR. It reports false when a shift of delta columns cannot be expressed by
// leading spaces alone: a moved line holds a tab (whose width depends on its
// column), carries a blockquote prefix, sits left of the item's content column
// (a lazy continuation), the marker would move left of column 0, or the shift
// grows past maxBulletGrowth.
func bulletShift(text string, item parser.ListItemFact, delta int) ([]int, bool) {
	marker, end := item.Marker.Start(), item.Item.End()
	if delta == 0 || delta > maxBulletGrowth || marker < 0 || marker >= end || end > len(text) {
		return nil, false
	}
	start := strings.LastIndexAny(text[:marker], "\r\n") + 1
	column := marker - start
	if strings.Trim(text[start:marker], " ") != "" || column+delta < 0 {
		return nil, false
	}
	content := column + item.MarkerWidth + item.Padding
	var starts []int
	for at := start; at < end; {
		lineEnd := len(text)
		if next := strings.IndexAny(text[at:], "\r\n"); next >= 0 {
			lineEnd = at + next
		}
		line := text[at:lineEnd]
		body := strings.TrimLeft(line, " ")
		switch {
		case strings.ContainsRune(line, '\t') || strings.HasPrefix(body, ">"):
			return nil, false
		case body == "":
		case at != start && len(line)-len(body) < content:
			return nil, false
		default:
			starts = append(starts, at)
		}
		at = lineEnd + 1
		if strings.HasPrefix(text[lineEnd:], "\r\n") {
			at++
		}
	}
	return starts, true
}

// bulletEdits removes or inserts the same number of spaces at each line start.
func bulletEdits(source parser.Source, starts []int, delta int) ([]fix.Edit, error) {
	spaces := []byte(strings.Repeat(" ", max(delta, -delta)))
	edits := make([]fix.Edit, 0, len(starts))
	for _, at := range starts {
		var e fix.Edit
		var err error
		if delta < 0 {
			e.Span, err = source.Span(at, at-delta)
			e.Expected = spaces
		} else {
			e.Span, err = source.Span(at, at)
			e.Replacement = spaces
		}
		if err != nil {
			return nil, err
		}
		edits = append(edits, e)
	}
	return edits, nil
}

// bulletParents maps each item to its parent's index, or -1 for a root. Items
// are in source preorder, so a parent always precedes its children.
func bulletParents(items []parser.ListItemFact) ([]int, bool) {
	type key struct{ start, end int }
	index := make(map[key]int, len(items))
	parents := make([]int, len(items))
	for i, item := range items {
		parents[i] = -1
		if item.ParentItem.Present() {
			p, ok := index[key{item.ParentItem.Start(), item.ParentItem.End()}]
			if !ok {
				return nil, false
			}
			parents[i] = p
		}
		index[key{item.Item.Start(), item.Item.End()}] = i
	}
	return parents, true
}

// bulletDelta is the column shift that gives item i the indentation MD007 expects.
func bulletDelta(items []parser.ListItemFact, parents []int, i, indent int, start bool) int {
	var parent parser.ListItemFact
	if parents[i] >= 0 {
		parent = items[parents[i]]
	}
	actual, expected := bulletOffsets(items[i], parent, indent, start)
	return expected - actual
}

// sourceText reads a whole source after charging its length.
func sourceText(ctx context.Context, c fix.RoundContext) (string, error) {
	source := c.Current()
	if err := c.Checkpoint(ctx, int64(source.Len())+1); err != nil {
		return "", err
	}
	all, err := source.Span(0, source.Len())
	if err != nil {
		return "", err
	}
	return source.Text(all)
}

// Correction regenerates one atomic group per eligible MD007 finding
// from the round's own parse. Only the outermost of nested findings is proposed in
// a round, so groups never overlap; the next round regenerates the rest.
func Correction(ctx context.Context, c fix.RoundContext, diagnostics []result.Diagnostic) ([]fix.Group, error) {
	if len(diagnostics) == 0 {
		return nil, nil
	}
	items, indent, start, err := c.BulletIndentation(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Reserve(ctx, int64(len(items))*2+1, int64(len(items)+len(diagnostics))*4); err != nil {
		return nil, err
	}
	parents, ok := bulletParents(items)
	if !ok {
		return nil, parser.ErrModel
	}
	text, err := sourceText(ctx, c)
	if err != nil {
		return nil, err
	}
	byMarker := make(map[int]int, len(items))
	for i, item := range items {
		byMarker[item.Marker.Start()] = i
	}
	covered := -1
	var out []fix.Group
	for _, d := range diagnostics {
		i, ok := byMarker[d.ByteRange.Start]
		if !ok || items[i].Ordered || items[i].Marker.End() != d.ByteRange.End {
			return nil, fmt.Errorf("MD007 finding at byte %d has no list item fact", d.ByteRange.Start)
		}
		item := items[i]
		if err := c.Checkpoint(ctx, int64(item.Item.End()-item.Item.Start())+1); err != nil {
			return nil, err
		}
		delta := bulletDelta(items, parents, i, indent, start)
		starts, ok := bulletShift(text, item, delta)
		if !ok || len(starts) == 0 || starts[0] < covered {
			continue
		}
		edits, err := bulletEdits(c.Current(), starts, delta)
		if err != nil {
			return nil, err
		}
		covered = item.Item.End()
		out = append(out, fix.Group{ID: fmt.Sprintf("MD007-item-%d", item.Marker.Start()), Rule: "MD007", Provenance: "MD007 bullet item indentation", Anchor: item.Marker, Edits: edits, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return out, c.Checkpoint(ctx, 0)
}

// bulletRound holds the facts every MD007 group of a round shares. It is computed
// once, so proof cost does not grow with the group count.
type bulletRound struct {
	before, after         []parser.ListItemFact
	parents, afterParents []int
	indent, afterIndent   int
	start, afterStart     bool
	text                  string
	preserved             bool
}

// Checker returns the MD007 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD007", Check: checkBulletIndentation}
}

// checkBulletIndentation authorizes one group against the before parse, trusting
// neither producer data nor fix flags. It recomputes the item's shift and its exact
// line-start edits, and requires the corrected item to match its expected offset
// in the candidate. Whole-candidate proof runs once for the round.
func checkBulletIndentation(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("bullet-indentation", func() (any, error) { return bulletIndentationProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(bulletRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD007" || len(g.Edits) == 0 {
		return fix.Unsafe, nil
	}
	if err := c.Before.Reserve(ctx, int64(len(round.before)+len(g.Edits))+1, int64(len(g.Edits))+1); err != nil {
		return fix.Unknown, err
	}
	i := -1
	for j, item := range round.before {
		if item.Marker == g.Anchor && !item.Ordered {
			i = j
		}
	}
	if i < 0 {
		return fix.Unsafe, nil
	}
	item := round.before[i]
	delta := bulletDelta(round.before, round.parents, i, round.indent, round.start)
	starts, ok := bulletShift(round.text, item, delta)
	if !ok || len(starts) != len(g.Edits) {
		return fix.Unsafe, nil
	}
	edits, err := bulletEdits(c.Before.Current(), starts, delta)
	if err != nil {
		return fix.Unknown, err
	}
	for j, e := range edits {
		if g.Edits[j].Span != e.Span || !bytes.Equal(g.Edits[j].Expected, e.Expected) || !bytes.Equal(g.Edits[j].Replacement, e.Replacement) {
			return fix.Unsafe, nil
		}
	}
	if bulletDelta(round.after, round.afterParents, i, round.afterIndent, round.afterStart) != 0 {
		return fix.Unsafe, nil
	}
	return fix.Preserved, nil
}

// bulletIndentationProof proves that only the round's edits changed bytes, that
// both parses mean the same apart from list content columns, and that every list
// item keeps its order, parent, list membership, kind and marker.
func bulletIndentationProof(ctx context.Context, c fix.CheckContext) (bulletRound, error) {
	failed := bulletRound{}
	if unchanged, err := c.OnlyEditsChanged(ctx); err != nil || !unchanged {
		return failed, err
	}
	if same, err := c.MeaningPreservedAcrossIndentation(ctx); err != nil || !same {
		return failed, err
	}
	var round bulletRound
	var err error
	if round.before, round.indent, round.start, err = c.Before.BulletIndentation(ctx); err != nil {
		return failed, err
	}
	if round.after, round.afterIndent, round.afterStart, err = c.After.BulletIndentation(ctx); err != nil {
		return failed, err
	}
	if len(round.before) != len(round.after) {
		return failed, nil
	}
	if err := c.Before.Reserve(ctx, int64(len(round.before))*4+1, int64(len(round.before))*4+1); err != nil {
		return failed, err
	}
	var ok bool
	if round.parents, ok = bulletParents(round.before); !ok {
		return failed, nil
	}
	if round.afterParents, ok = bulletParents(round.after); !ok {
		return failed, nil
	}
	beforeGroups, afterGroups := bulletGroups(round.before), bulletGroups(round.after)
	for i, b := range round.before {
		a := round.after[i]
		if round.parents[i] != round.afterParents[i] || beforeGroups[i] != afterGroups[i] || b.Ordered != a.Ordered || b.Bullet != a.Bullet || b.Start != a.Start || b.Delimiter != a.Delimiter || b.MarkerWidth != a.MarkerWidth || b.Padding != a.Padding {
			return failed, nil
		}
	}
	if round.text, err = sourceText(ctx, c.Before); err != nil {
		return failed, err
	}
	round.preserved = true
	return round, c.Before.Checkpoint(ctx, 0)
}

// bulletGroups numbers each item's list in order of first appearance, so two
// parses agree exactly when no list merged with or split from another.
func bulletGroups(items []parser.ListItemFact) []int {
	type key struct{ start, end int }
	seen := make(map[key]int, len(items))
	out := make([]int, len(items))
	for i, item := range items {
		k := key{item.Group.Start(), item.Group.End()}
		n, ok := seen[k]
		if !ok {
			n = len(seen)
			seen[k] = n
		}
		out[i] = n
	}
	return out
}
