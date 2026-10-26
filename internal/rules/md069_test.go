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

func md069Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD069"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{}}
	return s
}
func TestMD069AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md069.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor  string
		Recognize, Incomplete bool

		Expected []struct {
			Range  []int
			Source string
			Side   string
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
				setting := md069Settings()
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
					if !reflect.DeepEqual(got, w.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source || d.Rule != "MD069" || d.FixAvailable || d.Message != "Repeated identical list markers." {
						t.Fatal(got, d, w)
					}
				}
			})
		}
	}
}

func TestMD069CatalogOptionsAndDefaultActivation(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD069") || r.Available("MD010") || len(r.Metadata()) != 87 {
		t.Fatal("availability")
	}
	enabledCount := 0
	for _, m := range r.Metadata() {
		if m.DefaultEnabled {
			enabledCount++
		}
		if m.ID == "MD069" && (!m.DefaultEnabled || m.Fix != engine.NoFix || !m.RequiresSyntax || m.UsesFrontmatter || len(m.Options) != 0) {
			t.Fatal(m)
		}
	}
	if enabledCount != 59 {
		t.Fatal(enabledCount)
	}
	for _, mapping := range []map[string]any{{"unknown": true}, {"enabled": false, "unknown": true}, {"enabled": nil}, {"enabled": "true"}, {"allow_directives": nil}, {"allow_directives": 1}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD069": setting}, engine.ValidationProviders{})
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
		if _, err = r.ResolveSettings(engine.Settings{"MD069": setting}, engine.ValidationProviders{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		s := md069Settings()
		delete(s, "MD069")
		out := analyzeHeadings(t, r, "- - Text", s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
		if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD069" || out.Diagnostics[0].FixAvailable {
			t.Fatal(out)
		}
	}
}
func TestMD069UTF8GatePrecedesRule(t *testing.T) {
	in, err := engine.NewInput("invalid.md", []byte{'-', ' ', 0xff, '\n', ' ', '-', ' ', 'x'}, md069Settings(), engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := registry(t, Registrations()).Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil || out.Complete || len(out.Errors) != 1 || len(out.Diagnostics) != 0 {
		t.Fatal(out, err)
	}
}

func TestMD069UnavailableSyntaxIsReducedCoverage(t *testing.T) {
	called := false
	r := registry(t, []engine.Registration{{ID: "MD069", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome {
		called = true
		return engine.RuleOutcome{}
	}}})
	out := analyzeHeadings(t, r, "Text\n- one\n# H", md069Settings(), engine.DocumentContext{Flavor: "unavailable"}, engine.RunOptions{})
	found := false
	for _, c := range out.Coverage {
		found = found || (c.Rule == "MD069" && c.Reason == result.ParserUncertain)
	}
	if called || out.Complete || !found || len(out.Diagnostics) != 0 || len(out.Errors) != 0 {
		t.Fatal(out)
	}
}

func TestMD069FinalPublicationWithholdsAllFindings(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 1}, {"aligned", "- one\n- two", 1}, {"violation", "- - one\n\n+ + two", 7}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+reason.Error(), func(t *testing.T) {
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD069", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, err := c.ListRepetitions(ctx)
					if err != nil {
						t.Fatal(err)
					}
					// Fixed independent counts: one check per item, optional
					// finding growth/emission, then the final publication check.
					late := &md018LateContext{Context: ctx, reason: reason, stop: tc.final}
					observed = compareRepeatedListMarkers(late, c, items)
					if late.calls != tc.final {
						t.Fatal(late.calls, tc.final)
					}
					return observed
				}}})
				out := analyzeHeadings(t, r, tc.source, md069Settings(), engine.DocumentContext{}, engine.RunOptions{})
				if !errors.Is(observed.Err, reason) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
					t.Fatal(observed, out)
				}
			})
		}
	}
}

