// Package orderedprefix produces and checks the MD029 correction of ordered list
// item numbers.
package orderedprefix

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// maxValue is the largest ordinal CommonMark admits: nine decimal digits.
const maxValue = 999999999

// Propose returns the edit renumbering one ordered item to want, or false when
// the correction is deferred. Only the marker digits change, written in plain
// decimal. A wider marker takes its growth from all-space padding, which must
// keep one space. A narrower marker of an item with more lines gives its width
// back to the padding, so the content column of later lines stays put; a
// single-line item keeps its padding. Tab padding and empty content are
// deferred whenever the width changes.
func Propose(source parser.Source, item parser.ListItemFact, want int) (fix.Edit, bool, error) {
	if !item.Ordered || !item.Delimiter.Present || want < 0 || want > maxValue {
		return fix.Edit{}, false, nil
	}
	marker, err := source.Text(item.Marker)
	if err != nil {
		return fix.Edit{}, false, err
	}
	if len(marker) < 2 || marker[len(marker)-1] != item.Delimiter.Value {
		return fix.Edit{}, false, nil
	}
	digits, value := marker[:len(marker)-1], strconv.Itoa(want)
	if digits == value {
		return fix.Edit{}, false, nil
	}
	delta := len(value) - len(digits)
	body, err := source.Span(item.Marker.End(), item.Item.End())
	if err != nil {
		return fix.Edit{}, false, err
	}
	rest, err := source.Text(body)
	if err != nil {
		return fix.Edit{}, false, err
	}
	run := len(rest) - len(strings.TrimLeft(rest, " \t"))
	padding := rest[:run]
	line := strings.IndexAny(rest, "\r\n")
	if line < 0 {
		line = len(rest)
	}
	multiline := strings.TrimLeft(rest[line:], "\r\n") != ""
	end := item.Marker.End() - 1
	replacement := value
	if delta != 0 {
		if run == line || strings.Contains(padding, "\t") || run != item.Padding {
			return fix.Edit{}, false, nil
		}
		if delta > 0 || multiline {
			if run-delta < 1 {
				return fix.Edit{}, false, nil
			}
			end = item.Marker.End() + run
			replacement = value + string(item.Delimiter.Value) + strings.Repeat(" ", run-delta)
		}
	}
	span, err := source.Span(item.Marker.Start(), end)
	if err != nil {
		return fix.Edit{}, false, err
	}
	expected, err := source.Text(span)
	if err != nil {
		return fix.Edit{}, false, err
	}
	return fix.Edit{Span: span, Expected: []byte(expected), Replacement: []byte(replacement)}, true, nil
}

// expectation is one ordered item whose number differs from the value the
// configured style expects of it.
type expectation struct {
	item parser.ListItemFact
	want int
}

// expectations mirrors MD029 detection over one parse: each group's first number
// starts the ordered sequence, and style one expects 1 everywhere. It indexes the
// mismatched items by marker start.
func expectations(ctx context.Context, items []parser.ListItemFact, one bool, checkpoint func(context.Context, int64) error) (map[int]expectation, error) {
	type sequence struct{ first, count int }
	groups := map[[2]int]sequence{}
	out := map[int]expectation{}
	for _, item := range items {
		if err := checkpoint(ctx, 1); err != nil {
			return nil, err
		}
		if !item.Ordered || !item.Start.Present {
			continue
		}
		key := [2]int{item.Group.Start(), item.Group.End()}
		state, exists := groups[key]
		if !exists {
			state.first = item.Start.Value
		}
		want := state.first + state.count
		if one {
			want = 1
		}
		state.count++
		groups[key] = state
		if item.Start.Value != want {
			out[item.Marker.Start()] = expectation{item, want}
		}
	}
	return out, nil
}

