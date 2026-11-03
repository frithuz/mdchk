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

type md045Literal struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Frontmatter  string
	Decorative   bool `json:"allow_decorative"`
	Findings     []md045Expected
	Problems     []md045Expected
}
type md045Expected struct {
	Bytes, Start, End [2]int
	Expected, Message string
}

func md045Literals(t *testing.T) []md045Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md045.json")
	if e != nil {
		t.Fatal(e)
	}
	var v struct{ Cases []md045Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) == 0 {
		t.Fatal(e)
	}
	return v.Cases
}
func md045Settings() engine.Settings {
	s := settings()
	for id := range s {
		off := false
		s[id] = engine.RuleSetting{Enabled: &off}
	}
	on := true
	s["MD045"] = engine.RuleSetting{Enabled: &on}
	return s
}
func TestMD045AnalysisFixtures(t *testing.T) {
	for _, tc := range md045Literals(t) {
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
					t.Fatal("source identity")
				}
				dc := engine.DocumentContext{Flavor: flavor}
				if tc.Frontmatter != "" {
					var e error
					dc.Frontmatter, e = parser.NewFrontmatterOptions(true, []string{tc.Frontmatter})
					if e != nil {
						t.Fatal(e)
					}
				}
				selection, e := registry(t, Registrations()).SelectRules(md045FixtureSettings(tc.Decorative), engine.ValidationProviders{})
				if e != nil {
					t.Fatal(e)
				}
				plan, e := selection.Plan(dc, nil)
				if e != nil {
					t.Fatal(e)
				}
				original := []byte(tc.Source)
				input, e := plan.NewInput("literal.md", original)
				if e != nil {
					t.Fatal(e)
				}
				var previous []result.Diagnostic
				for pass := 0; pass < 2; pass++ {
					out, trace, e := plan.Inspect(context.Background(), input, engine.RunOptions{}, engine.InspectionOptions{})
					if e != nil || out.Complete != (len(tc.Problems) == 0) || len(out.Errors)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes)+len(out.SuppressedDiagnostics) != 0 || len(out.Coverage) != len(tc.Problems) || len(out.Notes) != len(tc.Problems) || len(out.Diagnostics) != len(tc.Findings) || string(original) != tc.Source || string(input.Source().Bytes()) != tc.Source {
						t.Fatal(out, e, tc.Findings)
					}
					if len(out.Files) != 1 || out.Files[0].WriteStatus != "not-requested" {
						t.Fatal(out.Files)
					}
					if pass == 1 && !reflect.DeepEqual(previous, out.Diagnostics) {
						t.Fatal("non-deterministic findings")
					}
					previous = out.Diagnostics
					for i, d := range out.Diagnostics {
						w := tc.Findings[i]
						if d.Rule != "MD045" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range != (result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}) || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}) || tc.Source[w.Bytes[0]:w.Bytes[1]] != w.Expected {
							t.Fatal(d, w)
						}
					}
					for i, w := range tc.Problems {
						var note result.Note
						if e := json.Unmarshal(out.Notes[i], &note); e != nil {
							t.Fatal(e)
						}
						rangeWant := result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}
						if note.Rule != "MD045" || note.Code != "rule-parser-uncertain" || note.Message != w.Message || note.ByteRange == nil || *note.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || note.Range == nil || *note.Range != rangeWant || note.Snapshot == nil || note.Snapshot.SHA256 != tc.SHA {
							t.Fatal(note, w)
						}
						coverage := out.Coverage[i]
						if coverage.Rule != "MD045" || coverage.Reason != result.ParserUncertain || coverage.Range == nil || *coverage.Range != rangeWant || coverage.Snapshot == nil || coverage.Snapshot.SHA256 != tc.SHA {
							t.Fatal(coverage, w)
						}
					}
					found := false
					for _, ev := range trace.Evaluations() {
						if ev.Rule == "MD045" {
							found = true
							wantState := "complete"
							if len(tc.Problems) > 0 {
								wantState = "partial"
							}
							if ev.Active != wantState {
								t.Fatal(ev)
							}
						}
					}
					if !found {
						t.Fatal("evaluation missing")
					}
				}
			})
		}
	}
}

func md045FixtureSettings(decorative bool) engine.Settings {
	s := md045Settings()
	on := true
	s["MD045"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"allow_decorative": decorative}}
	return s
}
func TestMD045DirectivesAndInactiveObservation(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"suppressed", "denied", "inactive", "enable", "enable-denied", "protected"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				s := md045Settings()
				enabled := mode != "inactive" && mode != "enable" && mode != "enable-denied"
				allow := mode != "denied" && mode != "enable-denied"
				s["MD045"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow}
				source := "<!-- mdchk:disable MD045 -->\n![](x)"
				if mode == "inactive" {
					source = "![](x)"
				}
				if mode == "enable" || mode == "enable-denied" {
					source = "<!-- mdchk:enable MD045 -->\n![](x)"
				}
				if mode == "protected" {
					source = "```md\n<!-- mdchk:disable MD045 -->\n![](x)\n```\n\n![](x)"
				}
				selection, e := registry(t, Registrations()).SelectRules(s, engine.ValidationProviders{})
				if e != nil {
					t.Fatal(e)
				}
				plan, e := selection.Plan(engine.DocumentContext{Flavor: flavor}, nil)
				if e != nil {
					t.Fatal(e)
				}
				in, e := plan.NewInput("literal.md", []byte(source))
				if e != nil {
					t.Fatal(e)
				}
				out, trace, e := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{ObserveInactive: mode == "inactive"})
				wantVisible := 0
				if mode == "denied" || mode == "enable" || mode == "protected" {
					wantVisible = 1
				}
				if e != nil || !out.Complete || len(out.Diagnostics) != wantVisible || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents) != 0 {
					t.Fatal(out, e)
				}
				wantHidden := 0
				if mode == "suppressed" {
					wantHidden = 1
				}
				if len(out.SuppressedDiagnostics) != wantHidden {
					t.Fatal(out)
				}
				if mode == "inactive" {
					found := 0
					for _, v := range trace.Suppressed() {
						if v.Diagnostic.Rule == "MD045" {
							found++
							if v.Diagnostic.ByteRange != (result.ByteRange{Start: 2, End: 2}) {
								t.Fatal(v)
							}
						}
					}
					if found != 1 {
						t.Fatal(trace.Suppressed())
					}
				}
				if mode == "protected" && len(trace.Directives().Candidates()) != 0 {
					t.Fatal("code directive executed")
				}
			})
		}
	}
}
