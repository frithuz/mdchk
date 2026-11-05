package rules

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func outerTablePipes(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
			out.IncompleteReason = ""
		}
	}()
	rows, err := c.TableOuterPipes(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return tableOuterPipeOutcome(ctx, c, rows)
}

func tableOuterPipeOutcome(ctx context.Context, c engine.RuleContext, rows parser.TableOuterPipesInventory) engine.RuleOutcome {
	switch rows.Availability {
	case parser.Available:
		style, ok := c.Options()["style"].(string)
		if !ok {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		return compareTableOuterPipes(ctx, c, rows.Rows, style)
	case parser.Unavailable:
		return engine.RuleOutcome{IncompleteReason: "Table row context is unavailable; the complete MD055 inventory is withheld."}
	default:
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
}

// tableOuterPipeConvention names an expected leading/trailing pipe pair.
func tableOuterPipeConvention(leading, trailing bool) string {
	switch {
	case leading && trailing:
		return "leading and trailing pipes"
	case leading:
		return "a leading pipe only"
	case trailing:
		return "a trailing pipe only"
	default:
		return "no outer pipes"
	}
}

func compareTableOuterPipes(ctx context.Context, c engine.RuleContext, rows []parser.TableOuterPipeRow, style string) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	switch style {
	case "consistent", "leading_and_trailing", "none":
	default:
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
	// Rows never overlap and each table is a header, a delimiter and then body
	// rows, so a header row starts the next table.
	previous, role := 0, ""
	leading, trailing := style == "leading_and_trailing", style == "leading_and_trailing"
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
		switch row.Role {
		case "header":
			if role == "header" {
				return engine.RuleOutcome{Err: parser.ErrModel}
			}
		case "delimiter":
			if role != "header" {
				return engine.RuleOutcome{Err: parser.ErrModel}
			}
		case "body":
			if role != "delimiter" && role != "body" {
				return engine.RuleOutcome{Err: parser.ErrModel}
			}
		default:
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		role = row.Role
		if row.Start < previous || row.End <= row.Start || row.End > c.Source().Len() {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		previous = row.End
		lead, trail := row.LeadingPipe, row.TrailingPipe
		if lead.Present() && (lead.SourceToken() != c.Source().Token() || lead.Start() != row.Start || lead.End() != row.Start+1) || trail.Present() && (trail.SourceToken() != c.Source().Token() || trail.End() != row.End || trail.Start() != row.End-1) || lead.Present() && trail.Present() && lead.End() > trail.Start() {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
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
		if style == "consistent" && row.Role == "header" {
			// The header defines its table's convention and is never reported.
			leading, trailing = lead.Present(), trail.Present()
			continue
		}
		convention := tableOuterPipeConvention(leading, trailing)
		for _, edge := range [2]struct {
			expected bool
			pipe     parser.Span
			point    int
			name     string
		}{{leading, lead, row.Start, "leading"}, {trailing, trail, row.End, "trailing"}} {
			if edge.expected == edge.pipe.Present() {
				continue
			}
			finding := engine.Finding{Span: edge.pipe, Message: "Unexpected " + edge.name + " pipe; expected " + convention + "."}
			if edge.expected {
				span, err := c.Source().Span(edge.point, edge.point)
				if err != nil {
					return engine.RuleOutcome{Err: parser.ErrModel}
				}
				finding = engine.Finding{Span: span, Message: "Missing " + edge.name + " pipe; expected " + convention + "."}
			}
			if len(out.Findings) == cap(out.Findings) {
				capacity := 2 * len(rows)
				if cap(out.Findings) <= len(rows) {
					capacity = max(1, cap(out.Findings)*2)
				}
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
	if role == "header" {
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
	return out
}
