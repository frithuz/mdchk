package rules

import (
	"context"
	"encoding/json"
	"errors"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"

	"github.com/frithuz/mdchk/internal/result"
	"os"
	"reflect"
	"strings"
	"testing"
)

func md082Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD082"] = engine.RuleSetting{Enabled: &on}
	return s
}
func TestMD082AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md082.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, AnchorStyle                           string
		Recognize, ParserUncertain, Unavailable, AllowFinal bool
		GFMUnavailable                                      *bool
		Expected, GFMExpected                               []struct {
			Range  []int
			Source string
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	r := registry(t, Registrations())
	for _, f := range fixtures {
		if f.Name == "nul normalized" && !strings.ContainsRune(f.Source, 0) {
			t.Fatal("fixture did not decode original NUL byte")
		}
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(f.Name+"/"+flavor, func(t *testing.T) {
				fm, _ := parser.NewFrontmatterOptions(f.Recognize, nil)
				dc := engine.DocumentContext{Flavor: flavor, Frontmatter: fm}
				settings := md082Settings()
				settings["MD082"] = engine.RuleSetting{Enabled: settings["MD082"].Enabled, Options: engine.Options{"allow_final_section": f.AllowFinal}}
				dc.AnchorStyle = f.AnchorStyle
				expected := f.Expected
				if flavor == "gfm" && f.GFMExpected != nil {
					expected = f.GFMExpected
				}
				out := analyzeHeadings(t, r, f.Source, settings, dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, settings, dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) {
					t.Fatal("nondeterministic analysis")
				}
				unavailable := f.Unavailable
				if flavor == "gfm" && f.GFMUnavailable != nil {
					unavailable = *f.GFMUnavailable
				}
				if out.Complete == (f.ParserUncertain || unavailable) || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(expected) {
					t.Fatalf("complete=%v diagnostics=%+v errors=%+v interruptions=%+v", out.Complete, out.Diagnostics, out.Errors, out.Interruptions)
				}
				if f.ParserUncertain || unavailable {
					found := false
					for _, c := range out.Coverage {
						found = found || (f.ParserUncertain && c.Reason == result.ParserUncertain) || (unavailable && c.Reason == result.ContextUnavailable)
					}
					if !found {
						t.Fatal(out.Coverage)
					}
				}
				for i, want := range expected {
					d := out.Diagnostics[i]
					got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if !reflect.DeepEqual(got, want.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != want.Source || d.Rule != "MD082" || d.Message != "Section must contain substantive content." {
						t.Fatalf("got %v %+v, want %+v", got, d, want)
					}
				}
			})
		}
	}
}

func TestMD082DefaultSchemaAndPerDocumentState(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD082") || r.Available("MD095") || len(r.Metadata()) != 87 {
		t.Fatal("availability")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD082" && (m.DefaultEnabled || m.Fix != engine.NoFix || !m.RequiresSyntax || m.UsesFrontmatter || len(m.Options) != 1) {
			t.Fatal(m)
		}
	}
	for _, enabled := range []bool{false, true} {
		for _, option := range []engine.Options{{"allow_final_section": nil}, {"allow_final_section": "true"}, {"allow_final_section": 1}, {"unknown": true}} {
			if _, err := r.ResolveSettings(engine.Settings{"MD082": {Enabled: &enabled, Options: option}}, engine.ValidationProviders{}); err == nil {
				t.Fatal(option)
			}
		}
	}
	s := md082Settings()
	delete(s, "MD082")
	if out := analyzeHeadings(t, r, "# A", s, engine.DocumentContext{}, engine.RunOptions{}); !out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out.Diagnostics)
	}
	var inputs []engine.Input
	for _, path := range []string{"a.md", "b.md"} {
		in, err := engine.NewInput(path, []byte("# A"), md082Settings(), engine.DocumentContext{})
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, in)
	}
	out, err := r.Lint(context.Background(), inputs, engine.RunOptions{})
	if err != nil || !out.Complete || len(out.Diagnostics) != 2 {
		t.Fatal(out.Diagnostics, err)
	}
}

func TestMD082HiddenGlobalContext(t *testing.T) {
	for _, tc := range []struct {
		name, source   string
		final          bool
		active, hidden int
	}{
		{"hidden child counts", "# A\n<!-- mdchk:disable MD082 -->\n## B", false, 0, 1},
		{"hidden last controls finality", "# A\n<!-- mdchk:disable MD082 -->\n# B", true, 1, 0},
		{"hidden earlier not last", "<!-- mdchk:disable MD082 -->\n# A\n<!-- mdchk:enable MD082 -->\n# B", true, 0, 1},
		{"image alt opener cannot protect directive", "# Parent\n\n![<script>](x)\n\n<!-- mdchk:disable MD082 -->\n\n# Empty", false, 0, 1},
		{"image alt closer cannot unprotect directive", "# Parent\n\ntext<script>\n\n![</script>](x)\n\n<!-- mdchk:disable MD082 -->\n\n# Empty", false, 1, 0},
		{"nested child global", "# A\n\n> ## B\n\n# C\nText", false, 1, 0},
	} {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(tc.name+"/"+flavor, func(t *testing.T) {
				s := md082Settings()
				s["MD082"] = engine.RuleSetting{Enabled: s["MD082"].Enabled, Options: engine.Options{"allow_final_section": tc.final}}
				out := analyzeHeadings(t, registry(t, Registrations()), tc.source, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
					t.Fatal(out.Diagnostics, out.SuppressedDiagnostics, out.Errors)
				}
			})
		}
	}
}
func TestMD082AddedWorkInterruptionAndFinalChecks(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		var observed engine.RuleOutcome
		r := registry(t, []engine.Registration{{ID: "MD082", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			observed = sectionsWithoutContent(&traversalContext{Context: ctx, err: reason}, c)
			return observed
		}}})
		out := analyzeHeadings(t, r, "# A\n# A", md082Settings(), engine.DocumentContext{}, engine.RunOptions{})
		if !errors.Is(observed.Err, reason) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
			t.Fatal(observed, out)
		}
	}
}
func TestMD082LateOperationRetainsRealEmptyFindings(t *testing.T) {
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
			r := registry(t, []engine.Registration{{ID: "MD082", Check: func(caller context.Context, c engine.RuleContext) engine.RuleOutcome {
				out := sectionsWithoutContent(caller, c)
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
			in, err := engine.NewInput("a.md", []byte("# A\n# A\n# A"), md082Settings(), engine.DocumentContext{})
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
