package rules

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func md001Settings(title bool) engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD001"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"frontmatter_title": title}}
	return s
}

func analyzeHeadings(t *testing.T, r *engine.Registry, source string, s engine.Settings, dc engine.DocumentContext, opts engine.RunOptions) result.Result {
	t.Helper()
	caller := []byte(source)
	in, err := engine.NewInput("heading.md", caller, s, dc)
	if err != nil {
		t.Fatal(err)
	}
	token := in.Source().Token()
	out, err := r.Lint(context.Background(), []engine.Input{in}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if string(caller) != source || string(in.Source().Bytes()) != source || in.Source().Token() != token || len(out.Changes) != 0 || len(out.Documents) != 0 || out.Files[0].WriteStatus != "not-requested" {
		t.Fatal("analysis changed original or proposed edits", out)
	}
	for _, raw := range out.SuppressedDiagnostics {
		var suppressed result.SuppressedDiagnostic
		if err := json.Unmarshal(raw, &suppressed); err != nil || suppressed.Diagnostic.FixAvailable {
			t.Fatal("suppressed heading grants fix", string(raw), err)
		}
	}
	for _, d := range out.Diagnostics {
		if d.FixAvailable {
			t.Fatal("report-only heading grants fix", d)
		}
	}
	return out
}

func TestMD001AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md001.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source                 string
		Recognize, Title, Incomplete bool
		Expected                     []struct {
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
				fm, err := parser.NewFrontmatterOptions(f.Recognize, nil)
				if err != nil {
					t.Fatal(err)
				}
				dc := engine.DocumentContext{Flavor: flavor, Frontmatter: fm}
				out := analyzeHeadings(t, r, f.Source, md001Settings(f.Title), dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, md001Settings(f.Title), dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) {
					t.Fatal("nondeterministic analysis")
				}
				if out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(f.Expected) {
					t.Fatalf("unexpected outcome: %+v", out)
				}
				if f.Incomplete && (len(out.Coverage) != 1 || out.Coverage[0].Rule != "MD001" || out.Coverage[0].Reason != result.ContextUnavailable) {
					t.Fatal(out.Coverage)
				}
				for i, want := range f.Expected {
					d := out.Diagnostics[i]
					got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if !reflect.DeepEqual(got, want.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != want.Source || d.Rule != "MD001" || d.Message != "Heading level must not increase by more than one." {
						t.Fatalf("got %v %+v, want %+v", got, d, want)
					}
				}
			})
		}
	}
}

func TestMD001TitlePolicy(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		present       bool
	}{
		{"exact", "title: Guide", true}, {"decoded key", `"ti\u0074le": Guide`, true}, {"native String", `title: !!str 42`, true},
		{"zero width nonspace", "title: \"\u200b\"", true}, {"Unicode blank", "title: \"\u00a0\u2003\"", false},
		{"missing", "other: Guide", false}, {"wrong case", "Title: Guide", false}, {"nested", "other:\n  title: Guide", false},
		{"empty", `title: ""`, false}, {"integer", "title: 42", false}, {"Boolean", "title: true", false}, {"sequence", "title: [Guide]", false}, {"empty mapping", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, opt := range []bool{false, true} {
				fm, _ := parser.NewFrontmatterOptions(true, nil)
				out := analyzeHeadings(t, registry(t, Registrations()), "---\n"+tc.payload+"\n---\n### First\n", md001Settings(opt), engine.DocumentContext{Frontmatter: fm}, engine.RunOptions{})
				want := 0
				if opt && tc.present {
					want = 1
				}
				if !out.Complete || len(out.Diagnostics) != want {
					t.Fatal(out)
				}
			}
		})
	}
	for _, payload := range []string{`title = 42`, `title = false`, `Title = "Guide"`, "[nested]\ntitle = \"Guide\"", `title = "\u00a0"`} {
		fm, _ := parser.NewFrontmatterOptions(true, nil)
		out := analyzeHeadings(t, registry(t, Registrations()), "+++\n"+payload+"\n+++\n### First\n", md001Settings(true), engine.DocumentContext{Frontmatter: fm}, engine.RunOptions{})
		if !out.Complete || len(out.Diagnostics) != 0 {
			t.Fatal(payload, out)
		}
	}
}

func TestMD001DirectivesPreserveHeadingContext(t *testing.T) {
	r := registry(t, Registrations())
	source := "# Root\n<!-- mdchk:disable MD001 -->\n### Hidden\n<!-- mdchk:enable MD001 -->\n#### Fine\n###### Active\n"
	for _, allowed := range []bool{true, false} {
		s := md001Settings(false)
		setting := s["MD001"]
		setting.AllowDirectives = &allowed
		s["MD001"] = setting
		out := analyzeHeadings(t, r, source, s, engine.DocumentContext{}, engine.RunOptions{})
		active, hidden := 2, 0
		if allowed {
			active, hidden = 1, 1
		}
		if !out.Complete || len(out.Diagnostics) != active || len(out.SuppressedDiagnostics) != hidden {
			t.Fatal(out)
		}
		if allowed {
			trace := out.AnalysisRecords()[0].(engine.DirectiveAnalysis)
			if trace.Suppressed()[0].Owner != 0 || trace.Suppressed()[0].Diagnostic.Rule != "MD001" || trace.Suppressed()[0].Diagnostic.ByteRange.Start != 36 {
				t.Fatal(trace.Suppressed())
			}
		}
	}
	s := md001Settings(false)
	off := false
	s["MD001"] = engine.RuleSetting{Enabled: &off}
	if out := analyzeHeadings(t, r, "# Root\n### Third\n", s, engine.DocumentContext{}, engine.RunOptions{}); !out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
	// A suppressed H2 still prevents a false positive at the enabled H3.
	source = "# Root\n<!-- mdchk:disable MD001 -->\n## Context\n<!-- mdchk:enable MD001 -->\n### Fine\n"
	if out := analyzeHeadings(t, r, source, md001Settings(false), engine.DocumentContext{}, engine.RunOptions{}); !out.Complete || len(out.Diagnostics) != 0 || len(out.SuppressedDiagnostics) != 0 {
		t.Fatal(out)
	}
}

