package rules

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func md063Settings(style string, names []string) engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	if names == nil {
		names = []string{}
	}
	s["MD063"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"style": style, "proper_names": names}}
	return s
}

func TestMD063AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md063.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source          string
		Style                 string
		Names                 []string
		Recognize, Incomplete bool
		Expected, GFMExpected []struct {
			Range  []int
			Source string
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	r := registry(t, Registrations())
	for _, f := range fixtures {
		if f.Name == "nul" && !strings.ContainsRune(f.Source, 0) {
			t.Fatal("fixture did not decode original NUL byte")
		}
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(f.Name+"/"+flavor, func(t *testing.T) {
				fm, _ := parser.NewFrontmatterOptions(f.Recognize, nil)
				dc := engine.DocumentContext{Flavor: flavor, Frontmatter: fm}
				settings := md063Settings(f.Style, f.Names)
				expected := f.Expected
				if flavor == "gfm" && f.GFMExpected != nil {
					expected = f.GFMExpected
				}
				out := analyzeHeadings(t, r, f.Source, settings, dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, settings, dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) {
					t.Fatal("nondeterministic analysis")
				}
				if out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(expected) {
					t.Fatalf("unexpected outcome: %+v", out)
				}
				if f.Incomplete {
					found := false
					for _, c := range out.Coverage {
						found = found || c.Reason == result.ParserUncertain
					}
					if !found {
						t.Fatal(out.Coverage)
					}
				}
				for i, want := range expected {
					d := out.Diagnostics[i]
					got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if !reflect.DeepEqual(got, want.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != want.Source || d.Rule != "MD063" || d.Message != "Heading must follow the configured capitalization style." {
						t.Fatalf("got %v %+v, want %+v", got, d, want)
					}
				}
			})
		}
	}
}

func TestMD063CatalogAndOptions(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD063") || r.Available("MD095") || len(r.Metadata()) != 87 {
		t.Fatal("availability")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD063" && (m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.PlannedFix || len(m.Options) != 2) {
			t.Fatal(m)
		}
	}
	for _, enabled := range []bool{true, false} {
		for _, options := range []engine.Options{{"style": "other"}, {"style": nil}, {"proper_names": nil}, {"proper_names": "x"}, {"proper_names": []any{"x", 1}}, {"unknown": true}} {
			if _, err := r.ResolveSettings(engine.Settings{"MD063": {Enabled: &enabled, Options: options}}, engine.ValidationProviders{}); err == nil {
				t.Fatal(options)
			}
		}
		for _, names := range [][]string{{}, {""}, {"van\nGogh", "**literal**", " "}, {string([]byte{0xff})}} {
			if _, err := r.ResolveSettings(engine.Settings{"MD063": {Enabled: &enabled, Options: engine.Options{"proper_names": names}}}, engine.ValidationProviders{}); err != nil {
				t.Fatal(names, err)
			}
		}
	}
	s := md063Settings("sentence", nil)
	delete(s, "MD063")
	if out := analyzeHeadings(t, r, "# lower", s, engine.DocumentContext{}, engine.RunOptions{}); !out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
}

func TestMD063InvalidUTF8NameDoesNotMatchReplacement(t *testing.T) {
	r := registry(t, Registrations())
	for _, tc := range []struct {
		name string
		want int
	}{{"\xfflower", 1}, {"�lower", 0}} {
		out := analyzeHeadings(t, r, "# \x00lower", md063Settings("sentence", []string{tc.name}), engine.DocumentContext{}, engine.RunOptions{})
		if !out.Complete || len(out.Diagnostics) != tc.want {
			t.Fatal(tc, out)
		}
		if tc.want != 0 && out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 3, End: 4}) {
			t.Fatal(out.Diagnostics)
		}
	}
}

func TestMD063Directives(t *testing.T) {
	for _, tc := range []struct {
		name, source     string
		enabled, allowed bool
		active, hidden   int
	}{
		{"first hidden no realignment", "<!-- mdchk:disable MD063 -->\n# first second\n<!-- mdchk:enable MD063 -->\n# Third fourth", true, true, 1, 1},
		{"configured enable", "<!-- mdchk:enable MD063 -->\n# lower", false, true, 1, 0},
		{"off throughout", "# lower", false, true, 0, 0},
		{"denied disable", "<!-- mdchk:disable MD063 -->\n# lower", true, false, 1, 0},
		{"denied enable", "<!-- mdchk:enable MD063 -->\n# lower", false, false, 0, 0},
		{"fake code", "`<!-- mdchk:disable MD063 -->`\n\n# lower", true, true, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := md063Settings("title", nil)
			s["MD063"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allowed, Options: s["MD063"].Options}
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, s, engine.DocumentContext{}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
				t.Fatal(out)
			}
		})
	}
}

