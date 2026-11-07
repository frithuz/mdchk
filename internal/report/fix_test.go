package report

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func TestFixDiffPhysicalHunksAndScaling(t *testing.T) {
	for _, tc := range []struct{ old, next string }{{"", "\n"}, {"a", "a\n"}, {"a\r\nb\rc\n", "\na\r\nb\rc\n"}, {"a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\n", "A\nb\nc\nd\ne\nf\ng\nh\ni\nj\nK\n"}, {"雪\n", ""}, {strings.Repeat("paragraph\n\n", 8000), "\n" + strings.Repeat("paragraph\n\n", 8000)}} {
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		diff, err := BuildFixDiff(context.Background(), budget, parser.NewSource([]byte(tc.old)), parser.NewSource([]byte(tc.next)))
		if err != nil {
			t.Fatal(err)
		}
		if len(diff.Hunks) == 0 {
			t.Fatal("missing changed hunk")
		}
		oldLines := strings.ReplaceAll(strings.ReplaceAll(tc.old, "\r\n", "\n"), "\r", "\n")
		_ = oldLines
		for _, h := range diff.Hunks {
			a, b := 0, 0
			for _, line := range h.Lines {
				if strings.ContainsAny(line.Text, "\r\n") {
					t.Fatal(line)
				}
				if line.Kind != "add" {
					a++
				}
				if line.Kind != "remove" {
					b++
				}
			}
			if a != h.OldLines || b != h.NewLines {
				t.Fatal(h)
			}
		}
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	diff, err := BuildFixDiff(context.Background(), budget, parser.NewSource([]byte("雪\r\n")), parser.NewSource([]byte("雪\r")))
	if err != nil || len(diff.Hunks) != 1 || !reflect.DeepEqual(diff.Hunks[0].Lines, []result.DiffLine{{Kind: "remove", Text: "雪", LineEnding: "crlf"}, {Kind: "add", Text: "雪", LineEnding: "cr"}}) {
		t.Fatal(diff, err)
	}
	budget, _ = parser.NewOperationBudget(parser.Limits{WorkUnits: 1})
	_, err = BuildFixDiff(context.Background(), budget, parser.NewSource([]byte("a")), parser.NewSource([]byte("b")))
	if err == nil {
		t.Fatal("diff did not consume residual meter")
	}
}
