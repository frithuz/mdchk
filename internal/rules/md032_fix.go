package rules

import (
	"bytes"
	"context"
	"fmt"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// listBlankLineFacts indexes the derivable MD032 facts by the start of the line
// that receives the blank line. A line claimed by two facts is ambiguous and left
// out, so neither gets a group.
func listBlankLineFacts(ctx context.Context, source parser.Source, facts []parser.ListSpacingFact, checkpoint func(context.Context, int64) error) (map[int]parser.ListSpacingFact, error) {
	out := make(map[int]parser.ListSpacingFact, len(facts))
	claimed := map[int]bool{}
	for _, f := range facts {
		if err := checkpoint(ctx, int64(f.Prefix.End()-f.Prefix.Start())+4); err != nil {
			return nil, err
		}
		if _, ok, err := listBlankLine(source, f); err != nil {
			return nil, err
		} else if !ok || f.Loosens {
			continue
		}
		at := f.Prefix.Start()
		if claimed[at] {
			delete(out, at)
			continue
		}
		claimed[at] = true
		out[at] = f
	}
	return out, nil
}

// listBlankLineGroups regenerates one atomic insertion group per eligible MD032
// finding from the round's own parse. The finding's point joins its fact; a
// finding without an unambiguous derivable fact gets no group.
func listBlankLineGroups(ctx context.Context, c fix.RoundContext, diagnostics []result.Diagnostic) ([]fix.Group, error) {
	if len(diagnostics) == 0 {
		return nil, nil
	}
	facts, err := c.ListSpacing(ctx)
	if err != nil {
		return nil, err
	}
	byLine, err := listBlankLineFacts(ctx, c.Current(), facts, c.Checkpoint)
	if err != nil {
		return nil, err
	}
	byPoint := make(map[int]parser.ListSpacingFact, len(byLine))
	for _, f := range byLine {
		byPoint[f.Point.Start()] = f
	}
	groups := make([]fix.Group, 0, len(diagnostics))
	for _, d := range diagnostics {
		if err := c.Reserve(ctx, 128, 12); err != nil {
			return nil, err
		}
		f, ok := byPoint[d.ByteRange.Start]
		if !ok || d.ByteRange.End != d.ByteRange.Start {
			continue
		}
		replacement, _, err := listBlankLine(c.Current(), f)
		if err != nil {
			return nil, err
		}
		at, err := c.Current().Span(f.Prefix.Start(), f.Prefix.Start())
		if err != nil {
			return nil, err
		}
		side := "after"
		if f.Before {
			side = "before"
		}
		groups = append(groups, fix.Group{ID: fmt.Sprintf("MD032-%s-%d", side, at.Start()), Rule: "MD032", Provenance: "MD032 blank body line " + side + " list", Anchor: at, Edits: []fix.Edit{{Span: at, Replacement: replacement}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return groups, c.Checkpoint(ctx, 0)
}

// listBlankLineRound holds the facts every MD032 group of a round shares. It is
// computed once, so proof cost does not grow with the group count.
type listBlankLineRound struct {
	facts     map[int]parser.ListSpacingFact
	edits     map[int]fix.Edit
	preserved bool
}

// checkListBlankLine authorizes one group against the before parse, trusting
// neither producer data nor fix flags. The whole candidate must differ from the
// before source only by the round's edits, and both parses must have the same
// blocks in the same order with the same properties and text: every list keeps
// its items, nesting, markers, contents and tightness, and no paragraph, list or
// quote splits or merges. Shared protection, binding and directive checks remain
// mandatory in Validator.
func checkListBlankLine(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("list-blank-line", func() (any, error) { return listBlankLineProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(listBlankLineRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD032" || len(g.Edits) != 1 {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	f, ok := round.facts[e.Span.Start()]
	actual, applied := round.edits[e.Span.Start()]
	if !ok || !applied || e.Span.Start() != e.Span.End() || len(e.Expected) != 0 || g.Anchor != e.Span || actual.Span != e.Span {
		return fix.Unsafe, nil
	}
	replacement, _, err := listBlankLine(c.Before.Current(), f)
	if err != nil {
		return fix.Unknown, err
	}
	if !bytes.Equal(e.Replacement, replacement) || !bytes.Equal(actual.Replacement, replacement) {
		return fix.Unsafe, nil
	}
	return fix.Preserved, nil
}

// listBlankLineProof recomputes the derivable facts from the before parse, proves
// the combined candidate byte for byte and then compares both parses.
func listBlankLineProof(ctx context.Context, c fix.CheckContext) (listBlankLineRound, error) {
	failed := listBlankLineRound{}
	facts, err := c.Before.ListSpacing(ctx)
	if err != nil {
		return failed, err
	}
	byLine, err := listBlankLineFacts(ctx, c.Before.Current(), facts, c.Before.Checkpoint)
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
	round := listBlankLineRound{facts: byLine, edits: make(map[int]fix.Edit, len(edits))}
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
