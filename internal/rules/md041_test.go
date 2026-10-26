package rules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func md041Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD041"] = engine.RuleSetting{Enabled: &on}
	return s
}

func TestMD041AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md041.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source                 string
		Recognize, Title, Incomplete bool
		Level                        int
		Expected                     [][]int
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		if f.Name == "nul" && !strings.ContainsRune(f.Source, 0) {
			t.Fatal("fixture did not decode original NUL")
		}
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(f.Name+"/"+flavor, func(t *testing.T) {
				fm, _ := parser.NewFrontmatterOptions(f.Recognize, nil)
				s := md041Settings()
				level := f.Level
				if level == 0 {
					level = 1
				}
				s["MD041"] = engine.RuleSetting{Options: engine.Options{"level": level, "frontmatter_title": f.Title}}
				r := registry(t, Registrations())
				dc := engine.DocumentContext{Flavor: flavor, Frontmatter: fm}
				out := analyzeHeadings(t, r, f.Source, s, dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, s, dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) {
					t.Fatal("nondeterministic analysis")
				}
				if out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(f.Expected) {
					t.Fatalf("complete=%v diag=%v coverage=%v errors=%v", out.Complete, out.Diagnostics, out.Coverage, out.Errors)
				}
				if f.Incomplete {
					found := false
					for _, c := range out.Coverage {
						found = found || c.Reason == result.ContextUnavailable || c.Reason == result.ParserUncertain
					}
					if !found {
						t.Fatal(out.Coverage)
					}
				}
				for i, w := range f.Expected {
					d := out.Diagnostics[i]
					if d.Rule != "MD041" || d.ByteRange.Start != w[0] || d.ByteRange.End != w[0] || d.Range.Start.Line != w[1] || d.Range.Start.Column != w[2] || d.Range.End != d.Range.Start || d.Message != "Document must begin with a heading at the configured level." {
						t.Fatalf("got %+v want %v", d, w)
					}
				}
			})
		}
	}
}

func TestMD041LevelsAndMetadata(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for level := 1; level <= 6; level++ {
			for _, tc := range []struct {
				name, header, body string
				title, incomplete  bool
				count              int
			}{
				{"correct", "", strings.Repeat("#", level) + " A", false, false, 0},
				{"wrong", "", strings.Repeat("#", level%6+1) + " A", false, false, 1},
				{"yaml", "---\ntitle: Guide\n---\n", "Intro", true, false, 0},
				{"toml", "+++\ntitle = \"Guide\"\n+++\n", "Intro", true, false, 0},
				{"positive partial", "---\ntitle: Guide\nother: !custom x\n---\n", "Intro", true, false, 0},
				{"decoded key", "---\n\"ti\\u0074le\": !!str 42\n---\n", "Intro", true, false, 0},
				{"toml decoded key", "+++\n\"ti\\u0074le\" = \"42\"\n+++\n", "Intro", true, false, 0},
				{"alias", "---\nbase: &b Guide\ntitle: *b\n---\n", "Intro", true, false, 0},
				{"unknown", "---\ntitle: A\ntitle: B\n---\n", "Intro", true, true, 0},
				{"unknown satisfied", "---\ntitle: A\ntitle: B\n---\n", strings.Repeat("#", level) + " A", true, false, 0},
			} {
				t.Run(fmt.Sprintf("%s/%d/%s", flavor, level, tc.name), func(t *testing.T) {
					s := md041Settings()
					s["MD041"] = engine.RuleSetting{Options: engine.Options{"level": level, "frontmatter_title": tc.title}}
					fm, _ := parser.NewFrontmatterOptions(true, nil)
					out := analyzeHeadings(t, registry(t, Registrations()), tc.header+tc.body, s, engine.DocumentContext{Flavor: flavor, Frontmatter: fm}, engine.RunOptions{})
					if out.Complete == tc.incomplete || len(out.Diagnostics) != tc.count || len(out.Errors) != 0 {
						t.Fatalf("%+v", out)
					}
					if tc.incomplete && (len(out.Coverage) != 1 || out.Coverage[0].Rule != "MD041" || out.Coverage[0].Reason != result.ContextUnavailable) {
						t.Fatal(out.Coverage)
					}
				})
			}
		}
	}
}

