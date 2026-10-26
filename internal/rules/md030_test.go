package rules

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func md030Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD030"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{}}
	return s
}
func TestMD030AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md030.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor  string
		Recognize, Incomplete bool
		Spaces                int
		Expected              []struct {
			Range  []int
			Source string
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	r := registry(t, Registrations())
	for _, f := range fixtures {
		for _, flavor := range []string{"commonmark", "gfm"} {
			if f.Flavor != "" && f.Flavor != flavor {
				continue
			}
			t.Run(f.Name+"/"+flavor, func(t *testing.T) {
				fm, _ := parser.NewFrontmatterOptions(f.Recognize, nil)
				dc := engine.DocumentContext{Flavor: flavor, Frontmatter: fm}
				setting := md030Settings()
				spaces := f.Spaces
				if spaces == 0 {
					spaces = 1
				}
				setting["MD030"] = engine.RuleSetting{Options: engine.Options{"spaces": spaces}}
				out := analyzeHeadings(t, r, f.Source, setting, dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, setting, dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) || out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(f.Expected) {
					t.Fatal("outcome", out.Complete, out.Diagnostics, out.Errors, out.Interruptions, "expected", f.Expected)
				}
				snapshot := out.Files[0].InputSnapshot
				if snapshot == nil || snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source))) || snapshot.SizeBytes != len(f.Source) {
					t.Fatal(out.Files)
				}
				uncertain := false
				for _, coverage := range out.Coverage {
					uncertain = uncertain || coverage.Reason == result.ParserUncertain
				}
				if uncertain != f.Incomplete || (!f.Incomplete && len(out.Coverage) != 0) {
					t.Fatal(out.Coverage)
				}
				for i, w := range f.Expected {
					d := out.Diagnostics[i]
					got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if !reflect.DeepEqual(got, w.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source || d.Rule != "MD030" || d.FixAvailable || d.Message != "List marker separators must use the configured structural spacing." {
						t.Fatal(got, d, w)
					}
				}
			})
		}
	}
}

func TestMD030CatalogOptionsAndDefaultActivation(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD030") || r.Available("MD009") || len(r.Metadata()) != 87 {
		t.Fatal("availability")
	}
	enabledCount := 0
	for _, m := range r.Metadata() {
		if m.DefaultEnabled {
			enabledCount++
		}
		if m.ID == "MD030" && (!m.DefaultEnabled || m.Fix != engine.PlannedFix || !m.RequiresSyntax || m.UsesFrontmatter || len(m.Options) != 1) {
			t.Fatal(m)
		}
	}
	if enabledCount != 59 {
		t.Fatal(enabledCount)
	}
	for _, mapping := range []map[string]any{{"unknown": true}, {"enabled": false, "unknown": true}, {"enabled": nil}, {"enabled": "true"}, {"allow_directives": nil}, {"allow_directives": 1}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD030": setting}, engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal("accepted", mapping)
		}
	}
	for _, mapping := range []map[string]any{{}, {"enabled": false}, {"allow_directives": true}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.ResolveSettings(engine.Settings{"MD030": setting}, engine.ValidationProviders{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		s := md030Settings()
		delete(s, "MD030")
		out := analyzeHeadings(t, r, "-  One", s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
		if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD030" || out.Diagnostics[0].FixAvailable {
			t.Fatal(out)
		}
	}
}

func TestMD030FinalPublicationWithholdsAllFindings(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 1}, {"aligned", "- one\n- two", 3}, {"violation", "-  one\n-  two", 7}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+reason.Error(), func(t *testing.T) {
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD030", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, err := c.ListSeparators(ctx)
					if err != nil {
						t.Fatal(err)
					}
					// Fixed independent counts: one check per item, optional
					// finding growth/emission, then the final publication check.
					late := &md018LateContext{Context: ctx, reason: reason, stop: tc.final}
					observed = compareListMarkerSpacing(late, c, items, 1)
					if late.calls != tc.final {
						t.Fatal(late.calls, tc.final)
					}
					return observed
				}}})
				out := analyzeHeadings(t, r, tc.source, md030Settings(), engine.DocumentContext{}, engine.RunOptions{})
				if !errors.Is(observed.Err, reason) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
					t.Fatal(observed, out)
				}
			})
		}
	}
}

