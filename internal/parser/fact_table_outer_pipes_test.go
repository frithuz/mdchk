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

const md055Example = "A | B\n--- | ---\n| 1 | 2 |\n"

type md055Row struct {
	role                       string
	start, end                 int
	leadingStart, leadingEnd   int // -1 when absent
	trailingStart, trailingEnd int
}

func md055Rows(t *testing.T, got TableOuterPipesInventory, index LineIndex) []md055Row {
	t.Helper()
	var out []md055Row
	for _, row := range got.Rows {
		r := md055Row{row.Role, row.Start, row.End, -1, -1, -1, -1}
		if row.LeadingPipe.Present() {
			r.leadingStart, r.leadingEnd = row.LeadingPipe.start, row.LeadingPipe.end
		}
		if row.TrailingPipe.Present() {
			r.trailingStart, r.trailingEnd = row.TrailingPipe.start, row.TrailingPipe.end
		}
		for _, pipe := range []Span{row.LeadingPipe, row.TrailingPipe} {
			if pipe.Present() && pipe.SourceToken() != index.source.Token() {
				t.Fatal("foreign span", row)
			}
		}
		out = append(out, r)
	}
	return out
}

func md055Positive(t *testing.T) {
	t.Helper()
	d, b, index, options := md011Parse(t, md055Example, true)
	got, err := d.TableOuterPipes(context.Background(), b, index, options)
	want := []md055Row{{"header", 0, 5, -1, -1, -1, -1}, {"delimiter", 6, 15, -1, -1, -1, -1}, {"body", 16, 25, 16, 17, 24, 25}}
	if err != nil || got.Availability != Available || !reflect.DeepEqual(md055Rows(t, got, index), want) {
		t.Fatal("actual original positive prerequisite", got, err)
	}
}

func TestTableOuterPipesRows(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         []md055Row
	}{
		{"spec-example", md055Example, []md055Row{{"header", 0, 5, -1, -1, -1, -1}, {"delimiter", 6, 15, -1, -1, -1, -1}, {"body", 16, 25, 16, 17, 24, 25}}},
		{"crlf-and-spaces", "| a |  \r\n|---|\t\r\n b \r\n", []md055Row{{"header", 0, 5, 0, 1, 4, 5}, {"delimiter", 9, 14, 9, 10, 13, 14}, {"body", 18, 19, -1, -1, -1, -1}}},
		{"escaped-edge", "| a |\n| - |\n\\| b \\|\n", []md055Row{{"header", 0, 5, 0, 1, 4, 5}, {"delimiter", 6, 11, 6, 7, 10, 11}, {"body", 12, 19, -1, -1, -1, -1}}},
		{"code-span-pipe", "| a |\n| - |\n`|` |\n", []md055Row{{"header", 0, 5, 0, 1, 4, 5}, {"delimiter", 6, 11, 6, 7, 10, 11}, {"body", 12, 17, -1, -1, 16, 17}}},
		{"block-quote", "> a | b\n> --- | ---\n", []md055Row{{"header", 2, 7, -1, -1, -1, -1}, {"delimiter", 10, 19, -1, -1, -1, -1}}},
		{"list-item", "- | a |\n  | - |\n", []md055Row{{"header", 2, 7, 2, 3, 6, 7}, {"delimiter", 10, 15, 10, 11, 14, 15}}},
		{"two-tables", "a | b\n--- | ---\n\n| c |\n| - |\n", []md055Row{{"header", 0, 5, -1, -1, -1, -1}, {"delimiter", 6, 15, -1, -1, -1, -1}, {"header", 17, 22, 17, 18, 21, 22}, {"delimiter", 23, 28, 23, 24, 27, 28}}},
		{"fenced", "```\na | b\n--- | ---\n```\n", nil},
		{"html-block", "<div>\na | b\n--- | ---\n</div>\n", nil},
		{"no-delimiter", "| a |\n| b |\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, b, index, o := md011Parse(t, tc.source, true)
			got, e := d.TableOuterPipes(context.Background(), b, index, o)
			if e != nil || got.Availability != Available || !reflect.DeepEqual(md055Rows(t, got, index), tc.want) {
				t.Fatal(md055Rows(t, got, index), got.Availability, e)
			}
		})
	}
	d, b, index, o := md011Parse(t, md055Example, false)
	if got, e := d.TableOuterPipes(context.Background(), b, index, o); e != nil || got.Availability != Available || got.Rows != nil {
		t.Fatal("CommonMark has no tables", got, e)
	}
}

