// Package headingindent produces and checks the MD023 correction of indented
// headings: deleting the leaf indentation on every physical heading line.
package headingindent

import (
	"bytes"
	"context"
	"fmt"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// correctable indexes the round's indented headings that could be corrected by
// marker start: no lazy line and no partially consumed tab. Directive, protected
// and front-matter eligibility is the finding's own flag.
func correctable(ctx context.Context, headings []parser.IndentedHeading, checkpoint func(context.Context, int64) error) (map[int]parser.IndentedHeading, error) {
	out := make(map[int]parser.IndentedHeading, len(headings))
	for _, h := range headings {
		if err := checkpoint(ctx, int64(len(h.Lines))+1); err != nil {
			return nil, err
		}
		if h.Lazy || len(h.Lines) == 0 {
			continue
		}
		partial := false
		for _, line := range h.Lines {
			partial = partial || line.PartialTab
		}
		if !partial {
			out[h.Marker.Start()] = h
		}
	}
	return out, nil
}

// edits returns one deletion per indented heading line, in source order.
func edits(ctx context.Context, source parser.Source, h parser.IndentedHeading, checkpoint func(context.Context, int64) error) ([]fix.Edit, error) {
	out := make([]fix.Edit, 0, len(h.Lines))
	for _, line := range h.Lines {
		text, err := source.Text(line.Span)
		if err != nil {
			return nil, err
		}
		if err := checkpoint(ctx, int64(len(text))+1); err != nil {
			return nil, err
		}
		out = append(out, fix.Edit{Span: line.Span, Expected: []byte(text)})
	}
	return out, nil
}

// Groups regenerates one group for each eligible active MD023 finding, holding
// every leaf-indentation deletion of that heading so that a heading is never
// half corrected. The source of truth is the finding plus this round's parse.
func Groups(ctx context.Context, c fix.RoundContext, findings []result.Diagnostic) ([]fix.Group, error) {
	if len(findings) == 0 {
		return nil, nil
	}
	headings, err := c.IndentedHeadings(ctx)
	if err != nil {
		return nil, err
	}
	index, err := correctable(ctx, headings, c.Checkpoint)
	if err != nil {
		return nil, err
	}
	groups := make([]fix.Group, 0, len(findings))
	for _, d := range findings {
		if err := c.Reserve(ctx, 128, 12); err != nil {
			return nil, err
		}
		h, ok := index[d.ByteRange.Start]
		if !ok || h.Marker.End() != d.ByteRange.End {
			continue
		}
		deletions, err := edits(ctx, c.Current(), h, c.Checkpoint)
		if err != nil {
			return nil, err
		}
		groups = append(groups, fix.Group{ID: fmt.Sprintf("MD023-indentation-%d", h.Marker.Start()), Rule: "MD023", Provenance: "MD023 indented heading", Anchor: h.Marker, Edits: deletions, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return groups, nil
}

// indentationRound holds the facts every MD023 group of a round shares. It is
// computed once, so proof cost does not grow with the group count.
type indentationRound struct {
	before    map[int]parser.IndentedHeading
	after     map[int]bool
	preserved bool
}

// Check authorizes one group against the before parse, never against producer
// data. The whole candidate must differ only by the round's edits, keep the same
// block tree and headings, and the corrected heading must no longer be indented.
func Check(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("heading-indentation", func() (any, error) { return indentationProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(indentationRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	h, ok := round.before[g.Anchor.Start()]
	if g.Rule != "MD023" || !ok || h.Marker != g.Anchor || len(g.Edits) != len(h.Lines) {
		return fix.Unsafe, nil
	}
	expected, err := edits(ctx, c.Before.Current(), h, c.Before.Checkpoint)
	if err != nil {
		return fix.Unknown, err
	}
	for i, e := range g.Edits {
		if e.Span != expected[i].Span || len(e.Replacement) != 0 || !bytes.Equal(e.Expected, expected[i].Expected) {
			return fix.Unsafe, nil
		}
	}
	marker, ok, err := c.MapUnchanged(h.Marker)
	if err != nil {
		return fix.Unknown, err
	}
	if !ok || round.after[marker.Start()] {
		return fix.Unsafe, nil
	}
	return fix.Preserved, nil
}

// indentationProof compares the whole candidate with the before parse: only the
// round's edits changed bytes, the block tree keeps its kinds, containers and
// text, and every heading keeps its level, style, text and containers.
func indentationProof(ctx context.Context, c fix.CheckContext) (indentationRound, error) {
	failed := indentationRound{}
	if unchanged, err := c.OnlyEditsChanged(ctx); err != nil || !unchanged {
		return failed, err
	}
	if same, err := c.MeaningPreservedAcrossLineEndWhitespace(ctx); err != nil || !same {
		return failed, err
	}
	before, err := c.Before.Headings(ctx)
	if err != nil {
		return failed, err
	}
	after, err := c.After.Headings(ctx)
	if err != nil {
		return failed, err
	}
	if len(before) != len(after) {
		return failed, nil
	}
	for i, b := range before {
		a := after[i]
		if err := c.Before.Checkpoint(ctx, int64(len(b.Visible)+len(a.Visible)+len(b.Containers))+1); err != nil {
			return failed, err
		}
		if b.Level != a.Level || b.Style != a.Style || b.Closed != a.Closed || b.Visible != a.Visible || len(b.Containers) != len(a.Containers) {
			return failed, nil
		}
		for j, container := range b.Containers {
			if container.Kind != a.Containers[j].Kind {
				return failed, nil
			}
		}
	}
	indented, err := c.Before.IndentedHeadings(ctx)
	if err != nil {
		return failed, err
	}
	index, err := correctable(ctx, indented, c.Before.Checkpoint)
	if err != nil {
		return failed, err
	}
	remaining, err := c.After.IndentedHeadings(ctx)
	if err != nil {
		return failed, err
	}
	round := indentationRound{before: index, after: make(map[int]bool, len(remaining)), preserved: true}
	for _, h := range remaining {
		if err := c.Before.Checkpoint(ctx, 1); err != nil {
			return failed, err
		}
		round.after[h.Marker.Start()] = true
	}
	return round, nil
}
