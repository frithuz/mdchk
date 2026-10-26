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

func md007Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD007"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{}}
	return s
}
func TestMD007AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md007.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor  string
		Recognize, Incomplete bool
		Indent                int
		Start                 bool
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
				setting := md007Settings()
				indent := f.Indent
				if indent == 0 {
					indent = 2
				}
				setting["MD007"] = engine.RuleSetting{Options: engine.Options{"indent": indent, "start_indented": f.Start}}
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
					if !reflect.DeepEqual(got, w.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source || d.Rule != "MD007" || d.FixAvailable || d.Message != "Bullet list item indentation must match its configured parent offset." {
						t.Fatal(got, d, w)
					}
				}
			})
		}
	}
}

func TestMD007CatalogOptionsAndDefaultActivation(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD007") || r.Available("MD010") || len(r.Metadata()) != 87 {
		t.Fatal("availability")
	}
	enabledCount := 0
	for _, m := range r.Metadata() {
		if m.DefaultEnabled {
			enabledCount++
		}
		if m.ID == "MD007" && (!m.DefaultEnabled || m.Fix != engine.PlannedFix || !m.RequiresSyntax || m.UsesFrontmatter || len(m.Options) != 2) {
			t.Fatal(m)
		}
	}
	if enabledCount != 59 {
		t.Fatal(enabledCount)
	}
	for _, mapping := range []map[string]any{{"unknown": true}, {"enabled": false, "unknown": true}, {"enabled": nil}, {"enabled": "true"}, {"allow_directives": nil}, {"allow_directives": 1}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD007": setting}, engine.ValidationProviders{})
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
		if _, err = r.ResolveSettings(engine.Settings{"MD007": setting}, engine.ValidationProviders{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		s := md007Settings()
		delete(s, "MD007")
		out := analyzeHeadings(t, r, "- first\n - second", s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
		if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD007" || out.Diagnostics[0].FixAvailable {
			t.Fatal(out)
		}
	}
}

func TestMD007BaselineResetsAcrossInputs(t *testing.T) {
	r := registry(t, Registrations())
	a, _ := engine.NewInput("a.md", []byte(" - one\n- two"), md007Settings(), engine.DocumentContext{})
	b, _ := engine.NewInput("b.md", []byte("- one\n - two"), md007Settings(), engine.DocumentContext{})
	for _, inputs := range [][]engine.Input{{a, b}, {b, a}, {a, b}} {
		out, err := r.Lint(context.Background(), inputs, engine.RunOptions{})
		if err != nil || !out.Complete || len(out.Diagnostics) != 2 {
			t.Fatal(out, err)
		}
	}
}

func TestMD007FinalPublicationWithholdsAllFindings(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 2}, {"aligned", "- one\n- two", 4}, {"violation", "- one\n - two", 6}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+reason.Error(), func(t *testing.T) {
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD007", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, err := c.ListItems(ctx)
					if err != nil {
						t.Fatal(err)
					}
					// Fixed independent counts: reservation, one check per item, optional
					// finding growth/emission, then the final publication check.
					late := &md018LateContext{Context: ctx, reason: reason, stop: tc.final}
					observed = compareBulletIndentation(late, c, items, 2, false)
					if late.calls != tc.final {
						t.Fatal(late.calls, tc.final)
					}
					return observed
				}}})
				out := analyzeHeadings(t, r, tc.source, md007Settings(), engine.DocumentContext{}, engine.RunOptions{})
				if !errors.Is(observed.Err, reason) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
					t.Fatal(observed, out)
				}
			})
		}
	}
}

func TestMD007StateAndFindingReservationsUseOriginalAllowance(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		remaining    int64
		kind         string
	}{{"aligned baseline storage", "- one\n- two", 1, "nodes"}, {"aligned baseline work", "- one\n- two", 2, "work"}, {"comparison work", "- one\n- two", 4, "work"}, {"finding storage", " - a\n - b\n - c", 6, "nodes"}, {"finding emission", "- one\n - two", 3, "nodes"}, {"finding growth work", " - a\n - b\n - c", 10, "work"}} {
		t.Run(tc.name, func(t *testing.T) {
			// Calibrate only the unchanged parser/inventory cost in a separate operation.
			// Exhaustion is sticky, so each actual comparison gets a fresh original parse.
			var used int64
			execute := func(calibrate bool) (result.Result, engine.RuleOutcome) {
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD007", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
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
					observed = compareBulletIndentation(ctx, c, items, 2, false)
					return observed
				}}})
				return analyzeHeadings(t, r, tc.source, md007Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget}), observed
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

