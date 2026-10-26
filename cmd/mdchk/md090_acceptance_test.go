package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

type md090Context struct {
	Slides      bool     `json:"slides"`
	Patterns    []string `json:"matched_slide_patterns"`
	Frontmatter string   `json:"frontmatter"`
	Match       *struct {
		Pattern string `json:"pattern"`
		Index   int    `json:"index"`
		Origin  struct {
			Path     string `json:"path"`
			Property string `json:"property_path"`
			Position result.Position
		} `json:"origin"`
	} `json:"slide_match"`
}

func md090Configuration(flavor string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD090: {enabled: false}", "MD090: {enabled: true}", 1)
}
func TestCompiledPlatformMD090ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	data, err := os.ReadFile("../../internal/rules/testdata/md090.json")
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
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, f := range fixtures {
				t.Run(f.Name, func(t *testing.T) {
					root := configurationProject(t)
					conf := md090Configuration(flavor)
					if f.Recognize {
						conf += "frontmatter: {enabled: true}\n"
					}
					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					putConfigurationFile(t, root, "doc.md", f.Source)
					before := configurationBytes(t, root)
					for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
						got := runConfigurationProcess(t, binary, root, args...)
						out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
						status := 0
						if len(f.Expected) > 0 {
							status = 10
						}
						if f.Incomplete {
							status = 15
						}
						if got.Status != status || out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Diagnostics) != len(f.Expected) || len(out.Changes) != 0 || len(out.Documents) != 0 {
							t.Fatal(got)
						}
						for i, w := range f.Expected {
							d := out.Diagnostics[i]
							actual := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
							if !reflect.DeepEqual(actual, w.Range) || d.Rule != "MD090" || d.FixAvailable || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source {
								t.Fatal(d, w)
							}
						}
					}
					got, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
					if got.Status != 0 {
						t.Fatal(got)
					}
					if p.DocumentContext.Data == nil || p.DocumentContext.Availability != "available" {
						t.Fatal(p.DocumentContext)
					}
					var dc md090Context
					if err := json.Unmarshal(*p.DocumentContext.Data, &dc); err != nil {
						t.Fatal(err)
					}
					wantFM := "disabled"
					if f.Recognize {
						wantFM = "yaml"
						if f.Name == "toml header" {
							wantFM = "toml"
						}
						if f.Incomplete {
							wantFM = "unterminated"
						}
					}
					if dc.Slides || len(dc.Patterns) != 0 || dc.Match != nil || dc.Frontmatter != wantFM {
						t.Fatal(dc, wantFM)
					}
					if !f.Incomplete {
						found := false
						if p.Rules.Data != nil {
							for _, r := range *p.Rules.Data {
								if r.Rule == "MD090" {
									found = r.Implementation == "available" && r.ActiveEvaluation == "complete"
								}
							}
						}
						if !found {
							t.Fatal(p.Rules)
						}
					}
					if !reflect.DeepEqual(before, configurationBytes(t, root)) {
						t.Fatal("read-only analysis changed bytes")
					}
				})
			}
		})
	}
}

