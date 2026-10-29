package rules

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
	"testing"
)

func TestMD089OriginFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 1}, {"clean", "clean\n", 1}, {"protected", "```\nx\n```", 1}, {"growth", "中A中A中A", 15}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+"/"+reason.Error(), func(t *testing.T) {
				origin := &md018LateContext{Context: context.Background(), stop: 1 << 20, reason: reason}
				var observed engine.RuleOutcome
				registry := registry(t, []engine.Registration{{ID: "MD089", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, e := c.CJKSpacing(ctx)
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
					origin.calls = 0
					origin.stop = tc.final
					observed = compareCJKSpacing(context.Background(), c, items)
					if !errors.Is(observed.Err, reason) || len(observed.Findings) != 0 {
						t.Fatalf("direct origin final Err oracle: Err=%v findings=%d", observed.Err, len(observed.Findings))
					}
					if origin.calls != tc.final {
						t.Fatal("post-oracle final count", origin.calls, tc.final)
					}
					return observed
				}}})
				input, e := engine.NewInput("doc.md", []byte(tc.source), md089Settings(), engine.DocumentContext{})
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
