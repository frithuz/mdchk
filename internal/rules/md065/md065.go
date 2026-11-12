// Package md065 checks and corrects MD065, which requires blank lines around horizontal rules.
package md065

import (
	"context"
	"errors"
	"strings"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules/internal/blankpoints"
)

// Registration returns the MD065 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD065", Check: horizontalRuleBlankLines}
}

func horizontalRuleBlankLines(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 0, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	items, err := c.ThematicSpacing(ctx)
	if err != nil {
		out.Err = err
		return out
	}
	return compareHorizontalRuleBlankLines(ctx, c, items)
}

// compareHorizontalRuleBlankLines reports each missing side. A side is fix
// eligible when its blank line can be derived and the shared insertion guards
// accept the boundary with its neighbour line.
func compareHorizontalRuleBlankLines(ctx context.Context, c engine.RuleContext, items []parser.ThematicSpacingFact) engine.RuleOutcome {
	edits := c.InteriorEdits()
	return blankpoints.CompareEligible(ctx, c, items, "Horizontal rule", func(ctx context.Context, item parser.ThematicSpacingFact) (bool, error) {
		if _, ok, err := blankLine(c.Source(), item); err != nil || !ok {
			return false, err
		}
		return edits.InsertionEligible(ctx, "MD065", item.Point, item.Neighbour)
	})
}

// blankLine derives the blank body line an MD065 correction inserts at the
// fact's boundary: the boundary line's ancestor container prefix without
// trailing whitespace, then the previous line's ending. A prefix holding
// anything but spaces and quote markers, such as a tab, a lazy or shallower
// neighbour, and a list item whose list could turn loose get no line.
func blankLine(source parser.Source, item parser.ThematicSpacingFact) ([]byte, bool, error) {
	if item.Loosens || item.Lazy || !item.At.Present() || !item.Neighbour.Present() || !item.Prefix.Present() || !item.Ending.Present() || item.Ending.Start() == item.Ending.End() {
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
	return []byte(strings.TrimRight(prefix, " \t") + ending), true, nil
}
