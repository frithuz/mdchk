package parser

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

const md060Example = "| A| B |\n|---|---|\n|1|2|\n"

// md060Row flattens one row: role, trimmed bounds, outer pipes and each cell's
// field and text bounds.
type md060Row struct {
	role              string
	start, end        int
	leading, trailing bool
	cells             [][4]int
}

func md060Rows(got TableLayoutInventory) [][]md060Row {
	var out [][]md060Row
	for _, table := range got.Tables {
		var rows []md060Row
		for _, row := range table.Rows {
			r := md060Row{row.Role, row.Start, row.End, row.LeadingPipe, row.TrailingPipe, nil}
			for _, cell := range row.Cells {
				r.cells = append(r.cells, [4]int{cell.FieldStart, cell.FieldEnd, cell.TextStart, cell.TextEnd})
			}
			rows = append(rows, r)
		}
		out = append(out, rows)
	}
	return out
}

var md060ExampleRows = [][]md060Row{{
	{"header", 0, 8, true, true, [][4]int{{1, 3, 2, 3}, {4, 7, 5, 6}}},
	{"delimiter", 9, 18, true, true, [][4]int{{10, 13, 10, 13}, {14, 17, 14, 17}}},
	{"body", 19, 24, true, true, [][4]int{{20, 21, 20, 21}, {22, 23, 22, 23}}},
}}

func md060Positive(t *testing.T) {
	t.Helper()
	d, b, index, options := md011Parse(t, md060Example, true)
	got, err := d.TableLayout(context.Background(), b, index, options)
	if err != nil || got.Availability != Available || !reflect.DeepEqual(md060Rows(got), md060ExampleRows) {
		t.Fatal("actual original positive prerequisite", md060Rows(got), err)
	}
}

func TestTableLayoutRows(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         [][]md060Row
	}{
		{"spec-example", md060Example, md060ExampleRows},
		{"no-outer-pipes", "  a | b\n--- | ---\n c\n", [][]md060Row{{
			{"header", 2, 7, false, false, [][4]int{{2, 4, 2, 3}, {5, 7, 6, 7}}},
			{"delimiter", 8, 17, false, false, [][4]int{{8, 12, 8, 11}, {13, 17, 14, 17}}},
			{"body", 19, 20, false, false, [][4]int{{19, 20, 19, 20}}},
		}}},
		{"empty-and-crlf", "| a ||\r\n|-|-|\t\r\n", [][]md060Row{{
			{"header", 0, 6, true, true, [][4]int{{1, 4, 2, 3}, {5, 5, 5, 5}}},
			{"delimiter", 8, 13, true, true, [][4]int{{9, 10, 9, 10}, {11, 12, 11, 12}}},
		}}},
		{"blank-cell", "| a |   |\n| - | - |\n", [][]md060Row{{
			{"header", 0, 9, true, true, [][4]int{{1, 4, 2, 3}, {5, 8, 8, 8}}},
			{"delimiter", 10, 19, true, true, [][4]int{{11, 14, 12, 13}, {15, 18, 16, 17}}},
		}}},
		{"leading-and-trailing-only", "| a | b\n- | - |\n", [][]md060Row{{
			{"header", 0, 7, true, false, [][4]int{{1, 4, 2, 3}, {5, 7, 6, 7}}},
			{"delimiter", 8, 15, false, true, [][4]int{{8, 10, 8, 9}, {11, 14, 12, 13}}},
		}}},
		{"escaped-and-code-pipes", "| `a\\|b` | `c|d` |\n| - | - | - |\n", [][]md060Row{{
			{"header", 0, 18, true, true, [][4]int{{1, 9, 2, 8}, {10, 13, 11, 13}, {14, 17, 14, 16}}},
			{"delimiter", 19, 32, true, true, [][4]int{{20, 23, 21, 22}, {24, 27, 25, 26}, {28, 31, 29, 30}}},
		}}},
		{"block-quote", "> a | b\n> --- | ---\n", [][]md060Row{{
			{"header", 2, 7, false, false, [][4]int{{2, 4, 2, 3}, {5, 7, 6, 7}}},
			{"delimiter", 10, 19, false, false, [][4]int{{10, 14, 10, 13}, {15, 19, 16, 19}}},
		}}},
		{"list-item", "- | a |\n  | - |\n", [][]md060Row{{
			{"header", 2, 7, true, true, [][4]int{{3, 6, 4, 5}}},
			{"delimiter", 10, 15, true, true, [][4]int{{11, 14, 12, 13}}},
		}}},
		{"two-tables", "a | b\n--- | ---\n\n| c |\n| - |\n", [][]md060Row{{
			{"header", 0, 5, false, false, [][4]int{{0, 2, 0, 1}, {3, 5, 4, 5}}},
			{"delimiter", 6, 15, false, false, [][4]int{{6, 10, 6, 9}, {11, 15, 12, 15}}},
		}, {
			{"header", 17, 22, true, true, [][4]int{{18, 21, 19, 20}}},
			{"delimiter", 23, 28, true, true, [][4]int{{24, 27, 25, 26}}},
		}}},
		{"fenced", "```\na | b\n--- | ---\n```\n", nil},
		{"html-block", "<div>\na | b\n--- | ---\n</div>\n", nil},
		{"no-delimiter", "| a |\n| b |\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, b, index, o := md011Parse(t, tc.source, true)
			got, e := d.TableLayout(context.Background(), b, index, o)
			if e != nil || got.Availability != Available || !reflect.DeepEqual(md060Rows(got), tc.want) {
				t.Fatal(md060Rows(got), got.Availability, e)
			}
		})
	}
	d, b, index, o := md011Parse(t, md060Example, false)
	if got, e := d.TableLayout(context.Background(), b, index, o); e != nil || got.Availability != Available || got.Tables != nil {
		t.Fatal("CommonMark has no tables", got, e)
	}
}