func TestMD030StateAndFindingReservationsUseOriginalAllowance(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		remaining    int64
		kind         string
	}{{"comparison work", "- one\n- two", 0, "work"}, {"finding storage", "-  a\n-  b\n-  c", 3, "nodes"}, {"finding emission", "-  a\n-  b\n-  c", 6, "nodes"}, {"finding growth work", "-  a\n-  b\n-  c", 6, "work"}} {
		t.Run(tc.name, func(t *testing.T) {
			// Calibrate only the unchanged parser/inventory cost in a separate operation.
			// Exhaustion is sticky, so each actual comparison gets a fresh original parse.
			var used int64
			execute := func(calibrate bool) (result.Result, engine.RuleOutcome) {
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD030", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, err := c.ListSeparators(ctx)
					if err != nil {
						t.Fatal("inventory", err)
					}
					if len(items) != 2 && len(items) != 3 {
						t.Fatal("actual inventory", items)
					}
					for _, item := range items {
						if !item.FirstLineNonempty {
							t.Fatal("expected source payload", item)
						}
					}
					limit := budget.Limits().TotalNodes
					charge := budget.ChargeNodes
					if tc.kind == "work" {
						limit = budget.Limits().WorkUnits
						charge = budget.ChargeWork
					}
					if calibrate {
						var exhausted *parser.LimitExceeded
						if !errors.As(charge(limit), &exhausted) {
							t.Fatal("calibration did not exhaust")
						}
						used = exhausted.Observed - limit
						return engine.RuleOutcome{Err: exhausted}
					}
					if err := charge(limit - used - tc.remaining); err != nil {
						t.Fatal("drain", err)
					}
					observed = compareListMarkerSpacing(ctx, c, items, 1)
					return observed
				}}})
				return analyzeHeadings(t, r, tc.source, md030Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget}), observed
			}
			execute(true)
			out, observed := execute(false)
			var exhausted *parser.LimitExceeded
			if !errors.As(observed.Err, &exhausted) || out.Complete || len(out.Diagnostics) != 0 || len(observed.Findings) != 0 || len(out.Interruptions) != 1 {
				t.Fatal("comparison must exhaust directly before publication", observed, out)
			}
			expected := "total-nodes"
			if tc.kind == "work" {
				expected = "work-units"
			}
			if exhausted.Name != expected {
				t.Fatal(exhausted)
			}
		})
	}
}

func TestMD030ElapsedAndCallerControlsAfterInventory(t *testing.T) {
	for _, kind := range []string{"elapsed", "caller"} {
		limits := parser.DefaultLimits()
		if kind == "elapsed" {
			limits.OperationTime = 200 * time.Millisecond
		}
		budget, _ := parser.NewOperationBudget(limits)
		var observed engine.RuleOutcome
		r := registry(t, []engine.Registration{{ID: "MD030", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			items, err := c.ListSeparators(ctx)
			if err != nil {
				t.Fatal("inventory", err)
			}
			if kind == "elapsed" {
				time.Sleep(250 * time.Millisecond)
			} else {
				ctx = &md018LateContext{Context: ctx, stop: 1, reason: context.Canceled}
			}
			observed = compareListMarkerSpacing(ctx, c, items, 1)
			return observed
		}}})
		out := analyzeHeadings(t, r, "1. a\n3. b", md030Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
		if observed.Err == nil || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
			t.Fatal(observed, out)
		}
		if kind == "elapsed" {
			var stop *parser.LimitExceeded
			if !errors.As(observed.Err, &stop) || stop.Name != "operation-time" {
				t.Fatal(observed.Err)
			}
		} else if !errors.Is(observed.Err, context.Canceled) {
			t.Fatal(observed.Err)
		}
	}
}

