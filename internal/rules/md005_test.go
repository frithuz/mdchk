package rules

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func md005Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD005"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{}}
	return s
}
func TestMD005AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md005.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor  string
		Recognize, Incomplete bool
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
				out := analyzeHeadings(t, r, f.Source, md005Settings(), dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, md005Settings(), dc, engine.RunOptions{})
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
					if !reflect.DeepEqual(got, w.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source || d.Rule != "MD005" || d.FixAvailable || d.Message != "Sibling list item markers must align with the first item." {
						t.Fatal(got, d, w)
					}
				}
			})
		}
	}
}

func TestMD005CatalogOptionsAndDefaultActivation(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD005") || r.Available("MD011") || len(r.Metadata()) != 87 {
		t.Fatal("availability")
	}
	enabledCount := 0
	for _, m := range r.Metadata() {
		if m.DefaultEnabled {
			enabledCount++
		}
		if m.ID == "MD005" && (!m.DefaultEnabled || m.Fix != engine.PlannedFix || !m.RequiresSyntax || m.UsesFrontmatter || len(m.Options) != 0) {
			t.Fatal(m)
		}
	}
	if enabledCount != 59 {
		t.Fatal(enabledCount)
	}
	for _, mapping := range []map[string]any{{"unknown": true}, {"enabled": false, "unknown": true}, {"enabled": nil}, {"enabled": "true"}, {"allow_directives": nil}, {"allow_directives": 1}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD005": setting}, engine.ValidationProviders{})
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
		if _, err = r.ResolveSettings(engine.Settings{"MD005": setting}, engine.ValidationProviders{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		s := md005Settings()
		delete(s, "MD005")
		out := analyzeHeadings(t, r, "- first\n - second", s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
		if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD005" || out.Diagnostics[0].FixAvailable {
			t.Fatal(out)
		}
	}
}

func TestMD005DirectivesPreserveActualGroupBaseline(t *testing.T) {
	source := "<!-- mdchk:disable MD005 -->\n - one\n- two\n  <!-- mdchk:enable MD005 -->\n- three"
	for _, tc := range []struct {
		enabled, allowed bool
		active, hidden   int
	}{{true, true, 1, 1}, {false, true, 1, 1}, {true, false, 2, 0}, {false, false, 0, 0}} {
		for _, flavor := range []string{"commonmark", "gfm"} {
			s := md005Settings()
			s["MD005"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allowed}
			out := analyzeHeadings(t, registry(t, Registrations()), source, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
				t.Fatal(tc, out)
			}
			if tc.active == 1 && out.Diagnostics[0].ByteRange.Start != 72 {
				t.Fatal(out.Diagnostics)
			}
			if tc.hidden == 1 {
				var d struct {
					Diagnostic result.Diagnostic `json:"diagnostic"`
				}
				if err := json.Unmarshal(out.SuppressedDiagnostics[0], &d); err != nil || d.Diagnostic.ByteRange.Start != 36 {
					t.Fatal(out.SuppressedDiagnostics, err)
				}
			}
		}
	}
	// Verify parser ownership independently: all three original items really share
	// one group, including the hidden first item and suppressed middle mismatch.
	r := registry(t, []engine.Registration{{ID: "MD005", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
		items, err := c.ListItems(ctx)
		if err != nil || len(items) != 3 {
			t.Fatal(items, err)
		}
		for i, item := range items {
			if item.Group != items[0].Group || item.ContentIndent-item.MarkerWidth-item.Padding != []int{1, 0, 0}[i] {
				t.Fatal(items)
			}
		}
		return siblingListAlignment(ctx, c)
	}}})
	out := analyzeHeadings(t, r, source, md005Settings(), engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 1 {
		t.Fatal(out)
	}
}

func TestMD005BaselineResetsAcrossInputs(t *testing.T) {
	r := registry(t, Registrations())
	a, _ := engine.NewInput("a.md", []byte(" - one\n- two"), md005Settings(), engine.DocumentContext{})
	b, _ := engine.NewInput("b.md", []byte("- one\n - two"), md005Settings(), engine.DocumentContext{})
	for _, inputs := range [][]engine.Input{{a, b}, {b, a}, {a, b}} {
		out, err := r.Lint(context.Background(), inputs, engine.RunOptions{})
		if err != nil || !out.Complete || len(out.Diagnostics) != 2 {
			t.Fatal(out, err)
		}
	}
}

func TestMD005FinalPublicationWithholdsAllFindings(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 2}, {"aligned", "- one\n- two", 4}, {"violation", "- one\n - two", 6}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+reason.Error(), func(t *testing.T) {
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD005", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, err := c.ListItems(ctx)
					if err != nil {
						t.Fatal(err)
					}
					// Fixed independent counts: reservation, one check per item, optional
					// finding growth/emission, then the final publication check.
					late := &md018LateContext{Context: ctx, reason: reason, stop: tc.final}
					observed = compareListAlignment(late, c, items)
					if late.calls != tc.final {
						t.Fatal(late.calls, tc.final)
					}
					return observed
				}}})
				out := analyzeHeadings(t, r, tc.source, md005Settings(), engine.DocumentContext{}, engine.RunOptions{})
				if !errors.Is(observed.Err, reason) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
					t.Fatal(observed, out)
				}
			})
		}
	}
}

