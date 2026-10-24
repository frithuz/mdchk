package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// TerminalTaskSourcesPreserved compares actual task classification, original
// marker/token spelling and visible paragraph contributions across a suffix
// edit. Paragraph endings may extend; all task syntax and visible body facts
// must retain their original coordinates and spelling. This is a narrow source
// proof, not a substitute for the caller's exact suffix/body/protection proof.
func TerminalTaskSourcesPreserved(ctx context.Context, before, after parser.ParseResult, budget *parser.OperationBudget) (bool, error) {
	if ctx == nil || budget == nil {
		return false, errors.New("nil terminal task context or budget")
	}
	complete := func(r parser.ParseResult) bool {
		return r.Status == parser.Complete && r.AST == parser.Available && r.Phases.Blocks == parser.PhaseComplete && r.Phases.Inlines == parser.PhaseComplete && len(r.Uncertainty) == 0 && r.Document != nil && r.Document.Source() == r.Source
	}
	if !complete(before) || !complete(after) {
		return false, nil
	}
	a, err := before.Document.TaskSources(ctx, budget)
	if err != nil {
		return false, err
	}
	b, err := after.Document.TaskSources(ctx, budget)
	if err != nil {
		return false, err
	}
	if len(a) != len(b) {
		return false, nil
	}
	sameSpan := func(x, y parser.Span) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if x.Present() != y.Present() || x.Start() != y.Start() || x.End() != y.End() {
			return false, nil
		}
		if !x.Present() {
			return true, nil
		}
		if err := budget.ChargeWork(int64(x.End()-x.Start()) + int64(y.End()-y.Start()) + 1); err != nil {
			return false, err
		}
		old, e := before.Source.Text(x)
		if e != nil {
			return false, e
		}
		current, e := after.Source.Text(y)
		return old == current, e
	}
	for i, x := range a {
		y := b[i]
		p, q := x.Properties, y.Properties
		if err := budget.ChargeWork(int64(len(x.Content)+len(y.Content)+len(p.Segments)+len(q.Segments)) + 16); err != nil {
			return false, err
		}
		if p.Paragraph.Start() != q.Paragraph.Start() || p.Checked != q.Checked || p.CheckSpelling != q.CheckSpelling || len(p.Segments) != len(q.Segments) || len(x.Content) != len(y.Content) {
			return false, nil
		}
		for j, s := range []parser.Span{p.Marker, p.Opening, p.Check, p.Closing, p.Separator} {
			t := []parser.Span{q.Marker, q.Opening, q.Check, q.Closing, q.Separator}[j]
			if ok, e := sameSpan(s, t); e != nil || !ok {
				return false, e
			}
		}
		for j, s := range p.Segments {
			if ok, e := sameSpan(s, q.Segments[j]); e != nil || !ok {
				return false, e
			}
		}
		for j, s := range x.Content {
			t := y.Content[j]
			if s.Transformation != t.Transformation || s.Semantic != t.Semantic {
				return false, nil
			}
			if ok, e := sameSpan(s.Source, t.Source); e != nil || !ok {
				return false, e
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return true, budget.ChargeWork(0)
}

// A line ending can supply the required separator to an otherwise literal task
// marker at EOF. Ask the existing whole-profile parser, never another recognizer.
// LF, CRLF and CR are all task whitespace; the independent candidate checker
// additionally compares the actual chosen ending and actual candidate parse.
func terminalGFMEndingEligible(ctx context.Context, before parser.ParseResult, budget *parser.OperationBudget, frontmatter parser.FrontmatterOptions) (bool, error) {
	if err := budget.ChargeWork(int64(before.Source.Len())*4 + 4); err != nil {
		return false, err
	}
	if err := budget.ChargeNodes(2); err != nil {
		return false, err
	}
	content := append(before.Source.Bytes(), '\n')
	after := parser.ParseWithOptions(ctx, parser.NewSource(content), budget, parser.ParseOptions{GFM: true, Frontmatter: frontmatter})
	if after.Limit != nil {
		return false, after.Limit
	}
	if after.Failure != nil {
		return false, after.Failure
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if after.Interruption != "" {
		return false, errors.New(string(after.Interruption))
	}
	return TerminalTaskSourcesPreserved(ctx, before, after, budget)
}
