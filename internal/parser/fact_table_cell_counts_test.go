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

const md056Example = "A | B\n--- | ---\n1 | 2 | 3\n"

func md056Positive(t *testing.T) {
	t.Helper()
	d, b, index, options := md011Parse(t, md056Example, true)
	got, err := d.TableCellCounts(context.Background(), b, index, options)
	want := []TableCellCountRow{{Start: 16, End: 25, RawCells: 3, Width: 2}}
	if err != nil || got.Availability != Available || !reflect.DeepEqual(got.Rows, want) {
		t.Fatal("actual original positive prerequisite", got, err)
	}
}

func TestTableCellCountsRows(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         []TableCellCountRow
	}{
		{"spec-example", md056Example, []TableCellCountRow{{16, 25, 3, 2}}},
		{"short-and-matching", "| a | b | c |\n| - | - | - |\n| 1 |\n| 1 | 2 | 3 |\n", []TableCellCountRow{{28, 33, 1, 3}, {34, 47, 3, 3}}},
		{"crlf-and-spaces", "| a |  \r\n|---|\t\r\n  b | c \r\n", []TableCellCountRow{{19, 24, 2, 1}}},
		{"escaped-pipe", "| a | b |\n| - | - |\n| `x\\|y` | z |\n", []TableCellCountRow{{20, 34, 2, 2}}},
		{"code-span-pipe", "| a | b |\n| - | - |\n| `x|y` | z |\n", []TableCellCountRow{{20, 33, 3, 2}}},
		{"header-delimiter-mismatch", "| a | b | c |\n| - | - |\n| 1 | 2 |\n", nil},
		{"block-quote", "> a | b\n> --- | ---\n> 1\n", []TableCellCountRow{{22, 23, 1, 2}}},
		{"list-item", "- | a |\n  | - |\n  | 1 | 2 |\n", []TableCellCountRow{{18, 27, 2, 1}}},
		{"two-tables", "a | b\n--- | ---\n1\n\n| c |\n| - |\n| 2 |\n", []TableCellCountRow{{16, 17, 1, 2}, {31, 36, 1, 1}}},
		{"header-only", "a | b\n--- | ---\n", nil},
		{"fenced", "```\na | b\n--- | ---\n1 | 2 | 3\n```\n", nil},
		{"html-block", "<div>\na | b\n--- | ---\n1 | 2 | 3\n</div>\n", nil},
		{"no-delimiter", "| a |\n| b | c |\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, b, index, o := md011Parse(t, tc.source, true)
			got, e := d.TableCellCounts(context.Background(), b, index, o)
			if e != nil || got.Availability != Available || !reflect.DeepEqual(got.Rows, tc.want) {
				t.Fatal(got.Rows, got.Availability, e)
			}
		})
	}
	d, b, index, o := md011Parse(t, md056Example, false)
	if got, e := d.TableCellCounts(context.Background(), b, index, o); e != nil || got.Availability != Available || got.Rows != nil {
		t.Fatal("CommonMark has no tables", got, e)
	}
}

func TestTableCellCountsOriginalAuthority(t *testing.T) {
	for _, fault := range []string{"original", "detached", "wrong-source", "wrong-budget", "wrong-options", "incomplete", "nil-document", "nil-context"} {
		t.Run(fault, func(t *testing.T) {
			md056Positive(t)
			d, b, i, o := md011Parse(t, md056Example, true)
			ctx := context.Background()
			switch fault {
			case "detached":
				d, _ = NewDocument(d.Root(), d.References())
			case "wrong-source":
				i = NewLineIndex(NewSource([]byte(md056Example)))
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
			got, e := d.TableCellCounts(ctx, b, i, o)
			if fault == "original" {
				if e != nil || len(got.Rows) != 1 {
					t.Fatal(got, e)
				}
			} else if !errors.Is(e, ErrModel) || got.Rows != nil {
				t.Fatal(got, e)
			}
		})
	}
}

func TestTableCellCountsUnknownAndMalformed(t *testing.T) {
	for _, fault := range []string{"table-properties", "header-properties", "row-properties", "envelope", "foreign"} {
		t.Run(fault, func(t *testing.T) {
			md056Positive(t)
			d, b, i, o := md011Parse(t, md056Example, true)
			rows := md070Nodes(d, TableRow)
			switch fault {
			case "table-properties":
				md070Nodes(d, Table)[0].spec.Properties.Table = nil
			case "header-properties":
				rows[0].spec.Properties.Row = nil
			case "row-properties":
				rows[len(rows)-1].spec.Properties.Row = nil
			case "envelope":
				rows[len(rows)-1].spec.Envelope.end = i.source.Len() + 1
			case "foreign":
				rows[len(rows)-1].spec.Envelope.source = NewSource(i.source.Bytes())
			}
			got, e := d.TableCellCounts(context.Background(), b, i, o)
			if strings.HasSuffix(fault, "-properties") {
				if e != nil || got.Availability != Unavailable || got.Rows != nil {
					t.Fatal(got, e)
				}
			} else if !errors.Is(e, ErrModel) || got.Rows != nil || got.Availability != "" {
				t.Fatal(got, e)
			}
		})
	}
}

func TestTableCellCountsRepeatedConcurrentDetachedObservation(t *testing.T) {
	d, b, i, o := md011Parse(t, md056Example, true)
	before, e := d.TableCellCounts(context.Background(), b, i, o)
	if e != nil || len(before.Rows) != 1 {
		t.Fatal(before, e)
	}
	root, token := d.root, d.Source().Token()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			got, e := d.TableCellCounts(context.Background(), b, i, o)
			if e != nil || !reflect.DeepEqual(got, before) {
				t.Error(got, e)
			}
			if len(got.Rows) > 0 {
				got.Rows[0] = TableCellCountRow{}
			}
		})
	}
	wg.Wait()
	if d.root != root || d.Source().Token() != token || d.Source().content != md056Example {
		t.Fatal("original source/tree changed")
	}
}

func TestTableCellCountsBudgetAndCancellation(t *testing.T) {
	md056Positive(t)
	d, b, i, o := md011Parse(t, md056Example, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, e := d.TableCellCounts(ctx, b, i, o); !errors.Is(e, context.Canceled) || got.Rows != nil || got.Availability != "" {
		t.Fatal(got, e)
	}
	if e := b.ChargeWork(b.Limits().WorkUnits - b.work); e != nil {
		t.Fatal(e)
	}
	var stop *LimitExceeded
	for range 2 {
		if got, e := d.TableCellCounts(context.Background(), b, i, o); !errors.As(e, &stop) || got.Rows != nil || got.Availability != "" {
			t.Fatal(got, e)
		}
	}
}

func TestTableCellCountsManyRowsWithinSourceBound(t *testing.T) {
	const count = 1000
	for _, prefix := range [][2]string{{"", ""}, {"> ", "> "}, {"- ", "  "}} {
		t.Run(fmt.Sprintf("%q", prefix[0]), func(t *testing.T) {
			md056Positive(t)
			var text strings.Builder
			for range count {
				text.WriteString(prefix[0] + "| a | b |\n" + prefix[1] + "| - | - |\n" + prefix[1] + "| 1 |\n\n")
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
			got, e := p.Document.TableCellCounts(context.Background(), b, NewLineIndex(source), o)
			if e != nil || got.Availability != Available || len(got.Rows) != count {
				t.Fatal(len(got.Rows), got.Availability, e)
			}
			for j, row := range got.Rows {
				if row.RawCells != 1 || row.Width != 2 || j > 0 && row.Start <= got.Rows[j-1].End {
					t.Fatal("unordered or miscounted", j, row)
				}
			}
		})
	}
}
