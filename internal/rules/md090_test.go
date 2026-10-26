package rules

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func md090Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD090"] = engine.RuleSetting{Enabled: &on}
	return s
}
func TestMD090AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md090.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source          string
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
			t.Run(f.Name+"/"+flavor, func(t *testing.T) {
				fm, _ := parser.NewFrontmatterOptions(f.Recognize, nil)
				dc := engine.DocumentContext{Flavor: flavor, Frontmatter: fm}
				out := analyzeHeadings(t, r, f.Source, md090Settings(), dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, md090Settings(), dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) || out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(f.Expected) {
					t.Fatal(out)
				}
				for i, w := range f.Expected {
					d := out.Diagnostics[i]
					got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if !reflect.DeepEqual(got, w.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source || d.Rule != "MD090" || d.Message != "Thematic break must not immediately precede a heading." {
						t.Fatal(got, d, w)
					}
				}
			})
		}
	}
}
func TestMD090SlidesOptionsAndSchema(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD090") || r.Available("MD095") || len(r.Metadata()) != 87 {
		t.Fatal("availability")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD090" && (m.DefaultEnabled || m.Fix != engine.NoFix || !m.RequiresSyntax || m.UsesFrontmatter || len(m.Options) != 1 || m.Options["allow_slides"].Default != true) {
			t.Fatal(m)
		}
	}
	for _, enabled := range []bool{false, true} {
		for _, o := range []engine.Options{{"allow_slides": nil}, {"allow_slides": "true"}, {"allow_slides": 1}, {"unknown": true}} {
			if _, err := r.ResolveSettings(engine.Settings{"MD090": {Enabled: &enabled, Options: o}}, engine.ValidationProviders{}); err == nil {
				t.Fatal(o)
			}
		}
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, slide := range []bool{false, true} {
			for _, allow := range []bool{false, true} {
				dc := engine.DocumentContext{Flavor: flavor}
				if slide {
					dc.Slides = engine.SlideContext{Known: true, Slides: true, Pattern: "**"}
				}
				s := md090Settings()
				s["MD090"] = engine.RuleSetting{Enabled: s["MD090"].Enabled, Options: engine.Options{"allow_slides": allow}}
				out := analyzeHeadings(t, r, "***\n# H", s, dc, engine.RunOptions{})
				want := 1
				if slide && allow {
					want = 0
				}
				if !out.Complete || len(out.Diagnostics) != want {
					t.Fatal(slide, allow, out)
				}
			}
		}
	}
	s := md090Settings()
	delete(s, "MD090")
	if out := analyzeHeadings(t, r, "***\n# H", s, engine.DocumentContext{}, engine.RunOptions{}); !out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
}
func TestMD090HiddenHeadingDoesNotEraseSibling(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			source         string
			active, hidden int
		}{
			{"***\n# H <!-- mdchk:disable MD090 -->", 1, 0},
			{"<!-- mdchk:disable MD090 -->\n***\n# H <!-- mdchk:enable MD090 -->", 0, 1},
			{"***\n<!-- mdchk:disable MD090 -->\n# H", 0, 0},
		} {
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, md090Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
				t.Fatal(flavor, tc, out)
			}
		}
	}
}
func TestMD090KnownSlideExemptionBeforeSyntax(t *testing.T) {
	r := registry(t, Registrations())
	for _, allow := range []bool{false, true} {
		fm, _ := parser.NewFrontmatterOptions(true, nil)
		dc := engine.DocumentContext{Frontmatter: fm, Slides: engine.SlideContext{Known: true, Slides: true, Pattern: "**"}}
		s := md090Settings()
		s["MD090"] = engine.RuleSetting{Enabled: s["MD090"].Enabled, Options: engine.Options{"allow_slides": allow}}
		selected, err := r.SelectRules(s, engine.ValidationProviders{})
		if err != nil {
			t.Fatal(err)
		}
		if dc.Flavor == "" {
			dc.Flavor = "commonmark"
		}
		plan, err := selected.Plan(dc, nil)
		if err != nil {
			t.Fatal(err)
		}
		in, err := plan.NewInput("deck.md", []byte("---\n# H"))
		if err != nil {
			t.Fatal(err)
		}
		out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{})
		if err != nil || out.Complete || len(out.Diagnostics) != 0 {
			t.Fatal(out, err)
		}
		found := false
		for _, e := range trace.Evaluations() {
			if e.Rule == "MD090" {
				found = reflect.DeepEqual(e.Reasons, []string{"inapplicable"})
			}
		}
		if found != allow {
			t.Fatal(allow, trace.Evaluations())
		}
		for _, c := range out.Coverage {
			if allow && c.Rule == "MD090" && c.Reason == result.ParserUncertain {
				t.Fatal(c)
			}
		}
	}
}

func TestMD090AddedWorkInterruptionAndFinalChecks(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		var observed engine.RuleOutcome
		r := registry(t, []engine.Registration{{ID: "MD090", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			observed = thematicBreaksBeforeHeadings(&traversalContext{Context: ctx, err: reason}, c)
			return observed
		}}})
		out := analyzeHeadings(t, r, "***\n# A", md090Settings(), engine.DocumentContext{}, engine.RunOptions{})
		if !errors.Is(observed.Err, reason) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
			t.Fatal(observed, out)
		}
	}
}
func TestMD090LateOperationRetainsRealEmptyFindings(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			var ctx context.Context
			var cancel context.CancelFunc
			if deadline {
				ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			r := registry(t, []engine.Registration{{ID: "MD090", Check: func(caller context.Context, c engine.RuleContext) engine.RuleOutcome {
				out := thematicBreaksBeforeHeadings(caller, c)
				if out.Err != nil || len(out.Findings) != 3 {
					t.Fatal(out)
				}
				if deadline {
					time.Sleep(2 * time.Millisecond)
				} else {
					cancel()
				}
				return out
			}}})
			in, err := engine.NewInput("a.md", []byte("***\n# A\n\n***\n# B\n\n***\n# C"), md090Settings(), engine.DocumentContext{})
			if err != nil {
				t.Fatal(err)
			}
			out, err := r.Lint(ctx, []engine.Input{in}, engine.RunOptions{})
			if err != nil || out.Complete || len(out.Diagnostics) != 3 || len(out.Interruptions) != 1 || len(out.Errors) != 0 {
				t.Fatal(out, err)
			}
		})
	}
}
