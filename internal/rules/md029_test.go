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

func md029Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD029"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{}}
	return s
}
func TestMD029AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md029.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor  string
		Recognize, Incomplete bool
		Style                 string
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
				setting := md029Settings()
				style := f.Style
				if style == "" {
					style = "ordered"
				}
				setting["MD029"] = engine.RuleSetting{Options: engine.Options{"style": style}}
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
					if !reflect.DeepEqual(got, w.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source || d.Rule != "MD029" || d.FixAvailable || d.Message != "Numbered list prefixes must follow the configured numeric sequence." {
						t.Fatal(got, d, w)
					}
				}
			})
		}
	}
}

func TestMD029CatalogOptionsAndDefaultActivation(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD029") || r.Available("MD011") || len(r.Metadata()) != 87 {
		t.Fatal("availability")
	}
	enabledCount := 0
	for _, m := range r.Metadata() {
		if m.DefaultEnabled {
			enabledCount++
		}
		if m.ID == "MD029" && (!m.DefaultEnabled || m.Fix != engine.PlannedFix || !m.RequiresSyntax || m.UsesFrontmatter || len(m.Options) != 1) {
			t.Fatal(m)
		}
	}
	if enabledCount != 59 {
		t.Fatal(enabledCount)
	}
	for _, mapping := range []map[string]any{{"unknown": true}, {"enabled": false, "unknown": true}, {"enabled": nil}, {"enabled": "true"}, {"allow_directives": nil}, {"allow_directives": 1}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD029": setting}, engine.ValidationProviders{})
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
		if _, err = r.ResolveSettings(engine.Settings{"MD029": setting}, engine.ValidationProviders{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		s := md029Settings()
		delete(s, "MD029")
		out := analyzeHeadings(t, r, "1. first\n3. second", s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
		if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD029" || out.Diagnostics[0].FixAvailable {
			t.Fatal(out)
		}
	}
}

func TestMD029BaselineResetsAcrossInputs(t *testing.T) {
	r := registry(t, Registrations())
	a, _ := engine.NewInput("a.md", []byte("5. one\n9. two"), md029Settings(), engine.DocumentContext{})
	b, _ := engine.NewInput("b.md", []byte("1. one\n3. two"), md029Settings(), engine.DocumentContext{})
	for _, inputs := range [][]engine.Input{{a, b}, {b, a}, {a, b}} {
		out, err := r.Lint(context.Background(), inputs, engine.RunOptions{})
		if err != nil || !out.Complete || len(out.Diagnostics) != 2 {
			t.Fatal(out, err)
		}
	}
}

func TestMD029FinalPublicationWithholdsAllFindings(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 2}, {"aligned", "1. one\n2. two", 4}, {"violation", "1. one\n3. two", 6}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+reason.Error(), func(t *testing.T) {
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD029", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, err := c.ListItems(ctx)
					if err != nil {
						t.Fatal(err)
					}
					// Fixed independent counts: reservation, one check per item, optional
					// finding growth/emission, then the final publication check.
					late := &md018LateContext{Context: ctx, reason: reason, stop: tc.final}
					observed = compareNumberedPrefixes(late, c, items, false)
					if late.calls != tc.final {
						t.Fatal(late.calls, tc.final)
					}
					return observed
				}}})
				out := analyzeHeadings(t, r, tc.source, md029Settings(), engine.DocumentContext{}, engine.RunOptions{})
				if !errors.Is(observed.Err, reason) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
					t.Fatal(observed, out)
				}
			})
		}
	}
}

func TestMD029StateAndFindingReservationsUseOriginalAllowance(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		remaining    int64
		kind         string
	}{{"aligned baseline storage", "1. one\n2. two", 1, "nodes"}, {"aligned baseline work", "1. one\n2. two", 2, "work"}, {"comparison work", "1. one\n2. two", 4, "work"}, {"finding storage", "1. a\n9. b\n9. c", 6, "nodes"}, {"finding emission", "1. one\n3. two", 3, "nodes"}, {"finding growth work", "1. a\n9. b\n9. c", 10, "work"}} {
		t.Run(tc.name, func(t *testing.T) {
			// Calibrate only the unchanged parser/inventory cost in a separate operation.
			// Exhaustion is sticky, so each actual comparison gets a fresh original parse.
			var used int64
			execute := func(calibrate bool) (result.Result, engine.RuleOutcome) {
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD029", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
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
					observed = compareNumberedPrefixes(ctx, c, items, false)
					return observed
				}}})
				return analyzeHeadings(t, r, tc.source, md029Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget}), observed
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