func TestMD041MetadataNativeValues(t *testing.T) {
	for _, tc := range []struct {
		payload, delimiter string
		present            bool
	}{
		{`title: "\v\N\L\P"`, "---", false}, {`title: "\u200b"`, "---", true},
		{`title = "\u000B\u0085\u2028\u2029"`, "+++", false}, {`title = "\u200B"`, "+++", true},
		{"", "---", false}, {"other: Guide", "---", false}, {"Title: Guide", "---", false}, {"outer:\n  title: Guide", "---", false},
		{"title: null", "---", false}, {"title: true", "---", false}, {"title: 42", "---", false}, {"title: [Guide]", "---", false}, {"title: {key: Guide}", "---", false},
		{`title = ""`, "+++", false}, {"title = 42", "+++", false}, {"title = false", "+++", false}, {"title = [\"Guide\"]", "+++", false}, {"title = {key = \"Guide\"}", "+++", false}, {"[outer]\ntitle = \"Guide\"", "+++", false},
	} {
		for _, flavor := range []string{"commonmark", "gfm"} {
			s := md041Settings()
			s["MD041"] = engine.RuleSetting{Options: engine.Options{"frontmatter_title": true}}
			fm, _ := parser.NewFrontmatterOptions(true, nil)
			source := tc.delimiter + "\n" + tc.payload + "\n" + tc.delimiter + "\nIntro"
			out := analyzeHeadings(t, registry(t, Registrations()), source, s, engine.DocumentContext{Flavor: flavor, Frontmatter: fm}, engine.RunOptions{})
			want := 1
			if tc.present {
				want = 0
			}
			if !out.Complete || len(out.Diagnostics) != want || len(out.Errors) != 0 {
				t.Fatalf("%s %s %+v", flavor, tc.payload, out)
			}
		}
	}
}

