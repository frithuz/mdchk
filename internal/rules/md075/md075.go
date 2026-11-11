// Package md075 checks MD075.
package md075

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// Registration returns the MD075 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD075", Check: headerlessPipeRows}
}

func headerlessPipeRows(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
			out.IncompleteReason = ""
		}
	}()
	rows, err := c.PipeRowCandidates(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return pipeRowCandidateOutcome(ctx, c, rows)
}

func pipeRowCandidateOutcome(ctx context.Context, c engine.RuleContext, rows parser.PipeRowCandidatesInventory) engine.RuleOutcome {
	switch rows.Availability {
	case parser.Available:
		return comparePipeRowRuns(ctx, c, rows.Rows)
	case parser.Unavailable:
		return engine.RuleOutcome{IncompleteReason: "Paragraph pipe-row context is unavailable; the complete MD075 inventory is withheld."}
	default:
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
}

// pipeRowRunReported applies the structure test to one maximal run: two or
// more lines with one shared cell count of at least 2, with leading and
// trailing pipes on every line, or with a delimiter-shaped line; or any run in
// a Paragraph that touches a Table.
func pipeRowRunReported(run []parser.PipeRowCandidate) bool {
	if run[0].NextToTable {
		return true
	}
	if len(run) < 2 {
		return false
	}
	counts, outer, delimiter := run[0].Cells >= 2, true, false
	for _, row := range run {
		counts = counts && row.Cells == run[0].Cells
		outer = outer && row.LeadingPipe && row.TrailingPipe
		delimiter = delimiter || row.Delimiter
	}
	return counts || outer || delimiter
}

func comparePipeRowRuns(ctx context.Context, c engine.RuleContext, rows []parser.PipeRowCandidate) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	// Candidate lines never overlap and arrive in document order; a run ends
	// at a new Paragraph or a skipped physical line.
	previous, first := 0, 0
	for i := 0; ; i++ {
		if out.Err = c.SectionCheckpoint(ctx, 1, 0); out.Err != nil {
			return out
		}
		if i < len(rows) {
			row := rows[i]
			if out.Err = c.SectionCheckpoint(ctx, 24, 0); out.Err != nil {
				return out
			}
			if row.Cells < 1 || row.Line < 1 || row.Paragraph < 0 || row.Start < previous || row.End <= row.Start || row.End > c.Source().Len() {
				return engine.RuleOutcome{Err: parser.ErrModel}
			}
			if i > 0 && (row.Paragraph < rows[i-1].Paragraph || row.Paragraph == rows[i-1].Paragraph && (row.Line <= rows[i-1].Line || row.NextToTable != rows[i-1].NextToTable)) {
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
			if i == first || row.Paragraph == rows[i-1].Paragraph && row.Line == rows[i-1].Line+1 {
				continue
			}
		}
		if i > first {
			run := rows[first:i]
			if out.Err = c.SectionCheckpoint(ctx, int64(len(run))+1, 0); out.Err != nil {
				return out
			}
			if pipeRowRunReported(run) {
				span, err := c.Source().Span(run[0].Start, run[len(run)-1].End)
				if err != nil {
					return engine.RuleOutcome{Err: parser.ErrModel}
				}
				finding := engine.Finding{Span: span, Message: "Pipe rows are not part of a table."}
				if len(out.Findings) == cap(out.Findings) {
					// Each run produces at most one finding.
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
		}
		if i == len(rows) {
			break
		}
		first = i
	}
	return out
}
