// Package md064 checks and corrects MD064, which reports repeated spaces in prose.
package md064

import (
	"context"
	"errors"
	"sort"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// Registration returns the MD064 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD064", Check: check}
}

func check(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, err := c.RepeatedProseSpaces(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return compareRepeatedProseSpaces(ctx, c, items)
}

func compareRepeatedProseSpaces(ctx context.Context, c engine.RuleContext, items []parser.Span) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	headings, err := c.Headings(ctx)
	if err != nil {
		out.Err = err
		return out
	}
	edits := c.InteriorEdits()
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
		message := "Repeated spaces in prose."
		finding := engine.Finding{Span: item, Message: message}
		// Heading text stays manual: deleting a space changes the heading's anchor,
		// which links in this or other documents may target.
		if !inHeading(headings, item) {
			finding.FixEligible, err = edits.Eligible(ctx, "MD064", item, item)
			if err != nil {
				out.Err = err
				return out
			}
		}
		out.Findings = append(out.Findings, finding)
	}
	return out
}

// inHeading reports whether span lies inside one of the headings, which are in
// document order and do not overlap.
func inHeading(headings []parser.HeadingFact, span parser.Span) bool {
	i := sort.Search(len(headings), func(i int) bool { return headings[i].Envelope.End() >= span.End() })
	return i < len(headings) && headings[i].Envelope.Start() <= span.Start()
}
