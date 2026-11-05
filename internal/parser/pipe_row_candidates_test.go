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

const md075Example = "A | B\n1 | 2\n"

func md075Positive(t *testing.T) {
	t.Helper()
	d, b, index, options := md011Parse(t, md075Example, true)
	got, err := d.PipeRowCandidates(context.Background(), b, index, options)
	want := []PipeRowCandidate{{Start: 0, End: 5, Line: 1, Cells: 2}, {Start: 6, End: 11, Line: 2, Cells: 2}}
	if err != nil || got.Availability != Available || !reflect.DeepEqual(got.Rows, want) {
		t.Fatal("actual original positive prerequisite", got, err)
	}
}

func TestPipeRowCandidatesRows(t *testing.T) {
	type row = PipeRowCandidate
	for _, tc := range []struct {
		name, source string
		want         []PipeRowCandidate
	}{
		{"spec-example", md075Example, []row{{0, 5, 0, 1, 2, false, false, false, false}, {6, 11, 0, 2, 2, false, false, false, false}}},
		{"outer-pipes", "| A | B |\n| --- |\n| 1 |\n", []row{{0, 9, 0, 1, 2, true, true, false, false}, {10, 17, 0, 2, 1, true, true, true, false}, {18, 23, 0, 3, 1, true, true, false, false}}},
		{"colon-delimiter", "x\n:-: | -\n", []row{{2, 9, 0, 2, 2, false, false, true, false}}},
		{"lone-pipe", "|\n", []row{{0, 1, 0, 1, 1, true, false, false, false}}},
		{"prose-pipe", "Choose a | b here.\nNext line.\n", []row{{0, 18, 0, 1, 2, false, false, false, false}}},
		{"escaped-pipe", "a \\| b\nc \\| d\n", nil},
		{"backslash-parity", "a \\\\| b\n", []row{{0, 7, 0, 1, 2, false, false, false, false}}},
		{"code-span-pipe", "a `|` b\n", []row{{0, 7, 0, 1, 2, false, false, false, false}}},
		{"crlf-and-spaces", "  A | B  \r\n1 | 2\t\r\n", []row{{2, 7, 0, 1, 2, false, false, false, false}, {11, 16, 0, 2, 2, false, false, false, false}}},
		{"non-ascii", "é | ü\n", []row{{0, 7, 0, 1, 2, false, false, false, false}}},
		{"block-quote", "> A | B\n> 1 | 2\n", []row{{2, 7, 0, 1, 2, false, false, false, false}, {10, 15, 0, 2, 2, false, false, false, false}}},
		{"list-item", "- A | B\n  1 | 2\n", []row{{2, 7, 0, 1, 2, false, false, false, false}, {10, 15, 0, 2, 2, false, false, false, false}}},
		{"quote-in-list", "- > A | B\n  > 1 | 2\n", []row{{4, 9, 0, 1, 2, false, false, false, false}, {14, 19, 0, 2, 2, false, false, false, false}}},
		{"two-paragraphs", "a | b\n\nc | d\n", []row{{0, 5, 0, 1, 2, false, false, false, false}, {7, 12, 1, 3, 2, false, false, false, false}}},
		{"paragraph-before-table", "x | y\n| h | i |\n| - | - |\n| 1 |\n", []row{{0, 5, 0, 1, 2, false, false, false, false}, {6, 15, 0, 2, 2, true, true, false, false}, {16, 25, 0, 3, 2, true, true, true, false}, {26, 31, 0, 4, 1, true, true, false, false}}},
		{"valid-table", "| a | b |\n| - | - |\n| 1 |\n| 1 | 2 | 3 |\n", nil},
		{"header-delimiter-mismatch", "| a | b | c |\n| - | - |\n", []row{{0, 13, 0, 1, 3, true, true, false, false}, {14, 23, 0, 2, 2, true, true, true, false}}},
		{"fenced", "```\na | b\nc | d\n```\n", nil},
		{"indented", "    a | b\n    c | d\n", nil},
		{"html-block", "<div>\na | b\nc | d\n</div>\n", nil},
		{"setext", "a | b\n---\n", nil},
		{"front-matter", "---\na | b\nc | d\n---\n", nil},
		{"reference-definition", "[x]: /u 'a | b'\nc | d\n", []row{{16, 21, 0, 2, 2, false, false, false, false}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, b, index, o := md011Parse(t, tc.source, true)
			got, e := d.PipeRowCandidates(context.Background(), b, index, o)
			if e != nil || got.Availability != Available || !reflect.DeepEqual(got.Rows, tc.want) {
				t.Fatal(got.Rows, got.Availability, e)
			}
		})
	}
}