func TestCompiledPlatformMD090SlideContext(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, tc := range []struct {
				name, target, patterns   string
				allow, slides            bool
				pattern                  string
				index                    int
				unreliable, off, mapping bool
			}{
				{"matched default", "slides/doc.md", "[slides/**]", true, true, "slides/**", 0, false, false, false},
				{"matched checked", "slides/doc.md", "[slides/**]", false, true, "slides/**", 0, false, false, false},
				{"unmatched", "doc.md", "[slides/**]", true, false, "", 0, false, false, false},
				{"case sensitive", "SLIDES/doc.md", "[slides/**]", true, false, "", 0, false, false, false},
				{"directory only", "slides/doc.md", "[slides/]", true, false, "", 0, false, false, false},
				{"first match", "slides/doc.md", "[nowhere/**, slides/**, '**']", true, true, "slides/**", 1, false, false, false},
				{"globstar", "doc.md", "['**']", true, true, "**", 0, false, false, false},
				{"suffix explicit", "deck.slides.md", "['**/*.slides.md']", true, true, "**/*.slides.md", 0, false, false, false},
				{"suffix not implicit", "deck.slides.md", "[]", true, false, "", 0, false, false, false},
				{"disabled", "slides/doc.md", "[slides/**]", true, true, "slides/**", 0, false, true, false},
				{"empty mapping", "slides/doc.md", "[slides/**]", true, true, "slides/**", 0, false, false, true},
				{"unreliable exempt", "slides/doc.md", "[slides/**]", true, true, "slides/**", 0, true, false, false},
				{"unreliable checked", "slides/doc.md", "[slides/**]", false, true, "slides/**", 0, true, false, false},
				{"unreliable unmatched", "doc.md", "[slides/**]", true, false, "", 0, true, false, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					root := configurationProject(t)
					conf := md090Configuration(flavor)
					if tc.name != "matched default" {
						conf = strings.Replace(conf, "MD090: {enabled: true}", fmt.Sprintf("MD090: {enabled: true, allow_slides: %t}", tc.allow), 1)
					}
					if tc.off {
						conf = strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
					}
					if tc.mapping {
						conf = strings.Replace(conf, fmt.Sprintf("MD090: {enabled: true, allow_slides: %t}", tc.allow), "MD090: {}", 1)
					}
					conf += "documents: {slides: " + tc.patterns + "}\n"
					source := "***\n# H"
					if tc.unreliable {
						conf += "frontmatter: {enabled: true}\n"
						source = "---\n# H"
					}
					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					putConfigurationFile(t, root, tc.target, source)
					before := configurationBytes(t, root)
					want := 1
					if tc.slides && tc.allow || tc.off || tc.mapping || tc.unreliable {
						want = 0
					}
					status := 0
					if want > 0 {
						status = 10
					}
					if tc.unreliable {
						status = 15
					}
					got := runConfigurationProcess(t, binary, root, "lint", tc.target, "--format=json")
					out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
					if got.Status != status || out.Complete == tc.unreliable || len(out.Errors) != 0 || len(out.Diagnostics) != want {
						t.Fatal(got)
					}
					inspect, p := runInspection(t, binary, root, root, "inspect", tc.target, "--format=json")
					if inspect.Status != 0 || p.DocumentContext.Availability != "available" || p.DocumentContext.Data == nil {
						t.Fatal(inspect, p.DocumentContext)
					}
					var dc md090Context
					if err := json.Unmarshal(*p.DocumentContext.Data, &dc); err != nil {
						t.Fatal(err)
					}
					if dc.Slides != tc.slides || len(dc.Patterns) != 0 && !tc.slides || tc.slides && (len(dc.Patterns) != 1 || dc.Patterns[0] != tc.pattern || dc.Match == nil || dc.Match.Pattern != tc.pattern || dc.Match.Index != tc.index || dc.Match.Origin.Path != ".mdchk.yaml" || dc.Match.Origin.Property != fmt.Sprintf("documents.slides[%d]", tc.index) || dc.Match.Origin.Position.Line < 1) {
						t.Fatal(dc, tc)
					}
					if tc.slides {
						assertMD090OriginPositionWire(t, *p.DocumentContext.Data)
					}
					if tc.unreliable && tc.slides && tc.allow {
						if p.Rules.Availability != "partial" || p.Rules.Data == nil {
							t.Fatal(p.Rules)
						}
						found := false
						for _, r := range *p.Rules.Data {
							if r.Rule == "MD090" {
								found = reflect.DeepEqual(r.Reasons, []string{"inapplicable"}) && r.ActiveEvaluation == "skipped"
							}
						}
						if !found || dc.Frontmatter != "unterminated" {
							t.Fatal(p.Rules, dc)
						}
					}
					if !reflect.DeepEqual(before, configurationBytes(t, root)) {
						t.Fatal("annotation changed files")
					}
				})
			}
			for _, bad := range []string{"documents: {slides: ['**', '[']}\n", "documents: {slides: null}\n", "documents: {slides: 'a.md'}\n", "documents: {slides: [1]}\n", "rules: {MD090: {enabled: false, allow_slides: null}}\n", "rules: {MD090: {enabled: false, allow_slides: 'true'}}\n", "rules: {MD090: {enabled: false, unknown: true}}\n"} {
				root := configurationProject(t)
				conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				if strings.HasPrefix(bad, "rules:") {
					conf = strings.Replace(conf, "MD090: {enabled: false}", strings.TrimSuffix(strings.TrimPrefix(bad, "rules: {"), "}\n"), 1)
				} else {
					conf += bad
				}
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
				if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 {
					t.Fatal(got)
				}
			}
		})
	}
}

