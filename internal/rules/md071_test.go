package rules

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

const md071Message = "Frontmatter requires a blank line before the document body."

func md071Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD071"] = engine.RuleSetting{Enabled: &on}
	return s
}

func md071Context(t *testing.T, flavor string, recognize bool, formats []string) engine.DocumentContext {
	t.Helper()
	fm, err := parser.NewFrontmatterOptions(recognize, formats)
	if err != nil {
		t.Fatal(err)
	}
	return engine.DocumentContext{Flavor: flavor, Frontmatter: fm}
}

func TestMD071AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md071.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source          string
		Recognize, Incomplete bool
		Formats               []string
		SHA256                string
		SizeBytes             int

		Expected []struct {
			Range   []int
			Source  string
			Message string
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	r := registry(t, Registrations())
	for _, f := range fixtures {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(f.Name+"/"+flavor, func(t *testing.T) {
				dc := md071Context(t, flavor, f.Recognize, f.Formats)
				out := analyzeHeadings(t, r, f.Source, md071Settings(), dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, md071Settings(), dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) || out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(f.Expected) {
					t.Fatal("outcome", out.Complete, out.Diagnostics, out.Errors, out.Interruptions, "expected", f.Expected)
				}
				snapshot := out.Files[0].InputSnapshot
				if snapshot == nil || snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source))) || snapshot.SizeBytes != f.SizeBytes || snapshot.SHA256 != f.SHA256 {
					t.Fatal(out.Files)
				}
				// An unterminated header stops ordinary analysis before rule
				// dispatch; the engine's file-level parser-uncertain record covers it.
				uncertain := false
				for _, coverage := range out.Coverage {
					if coverage.Rule == "MD071" && coverage.Reason != result.ParserUncertain {
						t.Fatal(coverage)
					}
					uncertain = uncertain || coverage.Reason == result.ParserUncertain
				}
				if uncertain != f.Incomplete || (!f.Incomplete && len(out.Coverage) != 0) {
					t.Fatal(out.Coverage)
				}
				for i, w := range f.Expected {
					d := out.Diagnostics[i]
					got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if !reflect.DeepEqual(got, w.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source || d.Rule != "MD071" || d.FixAvailable || d.Message != w.Message || w.Message != md071Message {
						t.Fatal(got, d, w)
					}
				}
			})
		}
	}
}

func TestMD071CatalogAndStrictSettings(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD071") || r.Available("MD066") || len(r.Metadata()) != 87 {
		t.Fatal("catalog")
	}
	found := false
	for _, m := range r.Metadata() {
		if m.ID == "MD071" {
			found = true
			if !m.DefaultEnabled || m.Fix != engine.PlannedFix || !m.RequiresSyntax || !m.UsesFrontmatter || len(m.Options) != 0 || m.AppliesTo != nil {
				t.Fatal(m)
			}
		}
	}
	if !found {
		t.Fatal("MD071 metadata")
	}
	for _, mapping := range []map[string]any{{"unknown": true}, {"enabled": false, "unknown": true}, {"enabled": nil}, {"enabled": "false"}, {"allow_directives": nil}, {"enabled": false, "allow_directives": nil}, {"allow_directives": 1}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD071": setting}, engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal("accepted", mapping)
		}
	}
	for _, mapping := range []map[string]any{{}, {"enabled": true}, {"enabled": false}, {"allow_directives": true}, {"allow_directives": false}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.ResolveSettings(engine.Settings{"MD071": setting}, engine.ValidationProviders{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMD071EnablementAndDirectives(t *testing.T) {
	r := registry(t, Registrations())
	for _, flavor := range []string{"commonmark", "gfm"} {
		dc := md071Context(t, flavor, true, nil)
		t.Run(flavor+"/disabled", func(t *testing.T) {
			s := md071Settings()
			off := false
			s["MD071"] = engine.RuleSetting{Enabled: &off}
			out := analyzeHeadings(t, r, "---\ntitle: Guide\n---\n# Guide\n", s, dc, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != 0 || len(out.Coverage) != 0 {
				t.Fatal(out)
			}
		})
		t.Run(flavor+"/omitted-default", func(t *testing.T) {
			s := md071Settings()
			delete(s, "MD071")
			out := analyzeHeadings(t, r, "---\ntitle: Guide\n---\n# Guide\n", s, dc, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD071" {
				t.Fatal(out)
			}
		})
		// Transitions begin after the comment's closing marker, so the finding
		// anchor at the start of the comment line remains active.
		t.Run(flavor+"/body-directive", func(t *testing.T) {
			source := "---\ntitle: Guide\n---\n<!-- mdchk:disable MD071 -->\n# Guide\n"
			out := analyzeHeadings(t, r, source, md071Settings(), dc, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != 1 || len(out.SuppressedDiagnostics) != 0 {
				t.Fatal(out)
			}
			d := out.Diagnostics[0]
			if d.Rule != "MD071" || d.ByteRange != (result.ByteRange{Start: 21, End: 21}) || d.Range.Start != (result.Position{Line: 4, Column: 1}) || d.Message != md071Message {
				t.Fatal(d)
			}
		})
	}
}

func TestMD071InapplicableWithoutRecognition(t *testing.T) {
	r := registry(t, Registrations())
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, recognize := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", flavor, recognize), func(t *testing.T) {
				selected, err := r.SelectRules(md071Settings(), engine.ValidationProviders{})
				if err != nil {
					t.Fatal(err)
				}
				plan, err := selected.Plan(md071Context(t, flavor, recognize, nil), nil)
				if err != nil {
					t.Fatal(err)
				}
				in, err := plan.NewInput("doc.md", []byte("---\ntitle: Guide\n---\n# Guide\n"))
				if err != nil {
					t.Fatal(err)
				}
				out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{})
				want := 0
				if recognize {
					want = 1
				}
				if err != nil || !out.Complete || len(out.Diagnostics) != want || len(out.Coverage) != 0 {
					t.Fatal(out, err)
				}
				var reasons []string
				for _, e := range trace.Evaluations() {
					if e.Rule == "MD071" {
						reasons = e.Reasons
					}
				}
				if inapplicable := reflect.DeepEqual(reasons, []string{"inapplicable"}); inapplicable == recognize {
					t.Fatal(reasons)
				}
			})
		}
	}
}