func TestMD007ElapsedAndCallerControlsAfterInventory(t *testing.T) {
	for _, kind := range []string{"elapsed", "caller"} {
		limits := parser.DefaultLimits()
		if kind == "elapsed" {
			limits.OperationTime = 200 * time.Millisecond
		}
		budget, _ := parser.NewOperationBudget(limits)
		var observed engine.RuleOutcome
		r := registry(t, []engine.Registration{{ID: "MD007", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			items, err := c.ListItems(ctx)
			if err != nil {
				t.Fatal("inventory", err)
			}
			if kind == "elapsed" {
				time.Sleep(250 * time.Millisecond)
			} else {
				ctx = &md018LateContext{Context: ctx, stop: 1, reason: context.Canceled}
			}
			observed = compareBulletIndentation(ctx, c, items, 2, false)
			return observed
		}}})
		out := analyzeHeadings(t, r, "- a\n - b", md007Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
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

func TestMD007UTF8GatePrecedesRule(t *testing.T) {
	in, err := engine.NewInput("invalid.md", []byte{'-', ' ', 0xff, '\n', ' ', '-', ' ', 'x'}, md007Settings(), engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := registry(t, Registrations()).Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil || out.Complete || len(out.Errors) != 1 || len(out.Diagnostics) != 0 {
		t.Fatal(out, err)
	}
}

func TestMD007OriginControlsAndManualFindings(t *testing.T) {
	for _, cancelOrigin := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		var observed engine.RuleOutcome
		r := registry(t, []engine.Registration{{ID: "MD007", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			items, err := c.ListItems(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if cancelOrigin {
				cancel()
			}
			observed = compareBulletIndentation(context.Background(), c, items, 2, false)
			for _, finding := range observed.Findings {
				if finding.FixEligible {
					t.Fatal("manual finding grants fix authority")
				}
			}
			return observed
		}}})
		in, _ := engine.NewInput("doc.md", []byte("- one\n - two"), md007Settings(), engine.DocumentContext{})
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

func TestMD007ConcurrentInvocationsKeepIndependentBaselines(t *testing.T) {
	r := registry(t, Registrations())
	done := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func(i int) {
			text := "- a\n - b"
			if i%2 != 0 {
				text = " - a\n- b"
			}
			in, err := engine.NewInput("doc.md", []byte(text), md007Settings(), engine.DocumentContext{})
			if err == nil {
				var out result.Result
				out, err = r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
				if err == nil && (!out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange.Start != map[bool]int{true: 1, false: 5}[i%2 != 0]) {
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

func TestMD007UnavailableSyntaxIsReducedCoverage(t *testing.T) {
	called := false
	r := registry(t, []engine.Registration{{ID: "MD007", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome {
		called = true
		return engine.RuleOutcome{}
	}}})
	out := analyzeHeadings(t, r, "- one\n - two", md007Settings(), engine.DocumentContext{Flavor: "unavailable"}, engine.RunOptions{})
	found := false
	for _, c := range out.Coverage {
		found = found || (c.Rule == "MD007" && c.Reason == result.ParserUncertain)
	}
	if called || out.Complete || !found || len(out.Diagnostics) != 0 || len(out.Errors) != 0 {
		t.Fatal(out)
	}
}

func TestMD007ActualParentScalars(t *testing.T) {
	for _, tc := range []struct {
		name, source             string
		parentOrdered            bool
		padding, local, findings int
	}{
		{"bullet", "- One\n   - Two", false, 1, 1, 1},
		{"wrong root", " - One\n   - Two", false, 1, 0, 1},
		{"actual padding", "-   One\n    - Two", false, 3, 0, 1},
		{"quote", "- One\n  > - Two", false, 1, 0, 0},
		{"ordered quote", "1. One\n   > - Two", true, 1, 0, 0},
		{"partial tab", "- One\n\t- Two", false, 1, 2, 1},
		{"ordered", "123. One\n     - Two", true, 1, 0, 0},
		{"same line", "- - Two", false, 1, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := registry(t, []engine.Registration{{ID: "MD007", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				items, err := c.ListItems(ctx)
				if err != nil || len(items) != 2 {
					t.Fatal(items, err)
				}
				if items[1].ParentItem != items[0].Item || items[0].Ordered != tc.parentOrdered || items[0].Padding != tc.padding || items[1].ContentIndent-items[1].MarkerWidth-items[1].Padding != tc.local {
					t.Fatal(items)
				}
				return bulletNestingIndentation(ctx, c)
			}}})
			out := analyzeHeadings(t, r, tc.source, md007Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.findings {
				t.Fatal(out)
			}
		})
	}
}

func TestMD007RootOptionEqualityAndLargeIntegers(t *testing.T) {
	r := registry(t, Registrations())
	for _, indent := range []int{1, 2, 4, 1000000000, int(^uint(0) >> 1)} {
		for _, start := range []bool{false, true} {
			for local := 0; local < 4; local++ {
				s := md007Settings()
				s["MD007"] = engine.RuleSetting{Options: engine.Options{"indent": indent, "start_indented": start}}
				source := fmt.Sprintf("%s- One\n", string(makeSpaces(local)))
				out := analyzeHeadings(t, r, source, s, engine.DocumentContext{}, engine.RunOptions{})
				expected := 0
				if (!start && local != 0) || (start && local != indent) {
					expected = 1
				}
				if !out.Complete || len(out.Diagnostics) != expected {
					t.Fatal(indent, start, local, out)
				}
				// Ordered content target and root-false are independent of configured size.
				out = analyzeHeadings(t, r, "1. One\n   - Two", s, engine.DocumentContext{}, engine.RunOptions{})
				if !out.Complete || len(out.Diagnostics) != 0 {
					t.Fatal(indent, start, out)
				}
			}
		}
	}
}
func makeSpaces(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return b
}

func TestMD007InvalidOptionsEvenDisabled(t *testing.T) {
	r := registry(t, Registrations())
	off := false
	for _, o := range []engine.Options{{"indent": 0}, {"indent": -1}, {"indent": 1.5}, {"indent": "2"}, {"indent": true}, {"indent": nil}, {"start_indented": nil}, {"start_indented": 1}, {"start_indented": "false"}, {"unknown": true}} {
		if _, err := r.ResolveSettings(engine.Settings{"MD007": {Enabled: &off, Options: o}}, engine.ValidationProviders{}); err == nil {
			t.Fatal(o)
		}
	}
}

func TestMD007HiddenParentRetainsActualAnchor(t *testing.T) {
	source := "<!-- mdchk:disable MD007 -->\n - One\n   <!-- mdchk:enable MD007 -->\n     - Two"
	// A wrongly indented hidden parent remains the actual anchor. Its child has
	// local two, hence effective four; at indent four it is accepted.
	for _, allowed := range []bool{false, true} {
		s := md007Settings()
		s["MD007"] = engine.RuleSetting{AllowDirectives: &allowed, Options: engine.Options{"indent": 4}}
		r := registry(t, []engine.Registration{{ID: "MD007", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			items, err := c.ListItems(ctx)
			if err != nil || len(items) != 2 || items[1].ParentItem != items[0].Item {
				t.Fatal(items, err)
			}
			return bulletNestingIndentation(ctx, c)
		}}})
		out := analyzeHeadings(t, r, source, s, engine.DocumentContext{}, engine.RunOptions{})
		active, hidden := 1, 0
		if allowed {
			active, hidden = 0, 1
		}
		if !out.Complete || len(out.Diagnostics) != active || len(out.SuppressedDiagnostics) != hidden {
			t.Fatal(out)
		}
	}
}

func TestMD007RejectsMissingForeignAndNonprecedingParents(t *testing.T) {
	for _, kind := range []string{"missing", "foreign", "nonpreceding"} {
		t.Run(kind, func(t *testing.T) {
			r := registry(t, []engine.Registration{{ID: "MD007", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				items, err := c.ListItems(ctx)
				if err != nil || len(items) != 2 || items[1].ParentItem != items[0].Item {
					t.Fatal(items, err)
				}
				switch kind {
				case "missing":
					items = items[1:]
				case "foreign":
					s := parser.NewSource([]byte("- other"))
					items[1].ParentItem, _ = s.Span(0, 7)
				case "nonpreceding":
					items[0], items[1] = items[1], items[0]
				}
				out := compareBulletIndentation(ctx, c, items, 2, false)
				if !errors.Is(out.Err, parser.ErrModel) || len(out.Findings) != 0 {
					t.Fatal(out)
				}
				return out
			}}})
			out := analyzeHeadings(t, r, "- One\n  - Two", md007Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if out.Complete || len(out.Diagnostics) != 0 {
				t.Fatal(out)
			}
		})
	}
}