// Groups regenerates one group for each eligible active MD029 finding from this
// round's list facts. The expected value is this round's, never a saved one.
func Groups(ctx context.Context, c fix.RoundContext, findings []result.Diagnostic) ([]fix.Group, error) {
	if len(findings) == 0 {
		return nil, nil
	}
	one, err := c.OrderedListStyle()
	if err != nil {
		return nil, err
	}
	items, err := c.ListItems(ctx)
	if err != nil {
		return nil, err
	}
	wanted, err := expectations(ctx, items, one, c.Checkpoint)
	if err != nil {
		return nil, err
	}
	groups := make([]fix.Group, 0, len(findings))
	for _, d := range findings {
		if err := c.Reserve(ctx, 128, 12); err != nil {
			return nil, err
		}
		e, ok := wanted[d.ByteRange.Start]
		if !ok || e.item.Marker.End() != d.ByteRange.End {
			continue
		}
		edit, ok, err := Propose(c.Current(), e.item, e.want)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		groups = append(groups, fix.Group{ID: fmt.Sprintf("MD029-prefix-%d", e.item.Marker.Start()), Rule: "MD029", Provenance: "MD029 ordered list item number", Anchor: e.item.Marker, Edits: []fix.Edit{edit}, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return groups, nil
}

// prefixRound holds the facts every MD029 group of a round shares. It is
// computed once, so proof cost does not grow with the group count.
type prefixRound struct {
	wanted    map[int]expectation
	preserved bool
}

// Check authorizes one group against the before parse, never against producer
// data: its single edit must be exactly the edit Propose derives from the before
// facts. Whole-candidate proof runs once for the round.
func Check(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("ordered-prefix", func() (any, error) { return prefixProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(prefixRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD029" || len(g.Edits) != 1 {
		return fix.Unsafe, nil
	}
	e, ok := round.wanted[g.Edits[0].Span.Start()]
	if !ok || g.Anchor != e.item.Marker {
		return fix.Unsafe, nil
	}
	if ok, err := sameEdit(ctx, c, e, g.Edits[0]); err != nil || !ok {
		return fix.Unsafe, err
	}
	return fix.Preserved, nil
}

func sameEdit(ctx context.Context, c fix.CheckContext, e expectation, edit fix.Edit) (bool, error) {
	want, ok, err := Propose(c.Before.Current(), e.item, e.want)
	if err != nil || !ok {
		return false, err
	}
	if err := c.Before.Checkpoint(ctx, int64(len(want.Expected)+len(want.Replacement))+1); err != nil {
		return false, err
	}
	return want.Span == edit.Span && string(want.Expected) == string(edit.Expected) && string(want.Replacement) == string(edit.Replacement), nil
}

// prefixProof compares the whole candidate with the before parse: only the
// round's edits changed bytes, every MD029 edit is the expected renumbering, and
// the document keeps its block tree, list groups, items, nesting, containers,
// delimiters and paragraph membership. Only renumbered items change their number.
func prefixProof(ctx context.Context, c fix.CheckContext) (prefixRound, error) {
	failed := prefixRound{}
	unchanged, err := c.OnlyEditsChanged(ctx)
	if err != nil || !unchanged {
		return failed, err
	}
	one, err := c.Before.OrderedListStyle()
	if err != nil {
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
	wanted, err := expectations(ctx, before, one, c.Before.Checkpoint)
	if err != nil {
		return failed, err
	}
	renumbered := map[int]int{}
	for _, shared := range c.Edits() {
		if err := c.Before.Checkpoint(ctx, int64(len(shared.Groups))+1); err != nil {
			return failed, err
		}
		owned := false
		for _, id := range shared.Groups {
			owned = owned || strings.HasPrefix(id, "MD029-")
		}
		if !owned {
			continue
		}
		e, ok := wanted[shared.Edit.Span.Start()]
		if !ok {
			return failed, nil
		}
		if ok, err := sameEdit(ctx, c, e, shared.Edit); err != nil || !ok {
			return failed, err
		}
		renumbered[e.item.Marker.Start()] = e.want
	}
	for i, b := range before {
		if err := c.Before.Checkpoint(ctx, 1); err != nil {
			return failed, err
		}
		a := after[i]
		start := b.Start
		if want, ok := renumbered[b.Marker.Start()]; ok {
			start.Value = want
		}
		if a.Ordered != b.Ordered || a.Bullet != b.Bullet || a.Delimiter != b.Delimiter || a.Start != start || a.ContentIndent-a.MarkerWidth-a.Padding != b.ContentIndent-b.MarkerWidth-b.Padding {
			return failed, nil
		}
		if _, ok := renumbered[b.Marker.Start()]; !ok && (a.MarkerWidth != b.MarkerWidth || a.Padding != b.Padding) {
			return failed, nil
		}
		for _, pair := range [...][2]parser.Span{{b.Group, a.Group}, {b.Item, a.Item}, {b.ParentItem, a.ParentItem}} {
			if !pair[0].Present() {
				if pair[1].Present() {
					return failed, nil
				}
				continue
			}
			if same, err := sameMapped(c, pair[0], pair[1]); err != nil || !same {
				return failed, err
			}
		}
	}
	if same, err := sameTree(ctx, c); err != nil || !same {
		return failed, err
	}
	return prefixRound{wanted, true}, nil
}

// sameMapped reports whether after is before mapped around the round's edits. An
// envelope ending where another rule inserts a line ending may grow over it, as a
// block owning its line ending does, or stay before it, as inline text does.
func sameMapped(c fix.CheckContext, before, after parser.Span) (bool, error) {
	grown, ok, err := c.MapEndpoints(before)
	if err != nil || !ok {
		return false, err
	}
	if grown == after {
		return true, nil
	}
	end, err := c.Before.Current().Span(before.End(), before.End())
	if err != nil {
		return false, err
	}
	point, ok, err := c.MapFact(end)
	if err != nil || !ok {
		return false, err
	}
	return after.Start() == grown.Start() && after.End() == point.End(), nil
}

// sameTree walks both parses in preorder: every node keeps its kind and its
// mapped envelope, which covers containers, list boundaries, paragraphs and the
// lazy lines they own.
func sameTree(ctx context.Context, c fix.CheckContext) (bool, error) {
	before, after := c.Before.Parsed(), c.After.Parsed()
	if before.Status != parser.Complete || after.Status != parser.Complete || before.Document == nil || after.Document == nil || len(before.Uncertainty) != 0 || len(after.Uncertainty) != 0 {
		return false, nil
	}
	var nodes []*parser.Node
	var failure error
	parser.Walk(after.Document.Root(), func(n *parser.Node) bool {
		if failure = c.Before.Reserve(ctx, 1, 1); failure != nil {
			return false
		}
		nodes = append(nodes, n)
		return true
	})
	if failure != nil {
		return false, failure
	}
	i, same := 0, true
	parser.Walk(before.Document.Root(), func(n *parser.Node) bool {
		if failure = c.Before.Checkpoint(ctx, 1); failure != nil {
			return false
		}
		if i == len(nodes) || nodes[i].Kind() != n.Kind() {
			same = false
			return false
		}
		if ok, err := sameMapped(c, n.Span(), nodes[i].Span()); err != nil || !ok {
			failure, same = err, false
			return false
		}
		i++
		return true
	})
	if failure != nil {
		return false, failure
	}
	return same && i == len(nodes), nil
}