func TestMD069StateAndFindingReservationsUseOriginalAllowance(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		remaining    int64
		kind         string
	}{{"comparison work", "- - a", 0, "work"}, {"finding storage", "- - a\n\n+ + b\n\n* * c", 3, "nodes"}, {"finding emission", "- - a\n\n+ + b\n\n* * c", 6, "nodes"}, {"finding growth work", "- - a\n\n+ + b\n\n* * c", 11, "work"}} {
		t.Run(tc.name, func(t *testing.T) {
			// Calibrate only the unchanged parser/inventory cost in a separate operation.
			// Exhaustion is sticky, so each actual comparison gets a fresh original parse.
			var used int64
			execute := func(calibrate bool) (result.Result, engine.RuleOutcome) {
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD069", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, err := c.ListRepetitions(ctx)
					if err != nil {
						t.Fatal("inventory", err)
					}
					if len(items) != 1 && len(items) != 3 {
						t.Fatal("actual inventory", items)
					}
					for _, item := range items {
						if !item.Present() || item.Start() >= item.End() {
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
					observed = compareRepeatedListMarkers(ctx, c, items)
					return observed
				}}})
				return analyzeHeadings(t, r, tc.source, md069Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget}), observed
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

func TestMD069ElapsedAndCallerControlsAfterInventory(t *testing.T) {
	for _, kind := range []string{"elapsed", "caller"} {
		limits := parser.DefaultLimits()
		if kind == "elapsed" {
			limits.OperationTime = 200 * time.Millisecond
		}
		budget, _ := parser.NewOperationBudget(limits)
		var observed engine.RuleOutcome
		r := registry(t, []engine.Registration{{ID: "MD069", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			items, err := c.ListRepetitions(ctx)
			if err != nil {
				t.Fatal("inventory", err)
			}
			if kind == "elapsed" {
				time.Sleep(250 * time.Millisecond)
			} else {
				ctx = &md018LateContext{Context: ctx, stop: 1, reason: context.Canceled}
			}
			observed = compareRepeatedListMarkers(ctx, c, items)
			return observed
		}}})
		out := analyzeHeadings(t, r, "- - a", md069Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
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

func TestMD069OriginControlsAndManualFindings(t *testing.T) {
	for _, cancelOrigin := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		var observed engine.RuleOutcome
		r := registry(t, []engine.Registration{{ID: "MD069", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			items, err := c.ListRepetitions(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if cancelOrigin {
				cancel()
			}
			observed = compareRepeatedListMarkers(context.Background(), c, items)
			for _, finding := range observed.Findings {
				if finding.FixEligible {
					t.Fatal("manual finding grants fix authority")
				}
			}
			return observed
		}}})
		in, _ := engine.NewInput("doc.md", []byte("- - one\n\n+ + two"), md069Settings(), engine.DocumentContext{})
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

func TestMD069OriginFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 1}, {"aligned", "- one\n- two", 1}, {"violating", "- - one\n\n+ + two", 7}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+reason.Error(), func(t *testing.T) {
				origin := &md018LateContext{Context: context.Background(), stop: 1 << 20, reason: reason}
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD069", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, err := c.ListRepetitions(ctx)
					if err != nil {
						t.Fatal(err)
					}
					origin.calls = 0
					origin.stop = tc.final
					observed = compareRepeatedListMarkers(context.Background(), c, items)
					if origin.calls != tc.final {
						t.Fatal(origin.calls, tc.final)
					}
					return observed
				}}})
				in, _ := engine.NewInput("doc.md", []byte(tc.source), md069Settings(), engine.DocumentContext{})
				out, err := r.Lint(origin, []engine.Input{in}, engine.RunOptions{})
				if err != nil || !errors.Is(observed.Err, reason) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 {
					t.Fatal(observed, out, err)
				}
			})
		}
	}
}

func TestMD069DistinctInputsAndConcurrency(t *testing.T) {
	r := registry(t, Registrations())
	makeInput := func(name, source string) engine.Input {
		in, e := engine.NewInput(name, []byte(source), md069Settings(), engine.DocumentContext{})
		if e != nil {
			t.Fatal(e)
		}
		return in
	}
	a, b := makeInput("a.md", "- - one"), makeInput("b.md", "- two")
	run := func(inputs []engine.Input) {
		out, e := r.Lint(context.Background(), inputs, engine.RunOptions{})
		if e != nil || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Path != "a.md" {
			t.Error(out, e)
		}
	}
	for _, in := range [][]engine.Input{{a, b}, {b, a}, {a, b}} {
		run(in)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); run([]engine.Input{b, a}) }()
	}
	wg.Wait()
	reversed := Registrations()
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	q := registry(t, reversed)
	one, _ := r.Lint(context.Background(), []engine.Input{a, b}, engine.RunOptions{})
	two, _ := q.Lint(context.Background(), []engine.Input{b, a}, engine.RunOptions{})
	if !reflect.DeepEqual(one, two) {
		t.Fatal(one, two)
	}
}

func TestMD069DirectivesRetainNeighborsAndChosenPoints(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source     string
			enabled, allowed bool
			active, hidden   int
		}{
			{"off then enable", "<!-- mdchk:enable MD069 -->\n- - - One\n- - Two", false, true, 2, 0},
			{"off throughout", "- - - One\n- - Two", false, true, 0, 0},
			{"disable retains neighbor", "<!-- mdchk:disable MD069 -->\n- - - One\n- - Two", true, true, 0, 2},
			{"disable denied", "<!-- mdchk:disable MD069 -->\n- - - One\n- - Two", true, false, 2, 0},
			{"enable denied", "<!-- mdchk:enable MD069 -->\n- - - One\n- - Two", false, false, 0, 0},
			{"point precedes inline disable", "- - - One <!-- mdchk:disable MD069 -->\n- - Two", true, true, 1, 1},
			{"protected fake", "- - - `<!-- mdchk:disable MD069 -->`\n- - Two", true, true, 2, 0},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				s := md069Settings()
				s["MD069"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allowed}
				out := analyzeHeadings(t, registry(t, Registrations()), tc.source, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
					t.Fatal(out)
				}
				if tc.name == "point precedes inline disable" && out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 0, End: 5}) {
					t.Fatal(out.Diagnostics)
				}
			})
		}
	}
}

func TestMD069InputSnapshotAndNoCorrectionAuthority(t *testing.T) {
	data := []byte("- - - Text")
	r := registry(t, Registrations())
	in, e := engine.NewInput("doc.md", data, md069Settings(), engine.DocumentContext{})
	if e != nil {
		t.Fatal(e)
	}
	data[0] = '+'
	out, e := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if e != nil || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 0, End: 5}) || out.Diagnostics[0].FixAvailable || len(out.Changes) != 0 || len(out.Documents) != 0 {
		t.Fatal(out, e)
	}
}
