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

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func md025Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD025"] = engine.RuleSetting{Enabled: &on}
	return s
}

func TestMD025AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md025.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source          string
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
				settings := md025Settings()
				settings["MD025"] = engine.RuleSetting{Options: engine.Options{"level": 1}}
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
					if !reflect.DeepEqual(got, want.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != want.Source || d.Rule != "MD025" || d.Message != "Document must not contain multiple titles." {
						t.Fatalf("got %v %+v, want %+v", got, d, want)
					}
				}
			})
		}
	}
}

func TestMD025AddedWorkInterruptions(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		var observed error
		r := registry(t, []engine.Registration{{ID: "MD025", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			out := multipleDocumentTitles(&traversalContext{Context: ctx, err: reason}, c)
			observed = out.Err
			return out
		}}})
		out := analyzeHeadings(t, r, " # H\n\n # H\n\n # H", md025Settings(), engine.DocumentContext{}, engine.RunOptions{})
		if !errors.Is(observed, reason) || out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "analysis" || len(out.Errors) != 0 {
			t.Fatal(observed, out)
		}
	}
	for _, kind := range []string{"work", "nodes"} {
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		r := registry(t, []engine.Registration{{ID: "MD025", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			if kind == "work" {
				_ = budget.ChargeWork(budget.Limits().WorkUnits)
			} else {
				_ = budget.ChargeNodes(budget.Limits().TotalNodes)
			}
			return multipleDocumentTitles(ctx, c)
		}}})
		out := analyzeHeadings(t, r, " # H\n\n # H\n\n # H", md025Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
		if out.Complete || len(out.Interruptions) != 1 || out.Interruptions[0].Phase != "analysis" || out.Interruptions[0].Limit == nil || len(out.Errors) != 0 {
			t.Fatal(out)
		}
	}
}

func TestMD025LateInterruptionRetainsReliableMarkers(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, source := range []string{" # H\n\n # H\n\n # H", " # H\n\n # H\n\n # H\n\n # H"} {
			calls := 0
			baseline := registry(t, []engine.Registration{{ID: "MD025", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				check := &md018LateContext{Context: ctx, reason: reason}
				out := multipleDocumentTitles(check, c)
				calls = check.calls
				return out
			}}})
			full := analyzeHeadings(t, baseline, source, md025Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if !full.Complete || len(full.Diagnostics) < 2 {
				t.Fatal(full.Diagnostics)
			}
			// Discover stops from the deterministic caller checkpoint sequence and require
			// every possible retained prefix, including the prefix before the final heading.
			retained := map[int]bool{}
			for stop := 1; stop <= calls; stop++ {
				var observed engine.RuleOutcome
				var token parser.SourceToken
				r := registry(t, []engine.Registration{{ID: "MD025", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					token = c.Source().Token()
					observed = multipleDocumentTitles(&md018LateContext{Context: ctx, reason: reason, stop: stop}, c)
					return observed
				}}})
				out := analyzeHeadings(t, r, source, md025Settings(), engine.DocumentContext{}, engine.RunOptions{})
				n := len(observed.Findings)
				if n == 0 {
					continue
				}
				if !errors.Is(observed.Err, reason) || out.Complete || len(out.Diagnostics) != n || len(out.Interruptions) != 1 || len(out.Coverage) == 0 || len(out.Errors) != 0 {
					t.Fatalf("stop %d: %+v %+v", stop, observed, out)
				}
				for i, f := range observed.Findings {
					if f.Span.SourceToken() != token || out.Diagnostics[i].ByteRange != full.Diagnostics[i].ByteRange || out.Diagnostics[i].Range != full.Diagnostics[i].Range {
						t.Fatal("prior marker/source lost", f, out.Diagnostics)
					}
				}
				retained[n] = true
			}
			for n := 1; n < len(full.Diagnostics); n++ {
				if !retained[n] {
					t.Fatalf("never retained %d reliable markers", n)
				}
			}
		}
	}
}

