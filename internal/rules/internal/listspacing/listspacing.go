// Package listspacing produces and checks the MD076 corrections of blank lines
// between sibling list items.
package listspacing

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// Separator is the exact correction of one MD076 finding. Edit inserts the
// missing blank lines at the start of the next sibling's marker line, or deletes
// whole surplus lines after the first Expected ones. Scope is the nonempty span
// that eligibility and protection are judged on, from the end of the previous
// item's last line through the edit. Marker is the next sibling's marker offset,
// which identifies the list allowed to change tightness.
type Separator struct {
	Edit   fix.Edit
	Scope  parser.Span
	Marker int
}

// Unclosed returns the envelopes of the fenced code blocks without a closing
// fence, in source order. A blank line inserted after one would become payload.
func Unclosed(ctx context.Context, doc *parser.Document, checkpoint func(context.Context, int64) error) ([]parser.Span, error) {
	var out []parser.Span
	var err error
	if doc == nil {
		return nil, nil
	}
	parser.Walk(doc.Root(), func(n *parser.Node) bool {
		if err = checkpoint(ctx, 1); err != nil {
			return false
		}
		if n.Kind() != parser.CodeBlock {
			return true
		}
		if code := n.Properties().Code; code == nil || code.Style != "fenced" {
			return true
		}
		for _, m := range n.Markers() {
			if m.Role == "closing" {
				return true
			}
		}
		out = append(out, n.Span())
		return true
	})
	return out, err
}

// Correct derives the separator edit from the source alone. It reports false
// when the gap's ancestor prefixes cannot be reproduced byte for byte: every
// gap line must be the marker line's prefix without trailing spaces, the
// previous line must start with that prefix exactly, and no prefix holds a tab.
// It also reports false when the previous line ends inside an unclosed fence.
func Correct(ctx context.Context, source parser.Source, lines parser.LineIndex, unclosed []parser.Span, fact parser.ListItemSpacingFact, checkpoint func(context.Context, int64) error) (Separator, bool, error) {
	if fact.Actual == fact.Expected || fact.Actual < 0 || fact.Expected < 0 || !fact.Span.Present() {
		return Separator{}, false, nil
	}
	if err := checkpoint(ctx, int64(lines.PositionWork(fact.Span.Start()))); err != nil {
		return Separator{}, false, err
	}
	position, err := lines.Position(fact.Span.Start())
	if err != nil {
		return Separator{}, false, nil
	}
	markerLine := position.Line
	if fact.Actual > fact.Expected {
		markerLine = position.Line - fact.Expected + fact.Actual
	}
	gap := markerLine - fact.Actual
	if gap < 2 || markerLine > lines.LineCount() {
		return Separator{}, false, nil
	}
	content := func(n int) (string, parser.PhysicalLine, error) {
		line, err := lines.Line(n)
		if err != nil {
			return "", line, err
		}
		if err := checkpoint(ctx, int64(line.Content.End()-line.Content.Start())+1); err != nil {
			return "", line, err
		}
		text, err := source.Text(line.Content)
		return text, line, err
	}
	text, marker, err := content(markerLine)
	if err != nil {
		return Separator{}, false, err
	}
	k := 0
	for k < len(text) && (text[k] == ' ' || text[k] == '>') {
		k++
	}
	if k == len(text) || text[k] == '\t' || fact.Actual < fact.Expected && marker.Content.Start()+k != fact.Span.Start() {
		return Separator{}, false, nil
	}
	prefix := text[:k]
	blank := strings.TrimRight(prefix, " ")
	for n := gap; n < markerLine; n++ {
		text, _, err := content(n)
		if err != nil {
			return Separator{}, false, err
		}
		if strings.TrimRight(text, " ") != blank {
			return Separator{}, false, nil
		}
	}
	previous, before, err := content(gap - 1)
	if err != nil {
		return Separator{}, false, err
	}
	if !strings.HasPrefix(previous, prefix) || strings.TrimRight(previous, " ") == blank {
		return Separator{}, false, nil
	}
	end := before.Content.End()
	if err := checkpoint(ctx, int64(len(unclosed))+1); err != nil {
		return Separator{}, false, err
	}
	if i := sort.Search(len(unclosed), func(i int) bool { return unclosed[i].Start() >= end }); i > 0 && unclosed[i-1].End() >= end {
		return Separator{}, false, nil
	}
	at := marker.Content.Start()
	if fact.Actual < fact.Expected {
		// fix.InsertionEnding picks the nearest complete ending, preceding winning
		// ties. At a line start after a terminated line that is the line's own
		// predecessor at distance zero, so reading it directly keeps the choice
		// without a whole-document scan per insertion.
		preceding, err := lines.Line(markerLine - 1)
		if err != nil {
			return Separator{}, false, err
		}
		ending, err := source.Text(preceding.Ending)
		if err != nil || ending == "" {
			return Separator{}, false, err
		}
		missing := fact.Expected - fact.Actual
		if err := checkpoint(ctx, int64((len(blank)+len(ending))*missing)+1); err != nil {
			return Separator{}, false, err
		}
		point, err := source.Span(at, at)
		if err != nil {
			return Separator{}, false, err
		}
		scope, err := source.Span(before.Content.End(), at+k+1)
		if err != nil {
			return Separator{}, false, nil
		}
		replacement := bytes.Repeat([]byte(blank+ending), missing)
		return Separator{Edit: fix.Edit{Span: point, Expected: []byte{}, Replacement: replacement}, Scope: scope, Marker: at + k}, true, nil
	}
	first, err := lines.Line(gap + fact.Expected)
	if err != nil {
		return Separator{}, false, err
	}
	last, err := lines.Line(gap + fact.Actual - 1)
	if err != nil {
		return Separator{}, false, err
	}
	span, err := source.Span(first.Content.Start(), last.Ending.End())
	if err != nil {
		return Separator{}, false, err
	}
	if err := checkpoint(ctx, int64(span.End()-span.Start())+1); err != nil {
		return Separator{}, false, err
	}
	expected, err := source.Text(span)
	if err != nil {
		return Separator{}, false, err
	}
	scope, err := source.Span(before.Content.End(), span.End())
	if err != nil {
		return Separator{}, false, err
	}
	return Separator{Edit: fix.Edit{Span: span, Expected: []byte(expected)}, Scope: scope, Marker: at + k}, true, nil
}