// The classifier agrees with the table splitter on every row it promoted.
func TestPipeRowCandidatesSharedClassifier(t *testing.T) {
	for _, source := range []string{"| a | b |\n| :-: | - |\n| 1 |\n| 1 | 2 | 3 |\n", "a \\| b | `c|d` |\n--- | --- | -\n\\\\| x\n", "|\n|-|\n"} {
		d, _, index, _ := md011Parse(t, source, true)
		for _, n := range md070Nodes(d, TableRow) {
			p := n.spec.Properties.Row
			start, end := n.spec.Envelope.start, n.spec.Envelope.end
			for end > start && (index.source.content[end-1] == '\n' || index.source.content[end-1] == '\r') {
				end--
			}
			shape, ok := classifyPipeRow(index.source.content, start, end, true, func(int) bool { return true }, nil)
			if !ok || shape.cells != p.RawCells || shape.separators != len(p.Separators) || shape.leading != p.LeadingPipe.Present() || shape.trailing != p.TrailingPipe.Present() || shape.delimiter != (p.Role == "delimiter") {
				t.Fatal(source, p.Role, shape, p.RawCells)
			}
		}
	}
}

func TestPipeRowCandidatesNextToTable(t *testing.T) {
	const source = "| a |\n| - |\n\nx | y\n"
	d, b, i, o := md011Parse(t, source, true)
	got, e := d.PipeRowCandidates(context.Background(), b, i, o)
	if e != nil || len(got.Rows) != 1 || got.Rows[0].NextToTable {
		t.Fatal("blank line separates", got, e)
	}
	// The parser never places a Paragraph directly beside a Table; widen the
	// table envelope over the blank line to exercise the sibling check.
	md070Nodes(d, Table)[0].spec.Envelope.end = 13
	got, e = d.PipeRowCandidates(context.Background(), b, i, o)
	if e != nil || got.Availability != Available || len(got.Rows) != 1 || !got.Rows[0].NextToTable {
		t.Fatal(got, e)
	}
}

