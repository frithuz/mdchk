// Package md060 checks MD060.
package md060

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// Registration returns the MD060 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD060", Check: tableColumnLayout}
}

func tableColumnLayout(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
			out.IncompleteReason = ""
		}
	}()
	tables, err := c.TableLayout(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return tableLayoutOutcome(ctx, c, tables)
}

func tableLayoutOutcome(ctx context.Context, c engine.RuleContext, tables parser.TableLayoutInventory) engine.RuleOutcome {
	switch tables.Availability {
	case parser.Available:
		style, ok := c.Options()["style"].(string)
		if !ok {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		return compareTableLayout(ctx, c, tables.Tables, style)
	case parser.Unavailable:
		return engine.RuleOutcome{IncompleteReason: "Table row context is unavailable; the complete MD060 inventory is withheld."}
	default:
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
}

// tableLayoutCellValid reports whether one cell matches the style. Left and
// right say whether a separator bounds that side; field is the cell's field and
// text its text. Width is the cell's column width W in code points.
func tableLayoutCellValid(style string, left, right bool, field, text string, leftPad, rightPad string, width int) bool {
	if strings.ContainsRune(leftPad, '\t') || strings.ContainsRune(rightPad, '\t') {
		return false
	}
	if text == "" {
		switch style {
		case "tight":
			return field == ""
		case "compact":
			return field == " "
		default:
			return field == strings.Repeat(" ", width+2)
		}
	}
	switch style {
	case "tight":
		return (!left || leftPad == "") && (!right || rightPad == "")
	case "compact":
		return (!left || leftPad == " ") && (!right || rightPad == " ")
	}
	length := utf8.RuneCountInString(field)
	switch {
	case left && right:
		return leftPad != "" && rightPad != "" && length == width+2
	case right:
		return rightPad != "" && length == width+1
	case left:
		return leftPad == " "
	default:
		// A cell without either pipe has no padding to align.
		return true
	}
}

func compareTableLayout(ctx context.Context, c engine.RuleContext, tables []parser.TableLayoutTable, style string) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	switch style {
	case "aligned", "compact", "tight":
	default:
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
	source := c.Source()
	// checkPoint rejects a row bound that splits a scalar or a CRLF.
	checkPoint := func(point int) error {
		if err := c.SectionCheckpoint(ctx, 9, 0); err != nil {
			return err
		}
		window, err := source.Span(max(0, point-1), min(source.Len(), point+1))
		if err != nil {
			return parser.ErrModel
		}
		text, err := source.Text(window)
		at := point - window.Start()
		if err != nil || point < source.Len() && !utf8.RuneStart(text[at]) || point > 0 && point < source.Len() && text[at-1] == '\r' && text[at] == '\n' {
			return parser.ErrModel
		}
		return nil
	}
	// Tables and rows never overlap and arrive in document order.
	previous := 0
	var widths []int
	for t := 0; ; t++ {
		if out.Err = c.SectionCheckpoint(ctx, 1, 0); out.Err != nil {
			return out
		}
		if t == len(tables) {
			break
		}
		rows := tables[t].Rows
		if out.Err = c.SectionCheckpoint(ctx, int64(len(rows))+1, 0); out.Err != nil {
			return out
		}
		if len(rows) < 2 {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		columns := 0
		for _, row := range rows {
			columns = max(columns, len(row.Cells))
		}
		if cap(widths) < columns {
			if out.Err = c.SectionCheckpoint(ctx, int64(columns)+1, int64(columns)); out.Err != nil {
				return out
			}
			widths = make([]int, columns)
		}
		widths = widths[:columns]
		clear(widths)
		// The first pass validates every row and cell and measures each
		// column's width; the second reports the cells that violate the style.
		for pass := range 2 {
			for i, row := range rows {
				if out.Err = c.SectionCheckpoint(ctx, 24, 0); out.Err != nil {
					return out
				}
				if pass == 0 {
					role := "body"
					if i < 2 {
						role = [2]string{"header", "delimiter"}[i]
					}
					if row.Role != role || len(row.Cells) == 0 || row.Start < previous || row.End <= row.Start || row.End > source.Len() {
						return engine.RuleOutcome{Err: parser.ErrModel}
					}
					previous = row.End
					for _, point := range [2]int{row.Start, row.End} {
						if out.Err = checkPoint(point); out.Err != nil {
							return out
						}
					}
				}
				span, err := source.Span(row.Start, row.End)
				if err != nil {
					return engine.RuleOutcome{Err: parser.ErrModel}
				}
				line, err := source.Text(span)
				if err != nil {
					return engine.RuleOutcome{Err: parser.ErrModel}
				}
				if out.Err = c.SectionCheckpoint(ctx, int64(len(line))+int64(len(row.Cells))+1, 0); out.Err != nil {
					return out
				}
				boundary := row.Start
				if row.LeadingPipe {
					boundary++
				}
				for j, cell := range row.Cells {
					last := j == len(row.Cells)-1
					left, right := j > 0 || row.LeadingPipe, !last || row.TrailingPipe
					if pass == 0 {
						end := cell.FieldEnd
						if last {
							end = row.End
							if row.TrailingPipe {
								end--
							}
						}
						if cell.FieldStart != boundary || cell.FieldEnd != end || cell.FieldStart > cell.TextStart || cell.TextStart > cell.TextEnd || cell.TextEnd > cell.FieldEnd || right && cell.FieldEnd >= row.End {
							return engine.RuleOutcome{Err: parser.ErrModel}
						}
						boundary = cell.FieldEnd + 1
					}
					field := line[cell.FieldStart-row.Start : cell.FieldEnd-row.Start]
					text := line[cell.TextStart-row.Start : cell.TextEnd-row.Start]
					leftPad := line[cell.FieldStart-row.Start : cell.TextStart-row.Start]
					rightPad := line[cell.TextEnd-row.Start : cell.FieldEnd-row.Start]
					if pass == 0 {
						if strings.Trim(leftPad, " \t") != "" || strings.Trim(rightPad, " \t") != "" || strings.Trim(text, " \t") != text || text == "" && (cell.TextStart != cell.FieldEnd || !left || !right) || !left && leftPad != "" || !right && rightPad != "" || text != "" && !utf8.RuneStart(text[0]) {
							return engine.RuleOutcome{Err: parser.ErrModel}
						}
						if left && line[cell.FieldStart-row.Start-1] != '|' || right && line[cell.FieldEnd-row.Start] != '|' {
							return engine.RuleOutcome{Err: parser.ErrModel}
						}
						widths[j] = max(widths[j], utf8.RuneCountInString(text))
						continue
					}
					if tableLayoutCellValid(style, left, right, field, text, leftPad, rightPad, widths[j]) {
						continue
					}
					span, err := source.Span(cell.FieldStart, cell.FieldEnd)
					if err != nil {
						return engine.RuleOutcome{Err: parser.ErrModel}
					}
					message := "Cell padding does not match the " + style + " style."
					if style == "aligned" {
						message = "Cell padding does not match the aligned style; column width is " + strconv.Itoa(widths[j]) + "."
					}
					if len(out.Findings) == cap(out.Findings) {
						capacity := max(1, cap(out.Findings)*2)
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
					out.Findings = append(out.Findings, engine.Finding{Span: span, Message: message})
				}
			}
		}
	}
	return out
}