func TestMD029ElapsedAndCallerControlsAfterInventory(t *testing.T) {
	for _, kind := range []string{"elapsed", "caller"} {
		limits := parser.DefaultLimits()
		if kind == "elapsed" {
			limits.OperationTime = 200 * time.Millisecond
		}
		budget, _ := parser.NewOperationBudget(limits)
		var observed engine.RuleOutcome
		r := registry(t, []engine.Registration{{ID: "MD029", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			items, err := c.ListItems(ctx)
			if err != nil {
				t.Fatal("inventory", err)
			}
			if kind == "elapsed" {
				time.Sleep(250 * time.Millisecond)
			} else {
				ctx = &md018LateContext{Context: ctx, stop: 1, reason: context.Canceled}
			}
			observed = compareNumberedPrefixes(ctx, c, items, false)
			return observed
		}}})
		out := analyzeHeadings(t, r, "1. a\n3. b", md029Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
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

func TestMD029UTF8GatePrecedesRule(t *testing.T) {
	in, err := engine.NewInput("invalid.md", []byte{'-', ' ', 0xff, '\n', ' ', '-', ' ', 'x'}, md029Settings(), engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := registry(t, Registrations()).Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil || out.Complete || len(out.Errors) != 1 || len(out.Diagnostics) != 0 {
		t.Fatal(out, err)
	}
}

func TestMD029OriginControlsAndManualFindings(t *testing.T) {
	for _, cancelOrigin := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		var observed engine.RuleOutcome
		r := registry(t, []engine.Registration{{ID: "MD029", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			items, err := c.ListItems(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if cancelOrigin {
				cancel()
			}
			observed = compareNumberedPrefixes(context.Background(), c, items, false)
			for _, finding := range observed.Findings {
				if finding.FixEligible {
					t.Fatal("manual finding grants fix authority")
				}
			}
			return observed
		}}})
		in, _ := engine.NewInput("doc.md", []byte("1. one\n3. two"), md029Settings(), engine.DocumentContext{})
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

func TestMD029ConcurrentInvocationsKeepIndependentBaselines(t *testing.T) {
	r := registry(t, Registrations())
	done := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func(i int) {
			text := "1. a\n3. b"
			if i%2 != 0 {
				text = "9. a\n1. b"
			}
			in, err := engine.NewInput("doc.md", []byte(text), md029Settings(), engine.DocumentContext{})
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

func TestMD029UnavailableSyntaxIsReducedCoverage(t *testing.T) {
	called := false
	r := registry(t, []engine.Registration{{ID: "MD029", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome {
		called = true
		return engine.RuleOutcome{}
	}}})
	out := analyzeHeadings(t, r, "1. one\n3. two", md029Settings(), engine.DocumentContext{Flavor: "unavailable"}, engine.RunOptions{})
	found := false
	for _, c := range out.Coverage {
		found = found || (c.Rule == "MD029" && c.Reason == result.ParserUncertain)
	}
	if called || out.Complete || !found || len(out.Diagnostics) != 0 || len(out.Errors) != 0 {
		t.Fatal(out)
	}
}

func TestMD029InvalidOptionsEvenDisabled(t *testing.T) {
	r := registry(t, Registrations())
	off := false
	for _, o := range []engine.Options{{"style": nil}, {"style": true}, {"style": 1}, {"style": "Ordered"}, {"style": "ONE"}, {"style": "zero"}, {"unknown": true}} {
		if _, err := r.ResolveSettings(engine.Settings{"MD029": {Enabled: &off, Options: o}}, engine.ValidationProviders{}); err == nil {
			t.Fatal(o)
		}
	}
}

func TestMD029ActualGroupsAndHiddenOrdinals(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source   string
			starts, groups []int
			active, hidden int
		}{
			{"nested", "5. a\n\n   8. inner\n   10. child\n6. b\n8. c", []int{5, 8, 10, 6, 8}, []int{0, 1, 1, 0, 0}, 2, 0},
			{"quote", "5. a\n   > 8. inner\n   > 10. child\n6. b", []int{5, 8, 10, 6}, []int{0, 1, 1, 0}, 1, 0},
			{"under bullet", "- a\n\n  3. inner\n  5. child", []int{-1, 3, 5}, []int{0, 1, 1}, 1, 0},
			{"delimiter", "003. a\n9. b\n7) c\n8) d", []int{3, 9, 7, 8}, []int{0, 0, 1, 1}, 1, 0},
			{"hidden", "<!-- mdchk:disable MD029 -->\n5. a\n9. b\n   <!-- mdchk:enable MD029 -->\n7. c\n9. d", []int{5, 9, 7, 9}, []int{0, 0, 0, 0}, 1, 1},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				r := registry(t, []engine.Registration{{ID: "MD029", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, err := c.ListItems(ctx)
					if err != nil || len(items) != len(tc.starts) {
						t.Fatal(items, err)
					}
					for i, item := range items {
						if tc.starts[i] >= 0 && (!item.Ordered || !item.Start.Present || item.Start.Value != tc.starts[i]) {
							t.Fatal(items)
						}
						for j := 0; j < i; j++ {
							if (item.Group == items[j].Group) != (tc.groups[i] == tc.groups[j]) {
								t.Fatal("group premise", items)
							}
						}
					}
					return numberedListPrefixes(ctx, c)
				}}})
				out := analyzeHeadings(t, r, tc.source, md029Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
					t.Fatal(out)
				}
			})
		}
	}
}

func TestMD029OriginFinalPublication(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 2}, {"aligned", "1. one\n2. two", 4}, {"violating", "1. one\n3. two", 6}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+reason.Error(), func(t *testing.T) {
				origin := &md018LateContext{Context: context.Background(), stop: 1 << 20, reason: reason}
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD029", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, err := c.ListItems(ctx)
					if err != nil {
						t.Fatal(err)
					}
					origin.calls = 0
					origin.stop = tc.final
					observed = compareNumberedPrefixes(context.Background(), c, items, false)
					if origin.calls != tc.final {
						t.Fatal(origin.calls, tc.final)
					}
					return observed
				}}})
				in, _ := engine.NewInput("doc.md", []byte(tc.source), md029Settings(), engine.DocumentContext{})
				out, err := r.Lint(origin, []engine.Input{in}, engine.RunOptions{})
				if err != nil || !errors.Is(observed.Err, reason) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 {
					t.Fatal(observed, out, err)
				}
			})
		}
	}
}
func TestMD029DistinctInputStyles(t *testing.T) {
	r := registry(t, Registrations())
	inputs := make([]engine.Input, 3)
	for i, style := range []string{"ordered", "one", "ordered"} {
		s := md029Settings()
		s["MD029"] = engine.RuleSetting{Options: engine.Options{"style": style}}
		inputs[i], _ = engine.NewInput(fmt.Sprintf("doc%d.md", i), []byte("003. one\n004. two"), s, engine.DocumentContext{})
	}
	check := func(batch []engine.Input) error {
		out, err := r.Lint(context.Background(), batch, engine.RunOptions{})
		if err != nil {
			return err
		}
		if !out.Complete || len(out.Diagnostics) != 2 || out.Diagnostics[0].Path != "doc1.md" || out.Diagnostics[1].Path != "doc1.md" {
			return fmt.Errorf("distinct styles outcome %+v", out.Diagnostics)
		}
		return nil
	}
	for _, batch := range [][]engine.Input{inputs, {inputs[2], inputs[1], inputs[0]}, inputs} {
		if err := check(batch); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 8)
	for range 8 {
		go func() { done <- check(inputs) }()
	}
	for range 8 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
func TestMD029RejectsAbsentGroupOrNumericStart(t *testing.T) {
	for _, group := range []bool{false, true} {
		r := registry(t, []engine.Registration{{ID: "MD029", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			items, err := c.ListItems(ctx)
			if err != nil || len(items) != 2 {
				t.Fatal(items, err)
			}
			if group {
				items[1].Group = parser.Span{}
			} else {
				items[1].Start = parser.Optional[int]{}
			}
			out := compareNumberedPrefixes(ctx, c, items, false)
			if !errors.Is(out.Err, parser.ErrModel) || len(out.Findings) != 0 {
				t.Fatal(out)
			}
			return out
		}}})
		out := analyzeHeadings(t, r, "1. a\n3. b", md029Settings(), engine.DocumentContext{}, engine.RunOptions{})
		if out.Complete || len(out.Diagnostics) != 0 {
			t.Fatal(out)
		}
	}
}
