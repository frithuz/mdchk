//go:build mdchk_conformance

package conformance

import (
	"context"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func actualFor(t *testing.T, source, scope string) Actual {
	t.Helper()
	op, e := parser.NewOperationBudget(parser.DefaultLimits())
	if e != nil {
		t.Fatal(e)
	}
	r, e := parser.ParseConformance(context.Background(), parser.NewSource([]byte(source)), op, scope)
	if e != nil {
		t.Fatal(e)
	}
	return EvaluateActual(context.Background(), r, op)
}
func TestActualSectionIsolation(t *testing.T) {
	source := "- [x] ~~www.a.org~~\n\n| a |\n| - |\n| <title> |\n"
	for _, scope := range []string{"core", "tables-only", "tasks-only", "strike-only", "bare-only", "filter-only", "integrated"} {
		a := actualFor(t, source, scope)
		if a.Status != "rendered" {
			t.Fatalf("%s: %+v", scope, a.Result)
		}
		seen := map[string]bool{}
		for _, f := range a.Catalog {
			seen[f.Kind] = true
			if f.Facts["origin"] == "bare-gfm" {
				seen["bare"] = true
			}
		}
		for kind, enabled := range map[string]string{"table": "tables-only", "task-item": "tasks-only", "strike": "strike-only", "bare": "bare-only", "filtered-tag": "filter-only"} {
			want := scope == enabled || scope == "integrated"
			if seen[kind] != want {
				t.Errorf("%s %s=%v want %v", scope, kind, seen[kind], want)
			}
		}
	}
}
func TestActualRenderingAndAlt(t *testing.T) {
	for _, tc := range []struct{ source, html string }{
		{"![~~a~~](b)\n", "<p><img src=\"b\" alt=\"a\" /></p>\n"},
		{"- [ ] first\n\n  next\n", "<ul>\n<li>\n<p><input disabled=\"\" type=\"checkbox\"> first</p>\n<p>next</p>\n</li>\n</ul>\n"},
		{"<title> &amp; <em>\n", "&lt;title> &amp; <em>\n"},
		{"", ""}, {"[x]: /target\n", ""},
	} {
		a := actualFor(t, tc.source, "integrated")
		if a.Status != "rendered" || a.HTML != tc.html {
			t.Errorf("%q: %+v want %q", tc.source, a.Result, tc.html)
		}
	}
}
func TestActualRefusesOutcomesAndBounds(t *testing.T) {
	op, _ := parser.NewOperationBudget(parser.DefaultLimits())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, _ := parser.ParseConformance(ctx, parser.NewSource([]byte("a")), op, "integrated")
	a := EvaluateActual(ctx, r, op)
	if a.Status == "rendered" || a.HTML != "" || len(a.Catalog) != 0 {
		t.Fatal(a)
	}
	op, _ = parser.NewOperationBudget(parser.DefaultLimits())
	r, _ = parser.ParseConformance(context.Background(), parser.NewSource([]byte("a")), op, "integrated")
	_ = op.ChargeWork(parser.DefaultLimits().WorkUnits)
	a = EvaluateActual(context.Background(), r, op)
	if a.Status != "error" || a.HTML != "" || len(a.Catalog) != 0 {
		t.Fatal(a)
	}
	if _, e := parser.ParseConformance(context.Background(), parser.NewSource(nil), op, "foreign"); e == nil {
		t.Fatal("unknown scope accepted")
	}
	a = actualFor(t, strings.Repeat("a", 16385), "core")
	if a.Status != "error" || len(a.Catalog) != 0 {
		t.Fatal("coordinate bound", a.Result)
	}
	a = actualFor(t, "a\xff", "core")
	if a.Status != "error" {
		t.Fatal("UTF8", a.Result)
	}
}

// A deterministic context interrupts at the last actual-export checkpoint,
// after all tree/coordinate work and the bounded JSON serialization.
type exportContext struct {
	context.Context
	calls, stop int
}

func (c *exportContext) Err() error {
	c.calls++
	if c.stop > 0 && c.calls >= c.stop {
		return context.Canceled
	}
	return nil
}
func TestActualFinalPublicationCancellation(t *testing.T) {
	makeResult := func() (parser.ParseResult, *parser.OperationBudget) {
		op, _ := parser.NewOperationBudget(parser.DefaultLimits())
		r, e := parser.ParseConformance(context.Background(), parser.NewSource([]byte("a")), op, "core")
		if e != nil {
			t.Fatal(e)
		}
		return r, op
	}
	r, op := makeResult()
	baseline := &exportContext{Context: context.Background()}
	if a := EvaluateActual(baseline, r, op); a.Status != "rendered" {
		t.Fatal(a.Result)
	}
	r, op = makeResult()
	ctx := &exportContext{Context: context.Background(), stop: baseline.calls}
	a := EvaluateActual(ctx, r, op)
	if ctx.calls != baseline.calls || a.Status != "error" || a.HTML != "" || len(a.Catalog) != 0 {
		t.Fatal(ctx.calls, baseline.calls, a.Result)
	}
}

func TestActualReferenceAmplificationReservations(t *testing.T) {
	run := func(source string, work int64) (Actual, *parser.OperationBudget) {
		limits := parser.DefaultLimits()
		limits.WorkUnits = work
		op, err := parser.NewOperationBudget(limits)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parser.ParseConformance(context.Background(), parser.NewSource([]byte(source)), op, "core")
		if err != nil || parsed.Status != parser.Complete {
			t.Fatal("parse", err, parsed.Status, parsed.Interruption)
		}
		return EvaluateActual(context.Background(), parsed, op), op
	}
	// A large definition lies outside each four-byte reference use. The renderer
	// is bounded, then the catalog must refuse BEFORE amplifying binding JSON.
	source := "[r]: /" + strings.Repeat("a", 7000) + "\n\n" + strings.Repeat("[r] ", 1500)
	a, op := run(source, 50000000)
	if a.Status != "error" || a.HTML != "" || len(a.Catalog) != 0 {
		t.Fatal("tight shared budget", a.Result)
	}
	// Default budget may refuse during output reservation or catalog expansion;
	// neither creates a new operation or publishes a successful prefix.
	if err := op.ChargeWork(0); err == nil || !strings.Contains(err.Error(), "work-units") {
		t.Fatal("expected sticky work exhaustion", err)
	}
	a, _ = run(source, 1<<35)
	if a.Status != "error" || !strings.Contains(a.Message, "catalog byte reservation") || a.HTML != "" || len(a.Catalog) != 0 {
		t.Fatal("catalog amplification", a.Result)
	}
	titleSource := "[r]: /target \"" + strings.Repeat("a", 7000) + "\"\n\n" + strings.Repeat("[r] ", 1500)
	a, _ = run(titleSource, 1<<35)
	if a.Status != "error" || !strings.Contains(a.Message, "catalog byte reservation") || a.HTML != "" || len(a.Catalog) != 0 {
		t.Fatal("title amplification", a.Result)
	}

	// This admitted source would produce slightly over16MiB of href output.
	// Raised work alone cannot bypass the separate pre-append output limit.
	source = "[r]: /" + strings.Repeat("a", 8187) + "\n\n" + strings.Repeat("[r] ", 2047)
	a, _ = run(source, 1<<35)
	if a.Status != "error" || !strings.Contains(a.Message, "output exceeds") || a.HTML != "" || len(a.Catalog) != 0 {
		t.Fatal("output amplification", a.Result)
	}
}

func TestActualRejectsContradictoryTypedLimit(t *testing.T) {
	op, _ := parser.NewOperationBudget(parser.DefaultLimits())
	r, _ := parser.ParseConformance(context.Background(), parser.NewSource([]byte("a")), op, "core")
	r.Limit = &parser.LimitExceeded{Name: "work-units", Observed: 2, Allowed: 1}
	a := EvaluateActual(context.Background(), r, op)
	if a.Status != "error" || a.Reason != "invalid-outcome" || a.HTML != "" || len(a.Catalog) != 0 {
		t.Fatal(a.Result)
	}
}