func TestMD030UTF8GatePrecedesRule(t *testing.T) {
	in, err := engine.NewInput("invalid.md", []byte{'-', ' ', 0xff, '\n', ' ', '-', ' ', 'x'}, md030Settings(), engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := registry(t, Registrations()).Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil || out.Complete || len(out.Errors) != 1 || len(out.Diagnostics) != 0 {
		t.Fatal(out, err)
	}
}

func TestMD030OriginControlsAndManualFindings(t *testing.T) {
	for _, cancelOrigin := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		var observed engine.RuleOutcome
		r := registry(t, []engine.Registration{{ID: "MD030", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			items, err := c.ListSeparators(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if cancelOrigin {
				cancel()
			}
			observed = compareListMarkerSpacing(context.Background(), c, items, 1)
			for _, finding := range observed.Findings {
				if finding.FixEligible {
					t.Fatal("manual finding grants fix authority")
				}
			}
			return observed
		}}})
		in, _ := engine.NewInput("doc.md", []byte("-  one\n-  two"), md030Settings(), engine.DocumentContext{})
		out, err := r.Lint(ctx, []engine.Input{in}, engine.RunOptions{})
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if cancelOrigin {
			if !errors.Is(observed.Err, context.Canceled) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 {
				t.Fatal(observed, out)
			}
		} else if !out.Complete || len(observed.Findings) != 2 || len(out.Diagnostics) != 2 {
			t.Fatal(observed, out)
		}
	}
}

func TestMD030UnavailableSyntaxIsReducedCoverage(t *testing.T) {
	called := false
	r := registry(t, []engine.Registration{{ID: "MD030", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome {
		called = true
		return engine.RuleOutcome{}
	}}})
	out := analyzeHeadings(t, r, "-  one\n-  two", md030Settings(), engine.DocumentContext{Flavor: "unavailable"}, engine.RunOptions{})
	found := false
	for _, c := range out.Coverage {
		found = found || (c.Rule == "MD030" && c.Reason == result.ParserUncertain)
	}
	if called || out.Complete || !found || len(out.Diagnostics) != 0 || len(out.Errors) != 0 {
		t.Fatal(out)
	}
}

func TestMD030OriginFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 1}, {"aligned", "- one\n- two", 3}, {"violating", "-  one\n-  two", 7}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+reason.Error(), func(t *testing.T) {
				origin := &md018LateContext{Context: context.Background(), stop: 1 << 20, reason: reason}
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD030", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, err := c.ListSeparators(ctx)
					if err != nil {
						t.Fatal(err)
					}
					origin.calls = 0
					origin.stop = tc.final
					observed = compareListMarkerSpacing(context.Background(), c, items, 1)
					if origin.calls != tc.final {
						t.Fatal(origin.calls, tc.final)
					}
					return observed
				}}})
				in, _ := engine.NewInput("doc.md", []byte(tc.source), md030Settings(), engine.DocumentContext{})
				out, err := r.Lint(origin, []engine.Input{in}, engine.RunOptions{})
				if err != nil || !errors.Is(observed.Err, reason) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 {
					t.Fatal(observed, out, err)
				}
			})
		}
	}
}

func TestMD030PositiveIntegerOptions(t *testing.T) {
	r := registry(t, Registrations())
	off := false
	for _, v := range []any{0, -1, 1.5, "1", true, nil} {
		if _, err := r.ResolveSettings(engine.Settings{"MD030": {Enabled: &off, Options: engine.Options{"spaces": v}}}, engine.ValidationProviders{}); err == nil {
			t.Fatal("accepted", v)
		}
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		settings := md030Settings()
		settings["MD030"] = engine.RuleSetting{Options: engine.Options{"spaces": int(^uint(0) >> 1)}}
		out := analyzeHeadings(t, r, "- One", settings, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
		if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 1, End: 2}) {
			t.Fatal(out)
		}
	}
}

func TestMD030DistinctInputOptionsAndConcurrency(t *testing.T) {
	r := registry(t, Registrations())
	makeInput := func(name string, spaces int) engine.Input {
		s := md030Settings()
		s["MD030"] = engine.RuleSetting{Options: engine.Options{"spaces": spaces}}
		in, e := engine.NewInput(name, []byte("-  item"), s, engine.DocumentContext{})
		if e != nil {
			t.Fatal(e)
		}
		return in
	}
	a, b := makeInput("a.md", 1), makeInput("b.md", 2)
	run := func(inputs []engine.Input) {
		out, e := r.Lint(context.Background(), inputs, engine.RunOptions{})
		if e != nil || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Path != "a.md" {
			t.Error(out, e)
		}
	}
	for _, inputs := range [][]engine.Input{{a, b}, {b, a}, {a, b}} {
		run(inputs)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); run([]engine.Input{b, a}) }()
	}
	wg.Wait()
}
