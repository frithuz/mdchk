// Package md044 checks and corrects MD044, which requires configured names to use their configured capitalization.
package md044

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// Registration returns the MD044 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD044", Check: configuredNameCapitalization}
}

func configuredNameCapitalization(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	items, replacements, err := c.ConfiguredNameReplacements(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return compareConfiguredNames(ctx, c, items, replacements)
}

func compareConfiguredNames(ctx context.Context, c engine.RuleContext, items []parser.Span, replacements []string) (out engine.RuleOutcome) {
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
	for i, item := range items {
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
		message := "Configured name uses different capitalization."
		finding := engine.Finding{Span: item, Message: message}
		if ok, err := correctable(c, headings, item, replacements[i]); err != nil {
			out.Err = err
			return out
		} else if ok {
			finding.FixEligible, err = edits.Eligible(ctx, "MD044", item, item)
			if err != nil {
				out.Err = err
				return out
			}
		}
		out.Findings = append(out.Findings, finding)
	}
	return out
}

// correctable reports whether an occurrence has one replacement that keeps any
// heading anchor: inside a heading, the simple case fold must stay the same, so a
// fold such as STRASSE to Straße stays manual there.
func correctable(c engine.RuleContext, headings []parser.HeadingFact, item parser.Span, replacement string) (bool, error) {
	if replacement == "" {
		return false, nil
	}
	i := sort.Search(len(headings), func(i int) bool { return headings[i].Envelope.End() >= item.End() })
	if i == len(headings) || headings[i].Envelope.Start() > item.Start() {
		return true, nil
	}
	text, err := c.Source().Text(item)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(text, replacement), nil
}
