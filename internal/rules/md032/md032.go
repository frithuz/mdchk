// Package md032 checks and corrects MD032, which requires lists to be surrounded by blank lines.
package md032

import (
	"context"
	"errors"
	"strings"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// Registration returns the MD032 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD032", Check: listBlankLines}
}

func listBlankLines(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, err := c.ListSpacing(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return compareListBlankLines(ctx, c, items)
}

func compareListBlankLines(ctx context.Context, c engine.RuleContext, items []parser.ListSpacingFact) (out engine.RuleOutcome) {
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
		message := "List requires a blank body line after it."
		if item.Before {
			message = "List requires a blank body line before it."
		}
		finding := engine.Finding{Span: item.Point, Message: message}
		if _, ok, err := listBlankLine(c.Source(), item); err != nil {
			out.Err = err
			return out
		} else if ok && !item.Loosens {
			finding.FixEligible, err = edits.Eligible(ctx, "MD032", item.Point, item.Ending)
			if err != nil {
				out.Err = err
				return out
			}
		}
		out.Findings = append(out.Findings, finding)
	}
	return out
}

// listBlankLine derives the blank body line an MD032 correction inserts at the
// start of the fact's line: its ancestor container prefix without trailing
// whitespace, then the previous line's ending. A prefix holding anything but
// spaces and quote markers, such as a tab, is not classified and gets no line.
func listBlankLine(source parser.Source, item parser.ListSpacingFact) ([]byte, bool, error) {
	if !item.Prefix.Present() || !item.Ending.Present() || item.Ending.Start() == item.Ending.End() {
		return nil, false, nil
	}
	prefix, err := source.Text(item.Prefix)
	if err != nil {
		return nil, false, err
	}
	ending, err := source.Text(item.Ending)
	if err != nil {
		return nil, false, err
	}
	if strings.Trim(prefix, " >") != "" {
		return nil, false, nil
	}
	return []byte(strings.TrimRight(prefix, " ") + ending), true, nil
}