func TestTableLayoutOriginalAuthority(t *testing.T) {
	for _, fault := range []string{"original", "detached", "wrong-source", "wrong-budget", "wrong-options", "incomplete", "nil-document", "nil-context"} {
		t.Run(fault, func(t *testing.T) {
			md060Positive(t)
			d, b, i, o := md011Parse(t, md060Example, true)
			ctx := context.Background()
			switch fault {
			case "detached":
				d, _ = NewDocument(d.Root(), d.References())
			case "wrong-source":
				i = NewLineIndex(NewSource([]byte(md060Example)))
			case "wrong-budget":
				b, _ = NewOperationBudget(DefaultLimits())
			case "wrong-options":
				o.GFM = !o.GFM
			case "incomplete":
				d.parsedComplete = false
			case "nil-document":
				d = nil
			case "nil-context":
				ctx = nil
			}
			got, e := d.TableLayout(ctx, b, i, o)
			if fault == "original" {
				if e != nil || len(got.Tables) != 1 || len(got.Tables[0].Rows) != 3 {
					t.Fatal(got, e)
				}
			} else if !errors.Is(e, ErrModel) || got.Tables != nil {
				t.Fatal(got, e)
			}
		})
	}
}

func TestTableLayoutUnknownAndMalformed(t *testing.T) {
	for _, fault := range []string{"table-properties", "row-properties", "cell-properties", "envelope", "foreign", "cell-envelope", "cell-kind"} {
		t.Run(fault, func(t *testing.T) {
			md060Positive(t)
			d, b, i, o := md011Parse(t, md060Example, true)
			rows, cells := md070Nodes(d, TableRow), md070Nodes(d, TableCell)
			switch fault {
			case "table-properties":
				md070Nodes(d, Table)[0].spec.Properties.Table = nil
			case "row-properties":
				rows[len(rows)-1].spec.Properties.Row = nil
			case "cell-properties":
				cells[len(cells)-1].spec.Properties.Cell = nil
			case "envelope":
				rows[len(rows)-1].spec.Envelope.end = i.source.Len() + 1
			case "foreign":
				rows[len(rows)-1].spec.Envelope.source = NewSource(i.source.Bytes())
			case "cell-envelope":
				cells[len(cells)-1].spec.Envelope.present = false
			case "cell-kind":
				rows[len(rows)-1].spec.Children[0] = rows[len(rows)-1]
			}
			got, e := d.TableLayout(context.Background(), b, i, o)
			if strings.HasSuffix(fault, "-properties") {
				if e != nil || got.Availability != Unavailable || got.Tables != nil {
					t.Fatal(got, e)
				}
			} else if !errors.Is(e, ErrModel) || got.Tables != nil || got.Availability != "" {
				t.Fatal(got, e)
			}
		})
	}
}

