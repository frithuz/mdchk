package md065

import (
	"bytes"
	"context"
	"fmt"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

const (
	beforeMessage = "Horizontal rule requires a blank body line before it."
	afterMessage  = "Horizontal rule requires a blank body line after it."
)

// boundary is one insertion offset with the line every fact claiming it derives.
type boundary struct {
	line  []byte
	facts []parser.ThematicSpacingFact
}

// blankLineFacts indexes the derivable MD065 facts by the start of the line that
// receives the blank line. One break's after side and the next one's before side
// share a boundary and derive the same line, so both keep it. Facts that claim
// one boundary with different lines are ambiguous and left out, so none gets a
// group.
func blankLineFacts(ctx context.Context, source parser.Source, facts []parser.ThematicSpacingFact, checkpoint func(context.Context, int64) error) (map[int]boundary, error) {
	out := make(map[int]boundary, len(facts))
	ambiguous := map[int]bool{}
	for _, f := range facts {
		if err := checkpoint(ctx, int64(f.Prefix.End()-f.Prefix.Start()+f.Ending.End()-f.Ending.Start())+4); err != nil {
			return nil, err
		}
		line, ok, err := blankLine(source, f)
		if err != nil {
			return nil, err
		}
		at := f.At.Start()
		if !ok || ambiguous[at] {
			continue
		}
		if b, claimed := out[at]; claimed {
			if !bytes.Equal(b.line, line) {
				delete(out, at)
				ambiguous[at] = true
				continue
			}
			b.facts = append(b.facts, f)
			out[at] = b
			continue
		}
		out[at] = boundary{line: line, facts: []parser.ThematicSpacingFact{f}}
	}
	return out, nil
}

// side reports the side a diagnostic's message names.
func side(d result.Diagnostic) (bool, bool) {
	switch d.Message {
	case beforeMessage:
		return true, true
	case afterMessage:
		return false, true
	}
	return false, false
}

// Correction regenerates one atomic insertion group per eligible MD065 finding
// from the round's own parse. The finding's point and side join its fact; a
// finding without an unambiguous derivable fact gets no group. Two sides sharing
// a boundary propose identical edits, which Plan applies once and credits to
// both.
func Correction(ctx context.Context, c fix.RoundContext, diagnostics []result.Diagnostic) ([]fix.Group, error) {
	if len(diagnostics) == 0 {
		return nil, nil
	}
	facts, err := c.ThematicSpacing(ctx)
	if err != nil {
		return nil, err
	}
	byLine, err := blankLineFacts(ctx, c.Current(), facts, c.Checkpoint)
	if err != nil {
		return nil, err
	}
	type key struct {
		point  int
		before bool
	}
	byPoint := make(map[key]parser.ThematicSpacingFact, len(byLine))
	for _, b := range byLine {
		for _, f := range b.facts {
			byPoint[key{f.Point.Start(), f.Before}] = f
		}
	}
	groups := make([]fix.Group, 0, len(diagnostics))
	for _, d := range diagnostics {
		if err := c.Reserve(ctx, 128, 12); err != nil {
			return nil, err
		}
		before, ok := side(d)
		if !ok || d.ByteRange.End != d.ByteRange.Start {
			continue
		}
		f, ok := byPoint[key{d.ByteRange.Start, before}]
		if !ok {
			continue
		}
		at, err := c.Current().Span(f.At.Start(), f.At.Start())
		if err != nil {
			return nil, err
		}
		anchor, err := c.Current().Span(f.Point.Start(), f.Point.Start())
		if err != nil {
			return nil, err
		}
		name := "after"
		if f.Before {
			name = "before"
		}
		replacement := bytes.Clone(byLine[at.Start()].line)
		groups = append(groups, fix.Group{ID: fmt.Sprintf("MD065-%s-%d", name, at.Start()), Rule: "MD065", Provenance: "MD065 blank body line " + name + " horizontal rule", Anchor: anchor, Edits: []fix.Edit{{Span: at, Replacement: replacement}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return groups, c.Checkpoint(ctx, 0)
}

// blankLineRound holds the facts every MD065 group of a round shares. It is
// computed once, so proof cost does not grow with the group count.
type blankLineRound struct {
	boundaries map[int]boundary
	edits      map[int]fix.Edit
	preserved  bool
}

// Checker returns the MD065 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD065", Check: checkBlankLine}
}

// checkBlankLine authorizes one group against the before parse, trusting neither
// producer data nor fix flags. The whole candidate must differ from the before
// source only by the round's edits, both parses must have the same blocks in the
// same order with the same properties and text, so no break turns into a setext
// underline and no list changes tightness, items or nesting, and both must hold
// the same thematic breaks in the same containers. The group's one edit must be
// the exact insertion Before's fact requires at its boundary. Shared protection,
// binding and directive checks remain mandatory in Validator.
func checkBlankLine(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("horizontal-rule-blank-line", func() (any, error) { return blankLineProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(blankLineRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD065" || len(g.Edits) != 1 {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	b, ok := round.boundaries[e.Span.Start()]
	actual, applied := round.edits[e.Span.Start()]
	if !ok || !applied || e.Span.Start() != e.Span.End() || len(e.Expected) != 0 || actual.Span != e.Span || len(actual.Expected) != 0 {
		return fix.Unsafe, nil
	}
	anchored := false
	for _, f := range b.facts {
		anchored = anchored || f.Point == g.Anchor && f.At == e.Span
	}
	if !anchored || !bytes.Equal(e.Replacement, b.line) || !bytes.Equal(actual.Replacement, b.line) {
		return fix.Unsafe, nil
	}
	return fix.Preserved, nil
}

// blankLineProof recomputes the derivable facts from the before parse, proves
// the combined candidate byte for byte and then compares both parses.
func blankLineProof(ctx context.Context, c fix.CheckContext) (blankLineRound, error) {
	failed := blankLineRound{}
	facts, err := c.Before.ThematicSpacing(ctx)
	if err != nil {
		return failed, err
	}
	byLine, err := blankLineFacts(ctx, c.Before.Current(), facts, c.Before.Checkpoint)
	if err != nil {
		return failed, err
	}
	_, edits, err := c.BoundedViews(ctx)
	if err != nil {
		return failed, err
	}
	if err := c.Before.Reserve(ctx, int64(len(edits))*2+1, int64(len(edits))+1); err != nil {
		return failed, err
	}
	round := blankLineRound{boundaries: byLine, edits: make(map[int]fix.Edit, len(edits))}
	for _, e := range edits {
		round.edits[e.Edit.Span.Start()] = e.Edit
	}
	if unchanged, err := c.OnlyEditsChanged(ctx); err != nil || !unchanged {
		return failed, err
	}
	if same, err := c.MeaningPreservedAcrossLineEndWhitespace(ctx); err != nil || !same {
		return failed, err
	}
	if same, err := sameBreaks(ctx, c); err != nil || !same {
		return failed, err
	}
	round.preserved = true
	return round, c.Before.Checkpoint(ctx, 0)
}

// thematicBreak is one parsed break: its marker byte, its envelope and the kinds
// of its containers, outermost first.
type thematicBreak struct {
	marker     byte
	envelope   parser.Span
	containers []parser.Kind
}

// thematicBreaks lists a round's thematic breaks in tree order. Breaks are
// blocks, so only the document and its container blocks are walked.
func thematicBreaks(ctx context.Context, c fix.RoundContext) ([]thematicBreak, error) {
	type frame struct {
		node       *parser.Node
		containers []parser.Kind
	}
	stack := []frame{{node: c.Parsed().Document.Root()}}
	var out []thematicBreak
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		var children []*parser.Node
		if kind := f.node.Kind(); kind == parser.DocumentKind || kind == parser.BlockQuote || kind == parser.List || kind == parser.ListItem {
			children = f.node.Children()
		}
		if err := c.Reserve(ctx, int64(len(children)+len(f.containers))+1, int64(len(children))+1); err != nil {
			return nil, err
		}
		if f.node.Kind() == parser.ThematicBreak {
			b := thematicBreak{envelope: f.node.Span(), containers: f.containers}
			for _, marker := range f.node.Markers() {
				if marker.Role != "thematic-marker" {
					continue
				}
				text, err := c.Current().Text(marker.Source)
				if err != nil || text == "" {
					return nil, parser.ErrModel
				}
				b.marker = text[0]
				break
			}
			out = append(out, b)
		}
		if len(children) == 0 {
			continue
		}
		containers := append(append([]parser.Kind(nil), f.containers...), f.node.Kind())
		for i := len(children) - 1; i >= 0; i-- {
			stack = append(stack, frame{node: children[i], containers: containers})
		}
	}
	return out, c.Checkpoint(ctx, 0)
}

// sameBreaks reports whether both parses hold the same thematic breaks, with the
// same markers, in the same containers and at the same mapped envelopes.
func sameBreaks(ctx context.Context, c fix.CheckContext) (bool, error) {
	before, err := thematicBreaks(ctx, c.Before)
	if err != nil {
		return false, err
	}
	after, err := thematicBreaks(ctx, c.After)
	if err != nil {
		return false, err
	}
	if len(before) != len(after) {
		return false, nil
	}
	for i, b := range before {
		a := after[i]
		if err := c.Before.Checkpoint(ctx, int64(len(b.containers))+1); err != nil {
			return false, err
		}
		if b.marker != a.marker || len(b.containers) != len(a.containers) {
			return false, nil
		}
		for j, kind := range b.containers {
			if kind != a.containers[j] {
				return false, nil
			}
		}
		same, err := c.SameEnvelope(b.envelope, a.envelope)
		if err != nil || !same {
			return false, err
		}
	}
	return true, nil
}