func TestTableOuterPipesOriginalAuthority(t *testing.T) {
	for _, fault := range []string{"original", "detached", "wrong-source", "wrong-budget", "wrong-options", "incomplete", "nil-document", "nil-context"} {
		t.Run(fault, func(t *testing.T) {
			md055Positive(t)
			d, b, i, o := md011Parse(t, md055Example, true)
			ctx := context.Background()
			switch fault {
			case "detached":
				d, _ = NewDocument(d.Root(), d.References())
			case "wrong-source":
				i = NewLineIndex(NewSource([]byte(md055Example)))
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
			got, e := d.TableOuterPipes(ctx, b, i, o)
			if fault == "original" {
				if e != nil || len(got.Rows) != 3 {
					t.Fatal(got, e)
				}
			} else if !errors.Is(e, ErrModel) || got.Rows != nil {
				t.Fatal(got, e)
			}
		})
	}
}

func TestTableOuterPipesUnknownAndMalformed(t *testing.T) {
	for _, fault := range []string{"row-properties", "envelope", "foreign"} {
		t.Run(fault, func(t *testing.T) {
			md055Positive(t)
			d, b, i, o := md011Parse(t, md055Example, true)
			rows := md070Nodes(d, TableRow)
			switch fault {
			case "row-properties":
				rows[len(rows)-1].spec.Properties.Row = nil
			case "envelope":
				rows[len(rows)-1].spec.Envelope.end = i.source.Len() + 1
			case "foreign":
				rows[len(rows)-1].spec.Envelope.source = NewSource(i.source.Bytes())
			}
			got, e := d.TableOuterPipes(context.Background(), b, i, o)
			if fault == "row-properties" {
				if e != nil || got.Availability != Unavailable || got.Rows != nil {
					t.Fatal(got, e)
				}
			} else if !errors.Is(e, ErrModel) || got.Rows != nil || got.Availability != "" {
				t.Fatal(got, e)
			}
		})
	}
}

func TestTableOuterPipesRepeatedConcurrentDetachedObservation(t *testing.T) {
	d, b, i, o := md011Parse(t, md055Example, true)
	before, e := d.TableOuterPipes(context.Background(), b, i, o)
	if e != nil || len(before.Rows) != 3 {
		t.Fatal(before, e)
	}
	root, token := d.root, d.Source().Token()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			got, e := d.TableOuterPipes(context.Background(), b, i, o)
			if e != nil || !reflect.DeepEqual(got, before) {
				t.Error(got, e)
			}
			if len(got.Rows) > 0 {
				got.Rows[0] = TableOuterPipeRow{}
			}
		})
	}
	wg.Wait()
	if d.root != root || d.Source().Token() != token || d.Source().content != md055Example {
		t.Fatal("original source/tree changed")
	}
}

func TestTableOuterPipesBudgetAndCancellation(t *testing.T) {
	md055Positive(t)
	d, b, i, o := md011Parse(t, md055Example, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, e := d.TableOuterPipes(ctx, b, i, o); !errors.Is(e, context.Canceled) || got.Rows != nil || got.Availability != "" {
		t.Fatal(got, e)
	}
	if e := b.ChargeWork(b.Limits().WorkUnits - b.work); e != nil {
		t.Fatal(e)
	}
	var stop *LimitExceeded
	for range 2 {
		if got, e := d.TableOuterPipes(context.Background(), b, i, o); !errors.As(e, &stop) || got.Rows != nil || got.Availability != "" {
			t.Fatal(got, e)
		}
	}
}

func TestTableOuterPipesManyRowsWithinSourceBound(t *testing.T) {
	const count = 1000
	for _, prefix := range [][2]string{{"", ""}, {"> ", "> "}, {"- ", "  "}} {
		t.Run(fmt.Sprintf("%q", prefix[0]), func(t *testing.T) {
			md055Positive(t)
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
			got, e := p.Document.TableOuterPipes(context.Background(), b, NewLineIndex(source), o)
			if e != nil || got.Availability != Available || len(got.Rows) != 2*count {
				t.Fatal(len(got.Rows), got.Availability, e)
			}
			for j := 1; j < len(got.Rows); j++ {
				if got.Rows[j].Start <= got.Rows[j-1].End {
					t.Fatal("unordered", j, got.Rows[j-1], got.Rows[j])
				}
			}
		})
	}
}
