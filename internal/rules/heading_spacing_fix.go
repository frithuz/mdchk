package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
)

// correctableGap is one heading delimiter gap made up only of ASCII spaces, with
// more than one of them. Excess runs from the second space through the last, and
// the rules name those whose findings can own it.
type correctableGap struct {
	excess       parser.Span
	md019, md021 bool
}

// correctableGaps indexes the parser-confirmed gaps by excess start, so findings
// and groups join them without a nested scan. Each gap is judged alone.
func correctableGaps(ctx context.Context, source parser.Source, headings []parser.HeadingFact, checkpoint func(context.Context, int64) error) (map[int]correctableGap, error) {
	gaps := map[int]correctableGap{}
	add := func(gap parser.Span, md019, md021 bool) error {
		if !gap.Present() {
			return nil
		}
		if err := checkpoint(ctx, int64(gap.End()-gap.Start())*2+2); err != nil {
			return err
		}
		text, err := source.Text(gap)
		if err != nil {
			return err
		}
		if len(text) < 2 || strings.Trim(text, " ") != "" {
			return nil
		}
		excess, err := source.Span(gap.Start()+1, gap.End())
		if err != nil {
			return err
		}
		known := gaps[excess.Start()]
		gaps[excess.Start()] = correctableGap{excess, known.md019 || md019, known.md021 || md021}
		return nil
	}
	for _, h := range headings {
		if err := checkpoint(ctx, 1); err != nil {
			return nil, err
		}
		if h.Style != "atx" {
			continue
		}
		if h.HasRawContent {
			if err := add(h.OpeningSeparator, true, false); err != nil {
				return nil, err
			}
		}
		if h.Closed {
			if err := add(h.ClosedOpeningGap, false, true); err != nil {
				return nil, err
			}
			if err := add(h.ClosedClosingGap, false, true); err != nil {
				return nil, err
			}
		}
	}
	return gaps, nil
}

// headingSpacingGroups regenerates one group for each eligible active finding.
// Identical edits from MD019 and MD021 stay separate groups so that Plan credits
// both rules. The source of truth is the finding plus this round's heading facts.
func headingSpacingGroups(ctx context.Context, c fix.RoundContext, findings []result.Diagnostic) ([]fix.Group, error) {
	if len(findings) == 0 {
		return nil, nil
	}
	headings, err := c.Headings(ctx)
	if err != nil {
		return nil, err
	}
	gaps, err := correctableGaps(ctx, c.Current(), headings, c.Checkpoint)
	if err != nil {
		return nil, err
	}
	groups := make([]fix.Group, 0, len(findings))
	for _, d := range findings {
		if err := c.Reserve(ctx, 128, 12); err != nil {
			return nil, err
		}
		gap, ok := gaps[d.ByteRange.Start]
		if !ok || gap.excess.End() != d.ByteRange.End || (d.Rule == "MD019" && !gap.md019) || (d.Rule == "MD021" && !gap.md021) {
			continue
		}
		expected, err := c.Current().Text(gap.excess)
		if err != nil {
			return nil, err
		}
		if err := c.Checkpoint(ctx, int64(len(expected))); err != nil {
			return nil, err
		}
		groups = append(groups, fix.Group{ID: fmt.Sprintf("%s-spacing-%d", d.Rule, gap.excess.Start()), Rule: d.Rule, Provenance: d.Rule + " excess heading delimiter spaces", Anchor: gap.excess, Edits: []fix.Edit{{Span: gap.excess, Expected: []byte(expected)}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return groups, nil
}

// headingSpacingRound holds the facts every MD019 and MD021 group of a round
// shares. It is computed once, so proof cost does not grow with the group count.
type headingSpacingRound struct {
	gaps      map[int]correctableGap
	preserved bool
}

// checkHeadingSpacing authorizes one group against the before parse, never against
// producer data. Whole-candidate proof runs once for the round. Directive structure
// is compared by the validator's shared preservation, and deleting spaces outside
// comments cannot create a directive finding, so no ForbidNewFindings is registered.
func checkHeadingSpacing(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("heading-spacing", func() (any, error) { return headingSpacingProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(headingSpacingRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if len(g.Edits) != 1 || len(g.Edits[0].Replacement) != 0 {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	gap, ok := round.gaps[e.Span.Start()]
	if !ok || gap.excess != e.Span || g.Anchor != e.Span || (g.Rule == "MD019" && !gap.md019) || (g.Rule == "MD021" && !gap.md021) || (g.Rule != "MD019" && g.Rule != "MD021") {
		return fix.Unsafe, nil
	}
	expected, err := c.Before.Current().Text(gap.excess)
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

// headingSpacingProof compares the whole candidate with the before parse: only
// the round's edits changed bytes, and every heading keeps its level, style, closed
// state, visible text, raw label, containers and anchor base.
func headingSpacingProof(ctx context.Context, c fix.CheckContext) (headingSpacingRound, error) {
	failed := headingSpacingRound{}
	unchanged, err := c.OnlyEditsChanged(ctx)
	if err != nil || !unchanged {
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
		if b.Level != a.Level || b.Style != a.Style || b.Closed != a.Closed || b.HasRawContent != a.HasRawContent || b.Visible != a.Visible || len(b.Containers) != len(a.Containers) {
			return failed, nil
		}
		label, ok, err := c.MapFact(b.Label)
		if err != nil || !ok || label != a.Label {
			return failed, err
		}
		envelope, ok, err := c.MapEndpoints(b.Envelope)
		if err != nil || !ok || envelope != a.Envelope {
			return failed, err
		}
		for j, container := range b.Containers {
			mapped, ok, err := c.MapEndpoints(container.Envelope)
			if err != nil || !ok || container.Kind != a.Containers[j].Kind || mapped != a.Containers[j].Envelope {
				return failed, err
			}
		}
	}
	if same, err := sameHeadingAnchors(ctx, c); err != nil || !same {
		return failed, err
	}
	gaps, err := correctableGaps(ctx, c.Before.Current(), before, c.Before.Checkpoint)
	if err != nil {
		return failed, err
	}
	return headingSpacingRound{gaps, true}, nil
}

func sameHeadingAnchors(ctx context.Context, c fix.CheckContext) (bool, error) {
	before, availability, err := c.Before.HeadingAnchors(ctx)
	if err != nil || availability != resource.Available {
		return false, err
	}
	after, availability, err := c.After.HeadingAnchors(ctx)
	if err != nil || availability != resource.Available {
		return false, err
	}
	if len(before) != len(after) {
		return false, nil
	}
	for i, b := range before {
		if err := c.Before.Checkpoint(ctx, int64(len(b.Base.Value)+len(after[i].Base.Value))+1); err != nil {
			return false, err
		}
		if b.Base != after[i].Base {
			return false, nil
		}
	}
	return true, nil
}