func TestPipeRowCandidatesOriginalAuthority(t *testing.T) {
	for _, fault := range []string{"original", "detached", "wrong-source", "wrong-budget", "wrong-options", "incomplete", "nil-document", "nil-context"} {
		t.Run(fault, func(t *testing.T) {
			md075Positive(t)
			d, b, i, o := md011Parse(t, md075Example, true)
			ctx := context.Background()
			switch fault {
			case "detached":
				d, _ = NewDocument(d.Root(), d.References())
			case "wrong-source":
				i = NewLineIndex(NewSource([]byte(md075Example)))
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
			got, e := d.PipeRowCandidates(ctx, b, i, o)
			if fault == "original" {
				if e != nil || len(got.Rows) != 2 {
					t.Fatal(got, e)
				}
			} else if !errors.Is(e, ErrModel) || got.Rows != nil {
				t.Fatal(got, e)
			}
		})
	}
}

func TestPipeRowCandidatesUnknownAndMalformed(t *testing.T) {
	for _, fault := range []string{"paragraph-envelope", "paragraph-foreign", "content-missing", "content-foreign", "content-order", "content-outside", "container-envelope"} {
		t.Run(fault, func(t *testing.T) {
			md075Positive(t)
			d, b, i, o := md011Parse(t, md075Example, true)
			p := md070Nodes(d, Paragraph)[0]
			switch fault {
			case "paragraph-envelope":
				p.spec.Envelope.end = i.source.Len() + 1
			case "paragraph-foreign":
				p.spec.Envelope.source = NewSource(i.source.Bytes())
			case "content-missing":
				p.spec.Content = nil
			case "content-foreign":
				p.spec.Content[1].Source.source = NewSource(i.source.Bytes())
			case "content-order":
				p.spec.Content[0], p.spec.Content[1] = p.spec.Content[1], p.spec.Content[0]
			case "content-outside":
				p.spec.Content[1].Source.end = p.spec.Envelope.end + 1
			case "container-envelope":
				d.root.spec.Envelope.end = i.source.Len() + 1
			}
			got, e := d.PipeRowCandidates(context.Background(), b, i, o)
			if fault != "container-envelope" {
				if e != nil || got.Availability != Unavailable || got.Rows != nil {
					t.Fatal(got, e)
				}
			} else if !errors.Is(e, ErrModel) || got.Rows != nil || got.Availability != "" {
				t.Fatal(got, e)
			}
		})
	}
}

func TestPipeRowCandidatesRepeatedConcurrentDetachedObservation(t *testing.T) {
	d, b, i, o := md011Parse(t, md075Example, true)
	before, e := d.PipeRowCandidates(context.Background(), b, i, o)
	if e != nil || len(before.Rows) != 2 {
		t.Fatal(before, e)
	}
	root, token := d.root, d.Source().Token()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			got, e := d.PipeRowCandidates(context.Background(), b, i, o)
			if e != nil || !reflect.DeepEqual(got, before) {
				t.Error(got, e)
			}
			if len(got.Rows) > 0 {
				got.Rows[0] = PipeRowCandidate{}
			}
		})
	}
	wg.Wait()
	if d.root != root || d.Source().Token() != token || d.Source().content != md075Example {
		t.Fatal("original source/tree changed")
	}
}

func TestPipeRowCandidatesBudgetAndCancellation(t *testing.T) {
	md075Positive(t)
	d, b, i, o := md011Parse(t, md075Example, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, e := d.PipeRowCandidates(ctx, b, i, o); !errors.Is(e, context.Canceled) || got.Rows != nil || got.Availability != "" {
		t.Fatal(got, e)
	}
	if e := b.ChargeWork(b.Limits().WorkUnits - b.work); e != nil {
		t.Fatal(e)
	}
	var stop *LimitExceeded
	for range 2 {
		if got, e := d.PipeRowCandidates(context.Background(), b, i, o); !errors.As(e, &stop) || got.Rows != nil || got.Availability != "" {
			t.Fatal(got, e)
		}
	}
}

func TestPipeRowCandidatesManyRowsWithinSourceBound(t *testing.T) {
	const count = 1000
	for _, prefix := range [][2]string{{"", ""}, {"> ", "> "}, {"- ", "  "}} {
		t.Run(fmt.Sprintf("%q", prefix[0]), func(t *testing.T) {
			md075Positive(t)
			var text strings.Builder
			for range count {
				text.WriteString(prefix[0] + "a | b\n" + prefix[1] + "1 | 2\n\n")
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
			got, e := p.Document.PipeRowCandidates(context.Background(), b, NewLineIndex(source), o)
			if e != nil || got.Availability != Available || len(got.Rows) != 2*count {
				t.Fatal(len(got.Rows), got.Availability, e)
			}
			for j, row := range got.Rows {
				if row.Cells != 2 || row.Paragraph != j/2 || j > 0 && row.Start <= got.Rows[j-1].End {
					t.Fatal("unordered or miscounted", j, row)
				}
			}
		})
	}
}