func TestCompiledPlatformMD090ExplicitBaseAndComposition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, matched := range []bool{false, true} {
			for _, allow := range []bool{false, true} {
				for _, ending := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}, {"CR", "\r"}} {
					t.Run(fmt.Sprintf("%s/%t/%t/%s", flavor, matched, allow, ending.name), func(t *testing.T) {
						root := configurationProject(t)
						conf := md090Configuration(flavor)
						conf = strings.Replace(conf, "MD090: {enabled: true}", fmt.Sprintf("MD090: {enabled: true, allow_slides: %t}", allow), 1)
						conf = strings.Replace(conf, "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
						conf += "documents: {slides: [slides/**]}\n"
						putConfigurationFile(t, root, "policy/.mdchk.yaml", conf)
						target := "outside/doc.md"
						if matched {
							target = "policy/slides/doc.md"
						}
						original := "***" + ending.text + "# H"
						candidate := original + ending.text
						digest := fmt.Sprintf("%x", sha256.Sum256([]byte(candidate)))
						want := 1
						if matched && allow {
							want = 0
						}
						status := 0
						if want > 0 {
							status = 10
						}
						for pass, source := range []string{original, candidate} {
							putConfigurationFile(t, root, target, source)
							before := configurationBytes(t, root)
							got := runConfigurationProcess(t, binary, root, "fix", "--dry-run", target, "--config", "policy/.mdchk.yaml", "--format=json")
							out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
							if got.Status != status || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != want || len(out.Changes) != 1-pass || len(out.Documents) != 0 || len(out.Files) != 1 {
								t.Fatal(got)
							}
							snap := out.Files[0].ResultSnapshot
							if snap == nil || snap.SHA256 != digest || snap.SizeBytes != len(candidate) {
								t.Fatal(out.Files)
							}
							if want == 1 {
								d := out.Diagnostics[0]
								if d.Rule != "MD090" || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: 0, End: 3}) || d.Range.Start != (result.Position{Line: 1, Column: 1}) || d.Range.End != (result.Position{Line: 1, Column: 4}) || d.Snapshot != *snap {
									t.Fatal(d)
								}
							}
							if pass == 0 {
								c := out.Changes[0]
								if !reflect.DeepEqual(c.Rules, []string{"MD047"}) || c.EditCount != 1 || c.Candidate != *snap {
									t.Fatal(c)
								}
							}
							if !reflect.DeepEqual(before, configurationBytes(t, root)) {
								t.Fatal("preview wrote files")
							}
						}
						inspected, p := runInspection(t, binary, root, root, "inspect", target, "--config", "policy/.mdchk.yaml", "--format=json")
						if inspected.Status != 0 || p.DocumentContext.Data == nil {
							t.Fatal(inspected)
						}
						var dc md090Context
						if err := json.Unmarshal(*p.DocumentContext.Data, &dc); err != nil || dc.Slides != matched {
							t.Fatal(dc, err)
						}
						// Cwd below the selected config still maps ../ targets to that config base.
						if matched {
							got := runConfigurationProcess(t, binary, filepath.Join(root, "policy", "slides"), "lint", "doc.md", "--config", "../.mdchk.yaml", "--format=json")
							if got.Status != status {
								t.Fatal(got)
							}
						}
					})
				}
			}
		}
	}
}