func TestMD041CatalogAndOptions(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD041") || r.Available("MD043") || len(r.Metadata()) != 87 {
		t.Fatal("availability")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD041" && (!m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.NoFix || len(m.Options) != 2) {
			t.Fatal(m)
		}
	}
	for _, enabled := range []bool{false, true} {
		for _, v := range []any{0, 7, -1, 1.5, "1", true, nil, []any{}, map[string]any{}} {
			if _, err := r.ResolveSettings(engine.Settings{"MD041": {Enabled: &enabled, Options: engine.Options{"level": v}}}, engine.ValidationProviders{}); err == nil {
				t.Fatal("invalid level", v)
			}
		}
		for _, v := range []any{nil, "false", 0, []bool{true}} {
			if _, err := r.ResolveSettings(engine.Settings{"MD041": {Enabled: &enabled, Options: engine.Options{"frontmatter_title": v}}}, engine.ValidationProviders{}); err == nil {
				t.Fatal("invalid title", v)
			}
		}
		if _, err := r.ResolveSettings(engine.Settings{"MD041": {Enabled: &enabled, Options: engine.Options{"unknown": true}}}, engine.ValidationProviders{}); err == nil {
			t.Fatal("unknown option")
		}
	}
	for level := 1; level <= 6; level++ {
		if _, err := r.ResolveSettings(engine.Settings{"MD041": {Options: engine.Options{"level": level}}}, engine.ValidationProviders{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, mapping := range []map[string]any{{}, {"enabled": false}, {"frontmatter_title": true}, {"frontmatter_title": false}} {
		v, err := engine.SettingFromMapping(mapping)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.ResolveSettings(engine.Settings{"MD041": v}, engine.ValidationProviders{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := engine.SettingFromMapping(nil); err == nil {
		t.Fatal("null mapping")
	}
	s := md041Settings()
	delete(s, "MD041")
	out := analyzeHeadings(t, r, "Intro", s, engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 1 {
		t.Fatal(out)
	}
}

func TestMD041Directives(t *testing.T) {
	for _, tc := range []struct {
		name, source          string
		enabled, allowed      bool
		active, hidden, owner int
	}{
		{"last disable", "<!-- mdchk:disable MD041 -->\n<!-- mdchk:disable MD041 -->\nIntro", true, true, 0, 1, 1},
		{"configured enable", "<!-- mdchk:enable MD041 -->\nIntro", false, true, 1, 0, 0},
		{"off throughout", "Intro", false, true, 0, 0, 0},
		{"deny disable", "<!-- mdchk:disable MD041 -->\nIntro", true, false, 1, 0, 0},
		{"deny enable", "<!-- mdchk:enable MD041 -->\nIntro", false, false, 0, 0, 0},
		{"first hidden", "<!-- mdchk:disable MD041 -->\nIntro\n\n<!-- mdchk:enable MD041 -->\n# H", true, true, 0, 1, 0},
		{"before indent", "  Intro <!-- mdchk:disable MD041 -->", true, true, 1, 0, 0},
		{"EOF disabled", "<!-- mdchk:disable MD041 -->", true, true, 0, 1, 0},
		{"unclosed not directive", "<!-- mdchk:disable MD041", true, true, 1, 0, 0},
		{"protected fake", "`<!-- mdchk:disable MD041 -->`\n\n# H", true, true, 1, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := md041Settings()
			s["MD041"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allowed}
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, s, engine.DocumentContext{}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
				t.Fatal(out)
			}
			if tc.hidden > 0 {
				trace := out.AnalysisRecords()[0].(engine.DirectiveAnalysis)
				if trace.Suppressed()[0].Owner != tc.owner {
					t.Fatal(trace.Suppressed())
				}
			}
		})
	}
}

func TestMD041AddedWorkInterruptions(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		var observed error
		r := registry(t, []engine.Registration{{ID: "MD041", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			out := openingDocumentHeading(&traversalContext{Context: ctx, err: reason}, c)
			observed = out.Err
			return out
		}}})
		out := analyzeHeadings(t, r, "<!--a-->    \nIntro", md041Settings(), engine.DocumentContext{}, engine.RunOptions{})
		if !errors.Is(observed, reason) || out.Complete || len(out.Interruptions) != 1 || len(out.Errors) != 0 {
			t.Fatal(observed, out)
		}
	}
	for _, kind := range []string{"work", "nodes"} {
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		r := registry(t, []engine.Registration{{ID: "MD041", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			if kind == "work" {
				_ = budget.ChargeWork(budget.Limits().WorkUnits)
			} else {
				_ = budget.ChargeNodes(budget.Limits().TotalNodes)
			}
			return openingDocumentHeading(ctx, c)
		}}})
		out := analyzeHeadings(t, r, "Intro", md041Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
		if out.Complete || len(out.Interruptions) != 1 || len(out.Errors) != 0 {
			t.Fatal(out)
		}
	}
}

func TestMD041DemandDrivenTitleOriginalAllowance(t *testing.T) {
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	run := func(source string, nodes int64, title bool) result.Result {
		limits := parser.DefaultLimits()
		limits.DocumentNodes = nodes
		budget, _ := parser.NewOperationBudget(limits)
		s := md041Settings()
		s["MD041"] = engine.RuleSetting{Options: engine.Options{"frontmatter_title": title}}
		return analyzeHeadings(t, registry(t, Registrations()), source, s, engine.DocumentContext{Frontmatter: fm}, engine.RunOptions{Budget: budget})
	}
	for _, body := range []string{"Intro", "# A"} {
		source := "---\ntitle: Guide\n---\n" + body
		low, high := int64(1), int64(10000)
		for low < high {
			n := (low + high) / 2
			if run(source, n, false).Complete {
				high = n
			} else {
				low = n + 1
			}
		}
		base := run(source, low, false)
		opted := run(source, low, true)
		enough := run(source, 10000, true)
		if !base.Complete || !enough.Complete || len(enough.Diagnostics) != 0 {
			t.Fatal(base, enough)
		}
		if body == "# A" {
			if !opted.Complete || len(opted.Diagnostics) != 0 {
				t.Fatal("satisfied opener decoded metadata", opted)
			}
		} else {
			if len(base.Diagnostics) != 1 || opted.Complete || len(opted.Interruptions) != 1 || opted.Interruptions[0].Limit == nil || opted.Interruptions[0].Limit.Name != "document-nodes" {
				t.Fatal("title reset original allowance", base, opted)
			}
		}
	}
}

func TestMD041LateOperationRetainsReliablePoint(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			var operation context.Context
			var cancel context.CancelFunc
			if deadline {
				operation, cancel = context.WithTimeout(context.Background(), time.Millisecond)
			} else {
				operation, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			seen := false
			r := registry(t, []engine.Registration{{ID: "MD041", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				out := openingDocumentHeading(ctx, c)
				if out.Err != nil || len(out.Findings) != 1 {
					t.Fatal(out)
				}
				seen = true
				if deadline {
					time.Sleep(2 * time.Millisecond)
				} else {
					cancel()
				}
				return out
			}}})
			in, err := engine.NewInput("point.md", []byte("Intro"), md041Settings(), engine.DocumentContext{})
			if err != nil {
				t.Fatal(err)
			}
			out, err := r.Lint(operation, []engine.Input{in}, engine.RunOptions{})
			if err != nil || !seen || out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange.Start != 0 || out.Diagnostics[0].ByteRange.End != 0 || len(out.Interruptions) != 1 || len(out.Errors) != 0 {
				t.Fatal(err, out)
			}
		})
	}
}
