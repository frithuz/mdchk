// Package md056 checks MD056.
package md056

import (
	"context"
	"errors"
	"strconv"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// Registration returns the MD056 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD056", Check: mismatchedTableCells}
}

func mismatchedTableCells(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
			out.IncompleteReason = ""
		}
	}()
	rows, err := c.TableCellCounts(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return tableCellCountOutcome(ctx, c, rows)
}

func tableCellCountOutcome(ctx context.Context, c engine.RuleContext, rows parser.TableCellCountsInventory) engine.RuleOutcome {
	switch rows.Availability {
	case parser.Available:
		return compareTableCellCounts(ctx, c, rows.Rows)
	case parser.Unavailable:
		return engine.RuleOutcome{IncompleteReason: "Table row context is unavailable; the complete MD056 inventory is withheld."}
	default:
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
}

// tableCellCountNoun spells a count with its singular or plural noun.
func tableCellCountNoun(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

func compareTableCellCounts(ctx context.Context, c engine.RuleContext, rows []parser.TableCellCountRow) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	// Body rows never overlap and arrive in document order.
	previous := 0
	for i := 0; ; i++ {
		if out.Err = c.SectionCheckpoint(ctx, 1, 0); out.Err != nil {
			return out
		}
		if i == len(rows) {
			break
		}
		row := rows[i]
		if out.Err = c.SectionCheckpoint(ctx, 24, 0); out.Err != nil {
			return out
		}
		if row.RawCells < 1 || row.Width < 1 || row.Start < previous || row.End <= row.Start || row.End > c.Source().Len() {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		previous = row.End
		for _, point := range [2]int{row.Start, row.End} {
			if out.Err = c.SectionCheckpoint(ctx, 9, 0); out.Err != nil {
				return out
			}
			window, err := c.Source().Span(max(0, point-1), min(c.Source().Len(), point+1))
			if err != nil {
				return engine.RuleOutcome{Err: parser.ErrModel}
			}
			text, err := c.Source().Text(window)
			at := point - window.Start()
			if err != nil || point < c.Source().Len() && !utf8.RuneStart(text[at]) || point > 0 && point < c.Source().Len() && text[at-1] == '\r' && text[at] == '\n' {
				return engine.RuleOutcome{Err: parser.ErrModel}
			}
		}
		if row.RawCells == row.Width {
			continue
		}
		span, err := c.Source().Span(row.Start, row.End)
		if err != nil {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		finding := engine.Finding{Span: span, Message: "Row has " + tableCellCountNoun(row.RawCells, "cell") + "; table has " + tableCellCountNoun(row.Width, "column") + "."}
		if len(out.Findings) == cap(out.Findings) {
			// Each row produces at most one finding.
			capacity := min(len(rows), max(1, cap(out.Findings)*2))
			if out.Err = c.SectionCheckpoint(ctx, int64(len(out.Findings))+int64(capacity)+1, int64(capacity)); out.Err != nil {
				return out
			}
			next := make([]engine.Finding, len(out.Findings), capacity)
			copy(next, out.Findings)
			out.Findings = next
		}
		if out.Err = c.SectionCheckpoint(ctx, 1, 1); out.Err != nil {
			return out
		}
		out.Findings = append(out.Findings, finding)
	}
	return out
}
