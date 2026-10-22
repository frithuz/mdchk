package fix_test

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"testing"
)

func TestInsertionEndingNearestCompleteOriginal(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		at         int
		want       string
		invalid    bool
	}{
		{"no ending", "abc", 1, "\n", false}, {"empty", "", 0, "\n", false},
		{"BOF", "abc\r\ndef\n", 0, "\r\n", false},
		{"preceding tie", "a\r\nxy\nz", 4, "\r\n", false},
		{"closer following", "a\r\nxyz\rz", 5, "\r", false},
		{"EOF mixed", "a\r\nb\nc", 6, "\n", false},
		{"EOF CR", "a\rb", 3, "\r", false},
		{"CRLF start", "a\r\nb", 1, "\r\n", false},
		{"CRLF interior", "a\r\nb", 2, "", true},
		{"scalar interior", "雪", 1, "", true}, {"outside", "abc", 4, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := parser.NewSource([]byte(tc.text))
			index := parser.NewLineIndex(s)
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			check := func(ctx context.Context, n int64) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				return budget.ChargeWork(n)
			}
			got, err := fix.InsertionEnding(context.Background(), s, index, tc.at, check)
			if tc.invalid {
				if !errors.Is(err, parser.ErrPosition) {
					t.Fatal(err)
				}
				return
			}
			if err != nil || string(got) != tc.want {
				t.Fatalf("got %q/%v want %q", got, err, tc.want)
			}
		})
	}
}
func TestInsertionEndingSharedResidualAndCancellation(t *testing.T) {
	s := parser.NewSource([]byte("a\r\nb\nlast"))
	index := parser.NewLineIndex(s)
	budget, _ := parser.NewOperationBudget(parser.Limits{WorkUnits: 1})
	check := func(_ context.Context, n int64) error { return budget.ChargeWork(n) }
	if _, err := fix.InsertionEnding(context.Background(), s, index, s.Len(), check); err == nil {
		t.Fatal("reset residual budget")
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	check = func(ctx context.Context, _ int64) error {
		calls++
		if calls == 2 {
			cancel()
		}
		return ctx.Err()
	}
	if _, err := fix.InsertionEnding(ctx, s, index, s.Len(), check); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	foreign := parser.NewSource([]byte("a\r\nb\nlast"))
	if _, err := fix.InsertionEnding(context.Background(), foreign, index, 0, func(context.Context, int64) error { return nil }); !errors.Is(err, parser.ErrRange) {
		t.Fatal(err)
	}
}