// Groups regenerates one group for each eligible active MD076 finding from this
// round's own separator facts. The source of truth is the finding plus the facts.
func Groups(ctx context.Context, c fix.RoundContext, findings []result.Diagnostic) ([]fix.Group, error) {
	if len(findings) == 0 {
		return nil, nil
	}
	facts, err := c.ListItemSpacing(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Reserve(ctx, int64(len(facts))+1, int64(len(facts))); err != nil {
		return nil, err
	}
	unclosed, err := Unclosed(ctx, c.Parsed().Document, c.Checkpoint)
	if err != nil {
		return nil, err
	}
	byStart := make(map[int]parser.ListItemSpacingFact, len(facts))
	for _, f := range facts {
		byStart[f.Span.Start()] = f
	}
	groups := make([]fix.Group, 0, len(findings))
	for _, d := range findings {
		if err := c.Reserve(ctx, 128, 12); err != nil {
			return nil, err
		}
		f, ok := byStart[d.ByteRange.Start]
		if !ok || f.Span.End() != d.ByteRange.End {
			continue
		}
		s, ok, err := Correct(ctx, c.Current(), c.Lines(), unclosed, f, c.Checkpoint)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		groups = append(groups, fix.Group{ID: fmt.Sprintf("MD076-separator-%d", f.Span.Start()), Rule: "MD076", Provenance: "MD076 list item separator", Anchor: f.Span, Edits: []fix.Edit{s.Edit}, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return groups, nil
}

// listSpacingRound holds the facts every MD076 group of a round shares. It is
// computed once, so proof cost does not grow with the group count.
type listSpacingRound struct {
	separators map[int]Separator
	anchors    map[int]parser.Span
	applied    map[int]fix.Edit
	preserved  bool
}

// Check authorizes one group against the before parse, never against producer
// data. Whole-candidate proof runs once for the round. Directive structure is
// compared by the validator's shared preservation.
func Check(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("list-item-spacing", func() (any, error) { return listSpacingProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(listSpacingRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD076" || len(g.Edits) != 1 {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	s, ok := round.separators[g.Anchor.Start()]
	actual, applied := round.applied[e.Span.Start()]
	if !ok || !applied || round.anchors[g.Anchor.Start()] != g.Anchor || s.Edit.Span != e.Span || actual.Span != e.Span || !bytes.Equal(e.Expected, s.Edit.Expected) || !bytes.Equal(e.Replacement, s.Edit.Replacement) || !bytes.Equal(actual.Replacement, s.Edit.Replacement) {
		return fix.Unsafe, nil
	}
	return fix.Preserved, nil
}

// listSpacingProof recomputes every separator edit from the before parse, proves
// that only the round's edits changed bytes, and compares both parses: only the
// lists that own a corrected separator may change tightness.
func listSpacingProof(ctx context.Context, c fix.CheckContext) (listSpacingRound, error) {
	failed := listSpacingRound{}
	facts, err := c.Before.ListItemSpacing(ctx)
	if err != nil {
		return failed, err
	}
	_, edits, err := c.BoundedViews(ctx)
	if err != nil {
		return failed, err
	}
	if err := c.Before.Reserve(ctx, int64(len(facts)+len(edits))*3+1, int64(len(facts)*2+len(edits))+1); err != nil {
		return failed, err
	}
	round := listSpacingRound{separators: map[int]Separator{}, anchors: map[int]parser.Span{}, applied: make(map[int]fix.Edit, len(edits))}
	for _, e := range edits {
		round.applied[e.Edit.Span.Start()] = e.Edit
	}
	unclosed, err := Unclosed(ctx, c.Before.Parsed().Document, c.Before.Checkpoint)
	if err != nil {
		return failed, err
	}
	markers := map[int]bool{}
	for _, f := range facts {
		s, ok, err := Correct(ctx, c.Before.Current(), c.Before.Lines(), unclosed, f, c.Before.Checkpoint)
		if err != nil {
			return failed, err
		}
		if !ok {
			continue
		}
		round.separators[f.Span.Start()] = s
		round.anchors[f.Span.Start()] = f.Span
		if e, ok := round.applied[s.Edit.Span.Start()]; ok && e.Span == s.Edit.Span && bytes.Equal(e.Replacement, s.Edit.Replacement) {
			markers[s.Marker] = true
		}
	}
	if unchanged, err := c.OnlyEditsChanged(ctx); err != nil || !unchanged {
		return failed, err
	}
	if same, err := c.MeaningPreservedAcrossListSeparators(ctx, markers); err != nil || !same {
		return failed, err
	}
	round.preserved = true
	return round, c.Before.Checkpoint(ctx, 0)
}