func TestMD025CatalogAndOptions(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD025") || r.Available("MD036") || len(r.Metadata()) != 87 {
		t.Fatal("incorrect availability")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD025" && (!m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.NoFix || len(m.Options) != 2) {
			t.Fatal(m)
		}
	}
	for _, value := range []any{nil, 1, "false", []any{}, map[string]any{}} {
		for _, enabled := range []bool{true, false} {
			_, err := r.ResolveSettings(engine.Settings{"MD025": {Enabled: &enabled, Options: engine.Options{"frontmatter_title": value}}}, engine.ValidationProviders{})
			if err == nil {
				t.Fatal("invalid siblings_only accepted", value, enabled)
			}
		}
	}
	for _, mapping := range []map[string]any{{"unknown": true}, {"enabled": false, "unknown": true}, {"enabled": "false"}, {"enabled": nil}, {"enabled": 1}, {"allow_directives": "false"}, {"allow_directives": nil}, {"enabled": false, "allow_directives": 1}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD025": setting}, engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal("invalid options accepted", mapping)
		}
	}
	for _, mapping := range []map[string]any{{}, {"enabled": false}, {"enabled": true, "allow_directives": false}, {"frontmatter_title": true}, {"frontmatter_title": false}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.ResolveSettings(engine.Settings{"MD025": setting}, engine.ValidationProviders{}); err != nil {
			t.Fatal(err)
		}
	}
	// Omission uses the catalog default, independently of explicit admission.
	s := md025Settings()
	delete(s, "MD025")
	out := analyzeHeadings(t, r, "# default\n# DEFAULT", s, engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 1 {
		t.Fatal(out)
	}
}

func TestMD025Directives(t *testing.T) {
	for _, tc := range []struct {
		name, source          string
		enabled, allowed      bool
		active, hidden, owner int
	}{
		{"last disable", "# A\n<!-- mdchk:disable MD025 -->\n<!-- mdchk:disable MD025 -->\n# a", true, true, 0, 1, 1},
		{"configured off then enable", "# A\n<!-- mdchk:enable MD025 -->\n# a", false, true, 1, 0, 0},
		{"hidden first keeps context", "<!-- mdchk:disable MD025 -->\n# A\n<!-- mdchk:enable MD025 -->\n# a", true, true, 1, 0, 0},
		{"off throughout", "# A\n# a", false, true, 0, 0, 0},
		{"disable denied", "# A\n<!-- mdchk:disable MD025 -->\n# a", true, false, 1, 0, 0},
		{"enable denied", "# A\n<!-- mdchk:enable MD025 -->\n# a", false, false, 0, 0, 0},
		{"label starts before inline disable", "# A\n# a <!-- mdchk:disable MD025 -->", true, true, 1, 0, 0},
		{"label starts with inline disable", "# A\n# <!-- mdchk:disable MD025 -->a", true, true, 1, 0, 0},
		{"Setext label starts before disable", "# A B\n\na\nb <!-- mdchk:disable MD025 -->\n===", true, true, 1, 0, 0},
		{"protected fake", "# A\n`<!-- mdchk:disable MD025 -->`\n\n# a", true, true, 1, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := md025Settings()
			s["MD025"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allowed}
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, s, engine.DocumentContext{}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
				t.Fatalf("active=%v suppressed=%v", out.Diagnostics, out.SuppressedDiagnostics)
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

func TestMD025ConfiguredLevelsAndMetadata(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for level := 1; level <= 6; level++ {
			for _, tc := range []struct {
				name, header      string
				title, incomplete bool
				count             int
			}{
				{"body", "", false, false, 2},
				{"yaml optout", "---\ntitle: Guide\n---\n", false, false, 2},
				{"yaml", "---\ntitle: Guide\n---\n", true, false, 3},
				{"toml", "+++\ntitle = \"Guide\"\n+++\n", true, false, 3},
				{"decoded key", "---\n\"ti\\u0074le\": !!str 42\n---\n", true, false, 3},
				{"toml decoded key", "+++\n\"ti\\u0074le\" = \"42\"\n+++\n", true, false, 3},
				{"positive partial", "---\ntitle: Guide\nother: !custom value\n---\n", true, false, 3},
				{"unknown duplicate", "---\ntitle: A\ntitle: B\n---\n", true, true, 2},
				{"unknown unsupported", "---\nother: !custom value\n---\n", true, true, 2},
				{"unknown invalid", "+++\ntitle = [\n+++\n", true, true, 2},
				{"unknown root", "---\n- Guide\n---\n", true, true, 2},
				{"optout invalid", "---\ntitle: [\n---\n", false, false, 2},
			} {
				t.Run(fmt.Sprintf("%s/%d/%s", flavor, level, tc.name), func(t *testing.T) {
					s := md025Settings()
					s["MD025"] = engine.RuleSetting{Options: engine.Options{"level": level, "frontmatter_title": tc.title}}
					fm, _ := parser.NewFrontmatterOptions(true, nil)
					marker := strings.Repeat("#", level)
					source := tc.header + marker + " First\n" + strings.Repeat("#", level%6+1) + " Other\n" + marker + " Second\n" + marker + " Third"
					out := analyzeHeadings(t, registry(t, Registrations()), source, s, engine.DocumentContext{Flavor: flavor, Frontmatter: fm}, engine.RunOptions{})
					if out.Complete == tc.incomplete || len(out.Diagnostics) != tc.count || len(out.Errors) != 0 || len(out.Interruptions) != 0 {
						t.Fatalf("metadata/count: %+v", out)
					}
					if tc.incomplete && (len(out.Coverage) != 1 || out.Coverage[0].Reason != result.ContextUnavailable) {
						t.Fatal(out.Coverage)
					}
					if tc.count == 2 && source[out.Diagnostics[0].ByteRange.Start:out.Diagnostics[0].ByteRange.End] != "Second" {
						t.Fatal("first body title guessed", out.Diagnostics)
					}
				})
			}
		}
	}
}

func TestMD025MetadataNativeValuesAndWhitespace(t *testing.T) {
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
			source := tc.delimiter + "\n" + tc.payload + "\n" + tc.delimiter + "\n# Body"
			s := md025Settings()
			s["MD025"] = engine.RuleSetting{Options: engine.Options{"frontmatter_title": true}}
			fm, _ := parser.NewFrontmatterOptions(true, nil)
			out := analyzeHeadings(t, registry(t, Registrations()), source, s, engine.DocumentContext{Flavor: flavor, Frontmatter: fm}, engine.RunOptions{})
			want := 0
			if tc.present {
				want = 1
			}
			if !out.Complete || len(out.Diagnostics) != want {
				t.Fatalf("%s %s: %+v", flavor, tc.payload, out)
			}
		}
	}
}