func TestCompiledPlatformMD090Routing(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor+"/mixed_selection", func(t *testing.T) {
			root := configurationProject(t)
			conf := md090Configuration(flavor) + "documents: {slides: [slides/**, excluded/**]}\nexclude: [excluded/**]\n"
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			for _, name := range []string{"slides/a.md", "ordinary/b.md", "excluded/c.md"} {
				putConfigurationFile(t, root, name, "***\n# H\n")
			}
			before := configurationBytes(t, root)
			for _, paths := range [][]string{{"slides", "ordinary", "excluded"}, {"ordinary", "excluded", "slides"}} {
				args := append([]string{"lint"}, paths...)
				args = append(args, "--format=json")
				got := runConfigurationProcess(t, binary, root, args...)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
				if got.Status != 10 || !out.Complete || len(out.CheckedFiles) != 2 || len(out.Diagnostics) != 1 || out.Diagnostics[0].Path != "ordinary/b.md" || len(out.SkippedFiles) == 0 {
					t.Fatal(got)
				}
			}
			if !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("annotation changed selection/files")
			}
		})
		t.Run(flavor+"/outside_globstar", func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, "policy/.mdchk.yaml", md090Configuration(flavor)+"documents: {slides: ['**']}\n")
			putConfigurationFile(t, root, "outside.md", "***\n# H\n")
			got := runConfigurationProcess(t, binary, root, "lint", "outside.md", "--config", "policy/.mdchk.yaml", "--format=json")
			if got.Status != 10 {
				t.Fatal(got)
			}
			inspected, p := runInspection(t, binary, root, root, "inspect", "outside.md", "--config", "policy/.mdchk.yaml", "--format=json")
			if inspected.Status != 0 || p.DocumentContext.Data == nil {
				t.Fatal(inspected)
			}
			var dc md090Context
			if err := json.Unmarshal(*p.DocumentContext.Data, &dc); err != nil || dc.Slides {
				t.Fatal(dc, err)
			}
		})
		t.Run(flavor+"/nested_discovery", func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md090Configuration(flavor)+"documents: {slides: []}\n")
			putConfigurationFile(t, root, "nested/.mdchk.yaml", md090Configuration(flavor)+"documents: {slides: ['**']}\n")
			putConfigurationFile(t, root, "nested/doc.md", "***\n# H\n")
			got := runConfigurationProcess(t, binary, root, "lint", "nested/doc.md", "--format=json")
			if got.Status != 10 {
				t.Fatal(got)
			}
			inspected, p := runInspection(t, binary, root, root, "inspect", "nested/doc.md", "--format=json")
			if inspected.Status != 0 || p.DocumentContext.Data == nil {
				t.Fatal(inspected)
			}
			var dc md090Context
			if err := json.Unmarshal(*p.DocumentContext.Data, &dc); err != nil || !dc.Slides || dc.Match == nil || dc.Match.Origin.Path != "nested/.mdchk.yaml" {
				t.Fatal(dc, err)
			}
			// Explicit root config overrides inspect's independent nested discovery.
			inspected, p = runInspection(t, binary, root, root, "inspect", "nested/doc.md", "--config", ".mdchk.yaml", "--format=json")
			if inspected.Status != 0 || p.DocumentContext.Data == nil {
				t.Fatal(inspected)
			}
			if err := json.Unmarshal(*p.DocumentContext.Data, &dc); err != nil || dc.Slides {
				t.Fatal(dc, err)
			}
		})
	}
}

// Inspect the actual JSON keys: Go's case-insensitive field matching alone cannot
// prove conformance to the lower-case Position contract.
func assertMD090OriginPositionWire(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var data map[string]json.RawMessage
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"slide_match", "origin", "position"} {
		next := map[string]json.RawMessage{}
		if err := json.Unmarshal(data[field], &next); err != nil {
			t.Fatal(field, err, string(raw))
		}
		data = next
	}
	if len(data) != 2 || len(data["line"]) == 0 || len(data["column"]) == 0 || len(data["Line"]) != 0 || len(data["Column"]) != 0 {
		t.Fatal("origin position does not match lower-case wire contract", string(raw))
	}
}
