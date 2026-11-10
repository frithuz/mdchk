package rules

import (
	"context"
	"errors"
	"fmt"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func listContinuationIndentation(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, err := c.ListContinuationIndentation(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return compareListContinuationIndentation(ctx, c, items)
}

func compareListContinuationIndentation(ctx context.Context, c engine.RuleContext, items []parser.ListContinuationFact) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	edits := c.InteriorEdits()
	_, header := c.Document().Frontmatter()
	for _, item := range items {
		if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
			out.Err = err
			return out
		}
		if len(out.Findings) == cap(out.Findings) {
			capacity := min(len(items), max(1, len(out.Findings)*2))
			if err := c.SectionCheckpoint(ctx, int64(len(out.Findings))+1, int64(capacity)); err != nil {
				out.Err = err
				return out
			}
			next := make([]engine.Finding, len(out.Findings), capacity)
			copy(next, out.Findings)
			out.Findings = next
		}
		if err := c.SectionCheckpoint(ctx, 1, 1); err != nil {
			out.Err = err
			return out
		}
		message := fmt.Sprintf("List continuation has %d indentation columns; expected %d.", item.Actual, item.Expected)
		finding := engine.Finding{Span: item.Span, Message: message}
		// Tabs, quotes and any leading front matter block stay manual.
		if item.SpaceOnly && !header {
			span, err := listContinuationProbe(c.Source(), item.Span)
			if err != nil {
				out.Err = err
				return out
			}
			finding.FixEligible, err = edits.Eligible(ctx, "MD077", item.Span, span)
			if err != nil {
				out.Err = err
				return out
			}
		}
		out.Findings = append(out.Findings, finding)
	}
	return out
}

// listContinuationProbe is the nonempty span whose context decides an MD077 edit.
// A surplus deletes its own span. The shared query withholds insertions, so a
// deficit is judged by the first content byte its spaces land before.
func listContinuationProbe(source parser.Source, span parser.Span) (parser.Span, error) {
	if span.Start() != span.End() {
		return span, nil
	}
	return source.Span(span.Start(), span.Start()+1)
}
