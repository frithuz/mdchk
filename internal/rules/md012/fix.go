package md012

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// prefixMismatch is the deferral reason for a run whose excess lines retain
// different container prefixes than the blank line they would leave.
const prefixMismatch = "prefix-mismatch"

// Correction regenerates one atomic group per active MD012 finding from the
// round's own analysis and parse. Each group removes the whole physical lines
// past the first maximum of its run, so every surviving byte and line ending is
// kept. Directive intervals and protection are enforced through the planner.
func Correction(ctx context.Context, c fix.RoundContext, diagnostics []result.Diagnostic) ([]fix.Group, error) {
	findings := len(diagnostics)
	if findings == 0 {
		return nil, nil
	}
	runs, maximum, err := c.BlankRuns(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Reserve(ctx, int64(len(runs))*2+1, int64(len(runs))+int64(findings)*12); err != nil {
		return nil, err
	}
	byStart := make(map[int][]parser.BlankRunLine, len(runs))
	for _, run := range runs {
		byStart[run[maximum].Prefix.End()] = run
	}
	out := make([]fix.Group, 0, findings)
	for _, d := range diagnostics {
		run, ok := byStart[d.ByteRange.Start]
		if !ok {
			return nil, fmt.Errorf("MD012 finding at byte %d has no blank run fact", d.ByteRange.Start)
		}
		anchor, err := span(c.Current(), run, maximum)
		if err != nil {
			return nil, err
		}
		group := fix.Group{ID: fmt.Sprintf("MD012-line-%d", d.Range.Start.Line), Rule: "MD012", Provenance: "MD012 blank line run", Anchor: anchor, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}}
		edits, ok, err := runEdits(ctx, c, run, maximum)
		if err != nil {
			return nil, err
		}
		if !ok {
			group.DeferReason = prefixMismatch
		}
		group.Edits = edits
		out = append(out, group)
	}
	return out, c.Checkpoint(ctx, 0)
}

// span covers the excess lines of a run.
func span(src parser.Source, run []parser.BlankRunLine, maximum int) (parser.Span, error) {
	return src.Span(run[maximum].Line.Start(), run[len(run)-1].Line.End())
}

// runEdits derives one whole-line removal per excess line of a run. It reports
// false, with no edits, when an excess line's retained prefix differs from the
// surviving neighbour's, or from the run's first line when none survives.
func runEdits(ctx context.Context, c fix.RoundContext, run []parser.BlankRunLine, maximum int) ([]fix.Edit, bool, error) {
	if err := c.Reserve(ctx, int64(len(run))*2+1, int64(len(run))); err != nil {
		return nil, false, err
	}
	src := c.Current()
	prefix := func(l parser.BlankRunLine) (string, error) {
		text, err := src.Text(l.Prefix)
		return strings.TrimRight(text, " \t"), err
	}
	want, err := prefix(run[max(0, maximum-1)])
	if err != nil {
		return nil, false, err
	}
	edits := make([]fix.Edit, 0, len(run)-maximum)
	for _, l := range run[maximum:] {
		got, err := prefix(l)
		if err != nil {
			return nil, false, err
		}
		if got != want {
			return nil, false, nil
		}
		text, err := src.Text(l.Line)
		if err != nil {
			return nil, false, err
		}
		if err := c.Reserve(ctx, int64(len(text))+1, 1); err != nil {
			return nil, false, err
		}
		edits = append(edits, fix.Edit{Span: l.Line, Expected: []byte(text)})
	}
	return edits, true, nil
}

// blankRound holds the facts every MD012 group of a round shares. It is computed
// once, so proof cost does not grow with the group count.
type blankRound struct {
	runs      map[int][]parser.BlankRunLine
	edits     map[int]fix.Edit
	maximum   int
	preserved bool
}

// Checker returns the MD012 candidate checker. Removing blank lines can make a
// heading, fence, list, or math block lose a required neighbour, so a candidate
// creating one of those findings is rejected and the run stays manual.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD012", Check: checkBlankLines, ForbidNewFindings: []string{"MD022", "MD031", "MD032", "MD065"}}
}

// checkBlankLines authorizes one group against the before parse, trusting
// neither producer data nor fix flags. Whole-candidate proof runs once a round.
func checkBlankLines(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("blank-lines", func() (any, error) { return blankLinesProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(blankRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD012" || len(g.Edits) == 0 {
		return fix.Unsafe, nil
	}
	run, ok := round.runs[g.Edits[0].Span.Start()]
	if !ok {
		return fix.Unsafe, nil
	}
	anchor, err := span(c.Before.Current(), run, round.maximum)
	if err != nil {
		return fix.Unknown, err
	}
	if g.Anchor != anchor {
		return fix.Unsafe, nil
	}
	edits, ok, err := runEdits(ctx, c.Before, run, round.maximum)
	if err != nil {
		return fix.Unknown, err
	}
	if !ok || len(edits) != len(g.Edits) {
		return fix.Unsafe, nil
	}
	for i, e := range edits {
		actual, applied := round.edits[e.Span.Start()]
		got := g.Edits[i]
		if !applied || got.Span != e.Span || actual.Span != e.Span || len(got.Replacement) != 0 || len(actual.Replacement) != 0 || !bytes.Equal(got.Expected, e.Expected) {
			return fix.Unsafe, nil
		}
	}
	return fix.Preserved, nil
}

// blankLinesProof recomputes the runs from the before parse, proves the combined
// candidate byte for byte and then compares the block structure of both parses.
func blankLinesProof(ctx context.Context, c fix.CheckContext) (blankRound, error) {
	failed := blankRound{}
	runs, maximum, err := c.Before.BlankRuns(ctx)
	if err != nil {
		return failed, err
	}
	_, edits, err := c.BoundedViews(ctx)
	if err != nil {
		return failed, err
	}
	if err := c.Before.Reserve(ctx, int64(len(runs)+len(edits))*2+1, int64(len(runs)+len(edits))+1); err != nil {
		return failed, err
	}
	round := blankRound{runs: make(map[int][]parser.BlankRunLine, len(runs)), edits: make(map[int]fix.Edit, len(edits)), maximum: maximum}
	for _, run := range runs {
		round.runs[run[maximum].Line.Start()] = run
	}
	for _, e := range edits {
		round.edits[e.Edit.Span.Start()] = e.Edit
	}
	if unchanged, err := c.OnlyEditsChanged(ctx); err != nil || !unchanged {
		return failed, err
	}
	if same, err := c.MeaningPreservedAcrossLineEndWhitespace(ctx); err != nil || !same {
		return failed, err
	}
	if same, err := c.SameStructureAcrossBullets(ctx); err != nil || !same {
		return failed, err
	}
	round.preserved = true
	return round, c.Before.Checkpoint(ctx, 0)
}
