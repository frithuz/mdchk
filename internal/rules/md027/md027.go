// Package md027 checks MD027, which reports extra spaces after a blockquote marker.
package md027

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// Registration returns the MD027 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD027", Check: check}
}

func check(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, err := c.BlockquoteSpaces(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return compareBlockquoteSpaces(ctx, c, items)
}

func compareBlockquoteSpaces(ctx context.Context, c engine.RuleContext, items []parser.Span) (out engine.RuleOutcome) {
	edits := c.InteriorEdits()
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
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
		message := "Extra spaces after blockquote marker."
		finding := engine.Finding{Span: item, Message: message}
		// The fact holds only ASCII SPACEs; the producer rechecks the bytes.
		var err error
		finding.FixEligible, err = edits.Eligible(ctx, "MD027", item, item)
		if err != nil {
			out.Err = err
			return out
		}
		out.Findings = append(out.Findings, finding)
	}
	return out
}
