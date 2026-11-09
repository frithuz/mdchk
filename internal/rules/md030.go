package rules

import (
	"context"
	"errors"
	"strings"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func listMarkerSpacing(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	spaces, err := c.ListMarkerSpaces(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	items, err := c.ListSeparators(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return compareListMarkerSpacing(ctx, c, items, spaces)
}

func compareListMarkerSpacing(ctx context.Context, c engine.RuleContext, items []parser.ListSeparatorFact, spaces int) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	edits := c.InteriorEdits()
	for _, item := range items {
		if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
			out.Err = err
			return out
		}
		if !item.FirstLineNonempty || item.Padding == spaces {
			continue
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
		finding := engine.Finding{Span: item.Separator, Message: "List marker separators must use the configured structural spacing."}
		if listSeparatorShape(item, spaces) {
			if err := c.Checkpoint(ctx, int64(item.Separator.End()-item.Separator.Start())+1); err != nil {
				out.Err = err
				return out
			}
			text, err := c.Source().Text(item.Separator)
			if err != nil {
				out.Err = err
				return out
			}
			if strings.Trim(text, " ") == "" {
				finding.FixEligible, err = edits.Eligible(ctx, "MD030", item.Separator, item.Separator)
				if err != nil {
					out.Err = err
					return out
				}
			}
		}
		out.Findings = append(out.Findings, finding)
	}
	return out
}

// listSeparatorShape reports whether rewriting a mismatched separator to spaces
// spaces moves nothing but the item's own single-line content: no continuation,
// child, nested item, code fallback, or following line the new column would
// absorb. Callers still require the separator bytes to be spaces only.
func listSeparatorShape(item parser.ListSeparatorFact, spaces int) bool {
	if spaces < 1 || spaces > 4 || !item.FirstLineNonempty || !item.OneLine || item.CodeFallback || item.Padding == spaces {
		return false
	}
	if item.Following < 0 {
		return true
	}
	return !item.Quoted && item.Following < item.Column+spaces && item.Following < item.Column+item.Padding
}