func TestMD025StrictLevelOptions(t *testing.T) {
	r := registry(t, Registrations())
	for _, v := range []any{0, 7, -1, 1.5, "1", true, nil, []any{}, map[string]any{}} {
		for _, enabled := range []bool{false, true} {
			if _, err := r.ResolveSettings(engine.Settings{"MD025": {Enabled: &enabled, Options: engine.Options{"level": v}}}, engine.ValidationProviders{}); err == nil {
				t.Fatal("invalid level", v, enabled)
			}
		}
	}
	for level := 1; level <= 6; level++ {
		if _, err := r.ResolveSettings(engine.Settings{"MD025": {Options: engine.Options{"level": level}}}, engine.ValidationProviders{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := engine.SettingFromMapping(nil); err == nil {
		t.Fatal("null mapping accepted")
	}
}
func TestMD025DefaultDoesNotDecodeMetadata(t *testing.T) {
	r := registry(t, Registrations())
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	dc := engine.DocumentContext{Frontmatter: fm}
	const source = "---\ntitle: Guide\n---\n### First\n### Later\n"
	// Establish the smallest document allowance at which the actual body-only
	// engine run completes. Opting into title consumption must share that original
	// allowance, while a sufficient allowance admits the configured-level title comparison.
	low, high := int64(1), int64(10000)
	run := func(nodes int64, title bool) result.Result {
		limits := parser.DefaultLimits()
		limits.DocumentNodes = nodes
		budget, err := parser.NewOperationBudget(limits)
		if err != nil {
			t.Fatal(err)
		}
		return analyzeHeadings(t, r, source, md025TitleSettings(title), dc, engine.RunOptions{Budget: budget})
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

func md025TitleSettings(title bool) engine.Settings {
	s := md025Settings()
	s["MD025"] = engine.RuleSetting{Options: engine.Options{"level": 3, "frontmatter_title": title}}
	return s
}

func TestMD025RecognitionAndUnknownFirstTitle(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source string
			recognize    bool
			formats      []string
			incomplete   bool
			count        int
		}{
			{"recognition off", "---\ntitle: Guide\n---\n# Body", false, nil, false, 0},
			{"format unselected", "+++\ntitle = \"Guide\"\n+++\n# Body", true, []string{"yaml"}, false, 0},
			{"no header", "# Body", true, nil, false, 0},
			{"header only", "---\ntitle: Guide\n---\n", true, nil, false, 0},
			{"unknown first body withheld", "---\ntitle: [\n---\n# Body", true, nil, true, 0},
			{"header fake directive opaque", "---\ntitle: Guide\n# <!-- mdchk:disable MD025 -->\n---\n# Body", true, nil, false, 1},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				fm, err := parser.NewFrontmatterOptions(tc.recognize, tc.formats)
				if err != nil {
					t.Fatal(err)
				}
				s := md025Settings()
				s["MD025"] = engine.RuleSetting{Options: engine.Options{"frontmatter_title": true}}
				out := analyzeHeadings(t, registry(t, Registrations()), tc.source, s, engine.DocumentContext{Flavor: flavor, Frontmatter: fm}, engine.RunOptions{})
				if out.Complete == tc.incomplete || len(out.Diagnostics) != tc.count || len(out.Errors) != 0 || len(out.Interruptions) != 0 {
					t.Fatal(out)
				}
				if tc.incomplete && (len(out.Coverage) != 1 || out.Coverage[0].Reason != result.ContextUnavailable) {
					t.Fatal(out.Coverage)
				}
			})
		}
	}
}