func TestTableLayoutRepeatedConcurrentDetachedObservation(t *testing.T) {
	d, b, i, o := md011Parse(t, md060Example, true)
	before, e := d.TableLayout(context.Background(), b, i, o)
	if e != nil || len(before.Tables) != 1 {
		t.Fatal(before, e)
	}
	root, token := d.root, d.Source().Token()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			got, e := d.TableLayout(context.Background(), b, i, o)
			if e != nil || !reflect.DeepEqual(got, before) {
				t.Error(got, e)
			}
			if len(got.Tables) > 0 {
				got.Tables[0].Rows[0].Cells[0] = TableLayoutCell{}
				got.Tables[0] = TableLayoutTable{}
			}
		})
	}
	wg.Wait()
	if d.root != root || d.Source().Token() != token || d.Source().content != md060Example {
		t.Fatal("original source/tree changed")
	}
}

func TestTableLayoutBudgetAndCancellation(t *testing.T) {
	md060Positive(t)
	d, b, i, o := md011Parse(t, md060Example, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, e := d.TableLayout(ctx, b, i, o); !errors.Is(e, context.Canceled) || got.Tables != nil || got.Availability != "" {
		t.Fatal(got, e)
	}
	if e := b.ChargeWork(b.Limits().WorkUnits - b.work); e != nil {
		t.Fatal(e)
	}
	var stop *LimitExceeded
	for range 2 {
		if got, e := d.TableLayout(context.Background(), b, i, o); !errors.As(e, &stop) || got.Tables != nil || got.Availability != "" {
			t.Fatal(got, e)
		}
	}
}

func TestTableLayoutManyTablesWithinSourceBound(t *testing.T) {
	const count = 1000
	for _, prefix := range [][2]string{{"", ""}, {"> ", "> "}, {"- ", "  "}} {
		t.Run(fmt.Sprintf("%q", prefix[0]), func(t *testing.T) {
			md060Positive(t)
			var text strings.Builder
			for range count {
				text.WriteString(prefix[0] + "| a | b |\n" + prefix[1] + "| - | - |\n\n")
			}
			source := NewSource([]byte(text.String()))
			limits := DefaultLimits()
			limits.WorkUnits = 2048*int64(text.Len()) + 1<<20
			b, e := NewOperationBudget(limits)
			if e != nil {
				t.Fatal(e)
			}
			o := ParseOptions{GFM: true}
			p := ParseWithOptions(context.Background(), source, b, o)
			if p.Status != Complete || p.Document == nil {
				t.Fatal(p.Status, p.Interruption)
			}
			got, e := p.Document.TableLayout(context.Background(), b, NewLineIndex(source), o)
			if e != nil || got.Availability != Available || len(got.Tables) != count {
				t.Fatal(len(got.Tables), got.Availability, e)
			}
			previous := -1
			for _, table := range got.Tables {
				if len(table.Rows) != 2 {
					t.Fatal(table)
				}
				for _, row := range table.Rows {
					if row.Start <= previous || len(row.Cells) != 2 {
						t.Fatal("unordered", row)
					}
					previous = row.End
				}
			}
		})
	}
}
