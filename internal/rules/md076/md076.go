// Package md076 checks MD076, which requires consistent blank lines between list items.
package md076

import (
	"context"
	"errors"
	"fmt"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules/internal/listspacing"
)

// Registration returns the MD076 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD076", Check: consistentListItemSpacing}
}

func consistentListItemSpacing(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, err := c.ListItemSpacing(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return compareListItemSpacing(ctx, c, items)
}

func compareListItemSpacing(ctx context.Context, c engine.RuleContext, items []parser.ListItemSpacingFact) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	edits := c.InteriorEdits()
	var unclosed []parser.Span
	if len(items) != 0 {
		var err error
		if unclosed, err = listspacing.Unclosed(ctx, c.Document(), c.Checkpoint); err != nil {
			out.Err = err
			return out
		}
	}
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
		message := fmt.Sprintf("List item separator has %d blank body lines; expected %d.", item.Actual, item.Expected)
		finding := engine.Finding{Span: item.Span, Message: message}
		// A gap whose ancestor prefixes cannot be reproduced exactly stays manual.
		separator, ok, err := listspacing.Correct(ctx, c.Source(), c.Lines(), unclosed, item, c.Checkpoint)
		if err != nil {
			out.Err = err
			return out
		}
		if ok {
			finding.FixEligible, err = edits.Eligible(ctx, "MD076", item.Span, separator.Scope)
			if err != nil {
				out.Err = err
				return out
			}
		}
		out.Findings = append(out.Findings, finding)
	}
	return out
}