func TestMD001CatalogAndOptions(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD001") || r.Available("MD095") || r.Available("MD095") || r.Available("MD072") {
		t.Fatal("incorrect availability")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD001" && (!m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.NoFix || m.Options["frontmatter_title"].Default != false) {
			t.Fatal(m)
		}
	}
	for _, mapping := range []map[string]any{{"frontmatter_title": nil}, {"frontmatter_title": "true"}, {"frontmatter_title": 1}, {"enabled": false, "unknown": true}, {"enabled": false, "frontmatter_title": "false"}, {"enabled": "false"}, {"allow_directives": nil}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD001": setting}, engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal("invalid options accepted", mapping)
		}
	}
	for _, mapping := range []map[string]any{{}, {"frontmatter_title": true}, {"frontmatter_title": false}, {"enabled": false}, {"enabled": true, "allow_directives": false}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.ResolveSettings(engine.Settings{"MD001": setting}, engine.ValidationProviders{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMD001AddedWorkInterruptions(t *testing.T) {
	for _, title := range []bool{false, true} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			var observed error
			r := registry(t, []engine.Registration{{ID: "MD001", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				out := headingIncrements(&traversalContext{Context: ctx, err: reason}, c)
				observed = out.Err
				return out
			}}})
			fm, _ := parser.NewFrontmatterOptions(true, nil)
			out := analyzeHeadings(t, r, "---\ntitle: Guide\n---\n# A\n### B\n", md001Settings(title), engine.DocumentContext{Frontmatter: fm}, engine.RunOptions{})
			if !errors.Is(observed, reason) || out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "analysis" || len(out.Errors) != 0 {
				t.Fatal(observed, out)
			}
		}
	}
	// Exhaust the same cumulative meter only after the original parse has reached
	// the callback. Actual heading/title projection must observe the sticky stop.
	for _, title := range []bool{false, true} {
		for _, kind := range []string{"work", "nodes"} {
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			r := registry(t, []engine.Registration{{ID: "MD001", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				if kind == "work" {
					_ = budget.ChargeWork(budget.Limits().WorkUnits)
				} else {
					_ = budget.ChargeNodes(budget.Limits().TotalNodes)
				}
				return headingIncrements(ctx, c)
			}}})
			fm, _ := parser.NewFrontmatterOptions(true, nil)
			out := analyzeHeadings(t, r, "---\ntitle: Guide\n---\n# A\n### B\n", md001Settings(title), engine.DocumentContext{Frontmatter: fm}, engine.RunOptions{Budget: budget})
			if out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "analysis" || out.Interruptions[0].Limit == nil || len(out.Errors) != 0 {
				t.Fatal(out)
			}
		}
	}
}

func TestMD001UnterminatedHeaderUsesSyntaxScheduling(t *testing.T) {
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	out := analyzeHeadings(t, registry(t, Registrations()), "---\ntitle: Guide\n### Protected\n", md001Settings(true), engine.DocumentContext{Frontmatter: fm}, engine.RunOptions{})
	if out.Complete || len(out.Diagnostics) != 0 || len(out.Errors) != 0 {
		t.Fatal(out)
	}
	found := false
	for _, coverage := range out.Coverage {
		if coverage.Reason == result.ParserUncertain {
			found = true
		}
	}
	if !found {
		t.Fatal(out.Coverage)
	}
}

func TestMD001DefaultDoesNotDecodeMetadata(t *testing.T) {
	r := registry(t, Registrations())
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	dc := engine.DocumentContext{Frontmatter: fm}
	const source = "---\ntitle: Guide\n---\n### First\n##### Later\n"
	// Establish the smallest document allowance at which the actual body-only
	// engine run completes. Opting into title consumption must share that original
	// allowance, while a sufficient allowance admits the virtual-H1 comparison.
	low, high := int64(1), int64(10000)
	run := func(nodes int64, title bool) result.Result {
		limits := parser.DefaultLimits()
		limits.DocumentNodes = nodes
		budget, err := parser.NewOperationBudget(limits)
		if err != nil {
			t.Fatal(err)
		}
		return analyzeHeadings(t, r, source, md001Settings(title), dc, engine.RunOptions{Budget: budget})
	}
	for low < high {
		n := (low + high) / 2
		if run(n, false).Complete {
			high = n
		} else {
			low = n + 1
		}
	}
	body := run(low, false)
	title := run(low, true)
	enough := run(10000, true)
	if !body.Complete || len(body.Diagnostics) != 1 || title.Complete || len(title.Interruptions) != 1 || title.Interruptions[0].Phase != "analysis" || title.Interruptions[0].Limit == nil || title.Interruptions[0].Limit.Name != "document-nodes" || !enough.Complete || len(enough.Diagnostics) != 2 {
		t.Fatal(body, title, enough)
	}
}