func TestMD005StateAndFindingReservationsUseOriginalAllowance(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		remaining    int64
		kind         string
	}{{"aligned baseline storage", "- one\n- two", 1, "nodes"}, {"aligned baseline work", "- one\n- two", 2, "work"}, {"comparison work", "- one\n- two", 4, "work"}, {"finding storage", "- a\n - b\n  - c", 6, "nodes"}, {"finding emission", "- one\n - two", 3, "nodes"}, {"finding growth work", "- a\n - b\n  - c", 10, "work"}} {
		t.Run(tc.name, func(t *testing.T) {
			// Calibrate only the unchanged parser/inventory cost in a separate operation.
			// Exhaustion is sticky, so each actual comparison gets a fresh original parse.
			var used int64
			execute := func(calibrate bool) (result.Result, engine.RuleOutcome) {
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD005", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, err := c.ListItems(ctx)
					if err != nil {
						t.Fatal("inventory", err)
					}
					// Three-item growth cases must be real direct siblings.
					// With six nodes remaining: baseline3 + capacity1 + record1
					// leaves one; second capacity2 must fail. Omitting growth
					// charges allows both records to fit and is caught directly.
					if tc.name == "finding storage" || tc.name == "finding growth work" {
						if len(items) != 3 {
							t.Fatal("expected three actual siblings", items)
						}
						for _, item := range items {
							if item.Group != items[0].Group {
								t.Fatal("growth fixture split actual groups", items)
							}
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
					observed = compareListAlignment(ctx, c, items)
					return observed
				}}})
				return analyzeHeadings(t, r, tc.source, md005Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget}), observed
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

func TestMD005ElapsedAndCallerControlsAfterInventory(t *testing.T) {
	for _, kind := range []string{"elapsed", "caller"} {
		limits := parser.DefaultLimits()
		if kind == "elapsed" {
			limits.OperationTime = 200 * time.Millisecond
		}
		budget, _ := parser.NewOperationBudget(limits)
		var observed engine.RuleOutcome
		r := registry(t, []engine.Registration{{ID: "MD005", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			items, err := c.ListItems(ctx)
			if err != nil {
				t.Fatal("inventory", err)
			}
			if kind == "elapsed" {
				time.Sleep(250 * time.Millisecond)
			} else {
				ctx = &md018LateContext{Context: ctx, stop: 1, reason: context.Canceled}
			}
			observed = compareListAlignment(ctx, c, items)
			return observed
		}}})
		out := analyzeHeadings(t, r, "- a\n - b", md005Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
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

func TestMD005UTF8GatePrecedesRule(t *testing.T) {
	in, err := engine.NewInput("invalid.md", []byte{'-', ' ', 0xff, '\n', ' ', '-', ' ', 'x'}, md005Settings(), engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := registry(t, Registrations()).Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil || out.Complete || len(out.Errors) != 1 || len(out.Diagnostics) != 0 {
		t.Fatal(out, err)
	}
}

func TestMD005OriginControlsAndManualFindings(t *testing.T) {
	for _, cancelOrigin := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		var observed engine.RuleOutcome
		r := registry(t, []engine.Registration{{ID: "MD005", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			items, err := c.ListItems(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if cancelOrigin {
				cancel()
			}
			observed = compareListAlignment(context.Background(), c, items)
			for _, finding := range observed.Findings {
				if finding.FixEligible {
					t.Fatal("manual finding grants fix authority")
				}
			}
			return observed
		}}})
		in, _ := engine.NewInput("doc.md", []byte("9. one\n 10. two"), md005Settings(), engine.DocumentContext{})
		out, err := r.Lint(ctx, []engine.Input{in}, engine.RunOptions{})
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if cancelOrigin {
			if !errors.Is(observed.Err, context.Canceled) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 {
				t.Fatal(observed, out)
			}
		} else if !out.Complete || len(observed.Findings) != 1 || len(out.Diagnostics) != 1 {
			t.Fatal(observed, out)
		}
	}
}

func TestMD005ConcurrentInvocationsKeepIndependentBaselines(t *testing.T) {
	r := registry(t, Registrations())
	done := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func(i int) {
			text := "- a\n - b"
			if i%2 != 0 {
				text = " - a\n- b"
			}
			in, err := engine.NewInput("doc.md", []byte(text), md005Settings(), engine.DocumentContext{})
			if err == nil {
				var out result.Result
				out, err = r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
				if err == nil && (!out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange.Start != 5) {
					err = fmt.Errorf("unexpected independent result: %+v", out)
				}
			}
			done <- err
		}(i)
	}
	for i := 0; i < 16; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestMD005UnavailableSyntaxIsReducedCoverage(t *testing.T) {
	called := false
	r := registry(t, []engine.Registration{{ID: "MD005", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome {
		called = true
		return engine.RuleOutcome{}
	}}})
	out := analyzeHeadings(t, r, "- one\n - two", md005Settings(), engine.DocumentContext{Flavor: "unavailable"}, engine.RunOptions{})
	found := false
	for _, c := range out.Coverage {
		found = found || (c.Rule == "MD005" && c.Reason == result.ParserUncertain)
	}
	if called || out.Complete || !found || len(out.Diagnostics) != 0 || len(out.Errors) != 0 {
		t.Fatal(out)
	}
}