func TestMD063AddedWorkInterruptions(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		var observed error
		r := registry(t, []engine.Registration{{ID: "MD063", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			out := headingCapitalization(&traversalContext{Context: ctx, err: reason}, c)
			observed = out.Err
			return out
		}}})
		out := analyzeHeadings(t, r, "# lower", md063Settings("sentence", nil), engine.DocumentContext{}, engine.RunOptions{})
		if !errors.Is(observed, reason) || out.Complete || len(out.Interruptions) != 1 || len(out.Diagnostics) != 0 || len(out.Errors) != 0 {
			t.Fatal(observed, out)
		}
	}
	for _, kind := range []string{"work", "nodes"} {
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		r := registry(t, []engine.Registration{{ID: "MD063", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			if kind == "work" {
				_ = budget.ChargeWork(budget.Limits().WorkUnits)
			} else {
				_ = budget.ChargeNodes(budget.Limits().TotalNodes)
			}
			return headingCapitalization(ctx, c)
		}}})
		out := analyzeHeadings(t, r, "# lower", md063Settings("sentence", nil), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
		if out.Complete || len(out.Interruptions) != 1 || len(out.Diagnostics) != 0 || len(out.Errors) != 0 {
			t.Fatal(out)
		}
	}
}

func TestMD063NameMatchingWorkAndWithholding(t *testing.T) {
	source := "# " + strings.Repeat("a", 300) + "z"
	names := make([]string, 40)
	for i := range names {
		names[i] = strings.Repeat("a", 300) + "x"
	}
	names[len(names)-1] = strings.Repeat("a", 300) + "z"
	run := func(n []string, work int64) result.Result {
		limits := parser.DefaultLimits()
		limits.WorkUnits = work
		budget, _ := parser.NewOperationBudget(limits)
		return analyzeHeadings(t, registry(t, Registrations()), source, md063Settings("sentence", n), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
	}
	empty := run(nil, 180000)
	small := run(names, 180000)
	enough := run(names, 500000)
	if !empty.Complete || len(empty.Diagnostics) != 1 || small.Complete || len(small.Interruptions) != 1 || len(small.Diagnostics) != 0 || !enough.Complete || len(enough.Diagnostics) != 0 {
		t.Fatal(empty, small, enough)
	}
	execute := func(stop int, reason error) (engine.RuleOutcome, int) {
		var observed engine.RuleOutcome
		calls := 0
		r := registry(t, []engine.Registration{{ID: "MD063", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			check := &md018LateContext{Context: ctx, reason: reason, stop: stop}
			observed = headingCapitalization(check, c)
			calls = check.calls
			return observed
		}}})
		analyzeHeadings(t, r, "# lower\n# other", md063Settings("title", []string{"other"}), engine.DocumentContext{}, engine.RunOptions{})
		return observed, calls
	}
	full, total := execute(0, context.Canceled)
	if full.Err != nil || len(full.Findings) != 1 {
		t.Fatal(full)
	}
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, stop := range []int{1, total / 2, total} {
			got, _ := execute(stop, reason)
			if !errors.Is(got.Err, reason) {
				t.Fatal(stop, got)
			}
			if stop < total/2 && len(got.Findings) != 0 {
				t.Fatal("dependent finding escaped", stop, got)
			}
		}
	}
}

func TestMD063LateOperationRetainsReliableFindings(t *testing.T) {
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
			r := registry(t, []engine.Registration{{ID: "MD063", Check: func(inner context.Context, c engine.RuleContext) engine.RuleOutcome {
				out := headingCapitalization(inner, c)
				if out.Err != nil || len(out.Findings) != 2 {
					t.Fatal(out)
				}
				if deadline {
					time.Sleep(2 * time.Millisecond)
				} else {
					cancel()
				}
				return out
			}}})
			input, err := engine.NewInput("heading.md", []byte("# lower\n# other"), md063Settings("sentence", nil), engine.DocumentContext{})
			if err != nil {
				t.Fatal(err)
			}
			out, err := r.Lint(ctx, []engine.Input{input}, engine.RunOptions{})
			if err != nil || out.Complete || len(out.Diagnostics) != 2 || len(out.Interruptions) != 1 || len(out.Errors) != 0 {
				t.Fatal(err, out)
			}
		})
	}
}

func TestMD063FinalCandidateWithholding(t *testing.T) {
	execute := func(stop int, reason error) (parser.Span, int, error) {
		var span parser.Span
		var failure error
		calls := 0
		r := registry(t, []engine.Registration{{ID: "MD063", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			headings, err := c.HeadingScalars(ctx)
			if err != nil {
				t.Fatal(err)
			}
			count := &md018LateContext{Context: ctx, stop: stop, reason: reason}
			span, failure = capitalizationFinding(count, c, "sentence", nil, headings[0])
			calls = count.calls
			return engine.RuleOutcome{Err: failure}
		}}})
		analyzeHeadings(t, r, "# lower", md063Settings("sentence", nil), engine.DocumentContext{}, engine.RunOptions{})
		return span, calls, failure
	}
	span, total, err := execute(0, context.Canceled)
	if err != nil || !span.Present() {
		t.Fatal(span, err)
	}
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, stop := range []int{1, total} {
			span, _, err := execute(stop, reason)
			if !errors.Is(err, reason) || span.Present() {
				t.Fatal(stop, span, err)
			}
		}
	}
}
