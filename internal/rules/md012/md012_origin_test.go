package md012_test

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/rules/md012"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
	"testing"
)

func TestMD012OriginFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 1}, {"clean", "clean\n", 1}, {"growth", "a\n\n\na\n\n\na\n\n\na\n\n\na\n\n\n", 15}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+"/"+reason.Error(), func(t *testing.T) {
				origin := &ruletest.LateStopContext{Context: context.Background(), Stop: 1 << 20, Reason: reason}
				var observed engine.RuleOutcome
				registry := registry(t, []engine.Registration{{ID: "MD012", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, e := c.BlankBodyLines(ctx)
					if e != nil {
						t.Fatal(e)
					}
					want := 0
					if tc.name == "growth" {
						want = 5
					}
					if len(items) != want {
						t.Fatal("actual original blank-run inventory", len(items), want)
					}
					origin.Calls = 0
					origin.Stop = tc.final
					observed = md012.CompareBlankBodyLines(context.Background(), c, items)
					if !errors.Is(observed.Err, reason) || len(observed.Findings) != 0 {
						t.Fatalf("direct origin final Err oracle: Err=%v findings=%d", observed.Err, len(observed.Findings))
					}
					if origin.Calls != tc.final {
						t.Fatal("post-oracle final count", origin.Calls, tc.final)
					}
					return observed
				}}})
				input, e := engine.NewInput("doc.md", []byte(tc.source), md012Settings(), engine.DocumentContext{})
				if e != nil {
					t.Fatal(e)
				}
				outcome, e := registry.Lint(origin, []engine.Input{input}, engine.RunOptions{})
				if e != nil || !errors.Is(observed.Err, reason) || outcome.Complete || len(outcome.Diagnostics) != 0 || len(outcome.Interruptions) != 1 {
					t.Fatal("atomic engine withholding", observed, outcome, e)
				}
			})
		}
	}
}
