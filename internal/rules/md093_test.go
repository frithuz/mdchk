package rules

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func md093Settings(allowed []string) engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	if allowed == nil {
		allowed = []string{}
	}
	s["MD093"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"allowed_constructs": allowed}}
	return s
}
func TestMD093AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md093.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor  string
		Allowed               []string
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
				out := analyzeHeadings(t, r, f.Source, md093Settings(f.Allowed), dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, md093Settings(f.Allowed), dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) || out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(f.Expected) {
					t.Fatal("outcome", out.Complete, out.Diagnostics, out.Errors, out.Interruptions, "expected", f.Expected)
				}
				for i, w := range f.Expected {
					d := out.Diagnostics[i]
					got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if !reflect.DeepEqual(got, w.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source || d.Rule != "MD093" || d.FixAvailable || d.Message != "Inline formatting is not allowed in headings." {
						t.Fatal(got, d, w)
					}
				}
			})
		}
	}
}
func TestMD093CatalogOptionsAndDefaults(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD093") || r.Available("MD095") || len(r.Metadata()) != 87 {
		t.Fatal("availability")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD093" && (m.DefaultEnabled || m.Fix != engine.NoFix || !m.RequiresSyntax || m.UsesFrontmatter || len(m.Options) != 1) {
			t.Fatal(m)
		}
	}
	for _, enabled := range []bool{false, true} {
		for _, o := range []engine.Options{{"allowed_constructs": nil}, {"allowed_constructs": "code"}, {"allowed_constructs": true}, {"allowed_constructs": []string{"emphasis", "bad"}}, {"allowed_constructs": []any{"code", 1}}, {"allowed_constructs": []string{"Code"}}, {"unknown": []string{}}} {
			if _, err := r.ResolveSettings(engine.Settings{"MD093": {Enabled: &enabled, Options: o}}, engine.ValidationProviders{}); err == nil {
				t.Fatal(o)
			}
		}
	}
	s := md093Settings(nil)
	delete(s, "MD093")
	if out := analyzeHeadings(t, r, "# **x**", s, engine.DocumentContext{}, engine.RunOptions{}); !out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
}
func TestMD093DirectivesAndLateInterruption(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			source         string
			active, hidden int
		}{
			{"# *a* <!-- mdchk:disable MD093 --> **b**", 1, 1},
			{"<!-- mdchk:disable MD093 -->\n# *a* <!-- mdchk:enable MD093 --> **b**", 1, 1},
		} {
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, md093Settings(nil), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
				t.Fatal(out)
			}
		}
	}
	var observed engine.RuleOutcome
	r := registry(t, []engine.Registration{{ID: "MD093", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
		observed = inlineFormattingInHeadings(&traversalContext{Context: ctx, err: context.Canceled}, c)
		return observed
	}}})
	out := analyzeHeadings(t, r, "# *a*", md093Settings(nil), engine.DocumentContext{}, engine.RunOptions{})
	if observed.Err == nil || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(observed, out)
	}
}
func TestMD093LateOperationRetainsRealFindings(t *testing.T) {
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
			r := registry(t, []engine.Registration{{ID: "MD093", Check: func(caller context.Context, c engine.RuleContext) engine.RuleOutcome {
				out := inlineFormattingInHeadings(caller, c)
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
			in, err := engine.NewInput("a.md", []byte("# *A*\n\n# *B*\n\n# *C*"), md093Settings(nil), engine.DocumentContext{})
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
