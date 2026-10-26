package main

import (
	"crypto/sha256"
	"fmt"
	"github.com/frithuz/mdchk/internal/result"
	"reflect"
	"strings"
	"testing"
)

type md080NativeExpected struct {
	span       result.ByteRange
	start, end result.Position
}

func TestCompiledPlatformMD080ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, tc := range []struct {
				name, source, mode, style       string
				expected, gfmExpected           []md080NativeExpected
				gfmUnavailable, parserUncertain bool
			}{
				{"published", "## Hello!\n\n## Hello", "", "github", []md080NativeExpected{{result.ByteRange{Start: 14, End: 19}, result.Position{Line: 3, Column: 4}, result.Position{Line: 3, Column: 9}}}, nil, false, false},
				{"valid", "## Hello!\n\n## Welcome", "", "github", []md080NativeExpected{}, nil, false, false},
				{"every_later", "# A\n# A\n# A", "", "github", []md080NativeExpected{{result.ByteRange{Start: 6, End: 7}, result.Position{Line: 2, Column: 3}, result.Position{Line: 2, Column: 4}}, {result.ByteRange{Start: 10, End: 11}, result.Position{Line: 3, Column: 3}, result.Position{Line: 3, Column: 4}}}, nil, false, false},
				{"empty_label", "#\n#", "", "github", []md080NativeExpected{{result.ByteRange{Start: 3, End: 3}, result.Position{Line: 2, Column: 2}, result.Position{Line: 2, Column: 2}}}, nil, false, false},
				{"empty_base", "# !!!\n# ???", "", "github", []md080NativeExpected{{result.ByteRange{Start: 8, End: 11}, result.Position{Line: 2, Column: 3}, result.Position{Line: 2, Column: 6}}}, nil, false, false},
				{"nested", "> ## Hello\n\n- ### hello!", "", "github", []md080NativeExpected{{result.ByteRange{Start: 18, End: 24}, result.Position{Line: 3, Column: 7}, result.Position{Line: 3, Column: 13}}}, nil, false, false},
				{"html_allocation", "<div id=\"a\"></div>\n\n# A\n# A", "", "github", []md080NativeExpected{{result.ByteRange{Start: 26, End: 27}, result.Position{Line: 4, Column: 3}, result.Position{Line: 4, Column: 4}}}, nil, false, false},
				{"suffix_distinct", "# A\n# A-1", "", "github", []md080NativeExpected{}, nil, false, false},
				{"explicit", "# A\n# A", "", "explicit", []md080NativeExpected{}, nil, false, false},
				{"entity", "# A\n# &#65;", "", "github", []md080NativeExpected{{result.ByteRange{Start: 6, End: 11}, result.Position{Line: 2, Column: 3}, result.Position{Line: 2, Column: 8}}}, nil, false, false},
				{"unicode", "雪\r\n\r\n> # A\r\n\r\n- # A", "", "github", []md080NativeExpected{{result.ByteRange{Start: 20, End: 21}, result.Position{Line: 5, Column: 5}, result.Position{Line: 5, Column: 6}}}, nil, false, false},
				{"strike", "# ~~Gone~~\n# Gone", "", "github", []md080NativeExpected{}, []md080NativeExpected{{result.ByteRange{Start: 13, End: 17}, result.Position{Line: 2, Column: 3}, result.Position{Line: 2, Column: 7}}}, false, false},
				{"script_unknown", "# A\n# A\n\n<script id=\"x\">\n</script>", "", "github", []md080NativeExpected{{result.ByteRange{Start: 6, End: 7}, result.Position{Line: 2, Column: 3}, result.Position{Line: 2, Column: 4}}}, nil, true, false},
				{"explicit_unknown", "# A\n# A\n\n<script id=\"x\">\n</script>", "", "explicit", []md080NativeExpected{}, nil, true, false},
				{"unreliable", "---\n# A\n# A", "", "github", []md080NativeExpected{}, nil, false, true},
				{"disabled", "# A\n# A", "off", "github", nil, nil, false, false},
				{"empty_mapping", "# A\n# A", "mapping", "github", nil, nil, false, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					conf := strings.Replace(cleanConfiguration(), "MD080: {enabled: false}", "MD080: {enabled: true}", 1) + "frontmatter: {enabled: true}\n"
					if tc.mode == "off" {
						conf = strings.Replace(cleanConfiguration(), "MD080: {enabled: false}", "", 1)
					}
					if tc.mode == "mapping" {
						conf = strings.Replace(cleanConfiguration(), "MD080: {enabled: false}", "MD080: {}", 1)
					}
					conf = strings.Replace(conf, "flavor: commonmark", "flavor: "+flavor, 1) + "resources: {anchor_style: " + tc.style + "}\n"
					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					putConfigurationFile(t, root, "doc.md", tc.source)
					before := configurationBytes(t, root)
					want := tc.expected
					if flavor == "gfm" && tc.gfmExpected != nil {
						want = tc.gfmExpected
					}
					unavailable := flavor == "gfm" && tc.gfmUnavailable
					if unavailable {
						want = nil
					}
					incomplete := unavailable || tc.parserUncertain
					for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
						got := runConfigurationProcess(t, binary, root, args...)
						out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
						status := 0
						if len(want) > 0 {
							status = 10
						}
						if incomplete {
							status = 15
						}
						if got.Status != status || out.Complete == incomplete || len(out.Errors) != 0 || len(out.Diagnostics) != len(want) || len(out.Changes) != 0 || len(out.Documents) != 0 {
							t.Fatal(got)
						}
						for n, w := range want {
							d := out.Diagnostics[n]
							if d.Rule != "MD080" || d.FixAvailable || d.ByteRange != w.span || d.Range.Start != w.start || d.Range.End != w.end || d.Message != "Heading anchor base must not collide." {
								t.Fatal(d, w)
							}
						}
						if unavailable {
							found := false
							for _, coverage := range out.Coverage {
								found = found || coverage.Rule == "MD080" && coverage.Reason == result.ContextUnavailable
							}
							if !found {
								t.Fatal(out.Coverage)
							}
						}
					}
					got, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
					if got.Status != 0 {
						t.Fatal(got)
					}
					if p.Configuration.Data == nil || p.Configuration.Data.Effective["resources"].(map[string]any)["anchor_style"] != tc.style {
						t.Fatal(p.Configuration)
					}
					found := false
					if tc.parserUncertain {
						if p.Rules.Data != nil || p.Rules.Availability != "unavailable" {
							t.Fatal(p.Rules)
						}
						found = true
					} else if p.Rules.Data != nil {
						for _, rule := range *p.Rules.Data {
							if rule.Rule == "MD080" {
								found = true
								evaluation := "complete"
								if unavailable {
									evaluation = "partial"
								}
								if rule.Implementation != "available" || tc.mode == "" && rule.ActiveEvaluation != evaluation {
									t.Fatal(rule)
								}
							}
						}
					}
					if !found || !reflect.DeepEqual(before, configurationBytes(t, root)) {
						t.Fatal("missing rule/changed bytes")
					}
				})
			}
			for _, suffix := range []string{
				"resources: {anchor_style: other}\n", "resources: {anchor_style: null}\n", "resources: {anchor_style: 1}\n", "resources: null\n", "resources: {index_file: index.md}\n",
			} {
				conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1) + suffix
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
				if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 {
					t.Fatal(got)
				}
			}
			for _, option := range []string{"{enabled: false, anchor_style: github}", "{unknown: true}", "{enabled: null}", "{allow_directives: null}", "null"} {
				conf := strings.Replace(cleanConfiguration(), "MD080: {enabled: false}", "MD080: "+option, 1)
				conf = strings.Replace(conf, "flavor: commonmark", "flavor: "+flavor, 1)
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

func TestCompiledPlatformMD080NewlinePolicyComposition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, style := range []string{"github", "explicit"} {
			for _, ending := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}, {"CR", "\r"}} {
				t.Run(flavor+"/"+style+"/"+ending.name, func(t *testing.T) {
					root := configurationProject(t)
					config := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
					config = strings.Replace(config, "MD080: {enabled: false}", "MD080: {enabled: true}", 1)
					config = strings.Replace(config, "MD047: {enabled: false}", "MD047: {enabled: true}", 1) + "resources: {anchor_style: " + style + "}\n"
					putConfigurationFile(t, root, ".mdchk.yaml", config)
					original := "# A" + ending.text + "# A"
					candidate := original + ending.text
					digest := fmt.Sprintf("%x", sha256.Sum256([]byte(candidate)))
					want, status := 0, 0
					if style == "github" {
						want, status = 1, 10
					}
					for pass, source := range []string{original, candidate} {
						putConfigurationFile(t, root, "doc.md", source)
						before := configurationBytes(t, root)
						got := runConfigurationProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
						out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
						if got.Status != status || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != want || len(out.Changes) != 1-pass || len(out.Documents) != 0 || len(out.Files) != 1 {
							t.Fatal(got)
						}
						for _, d := range out.Diagnostics {
							start := 5 + len(ending.text)
							if d.Rule != "MD080" || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: start, End: start + 1}) || d.Range.Start != (result.Position{Line: 2, Column: 3}) || d.Range.End != (result.Position{Line: 2, Column: 4}) || d.Snapshot.SHA256 != digest || d.Snapshot.SizeBytes != len(candidate) || d.Snapshot != *out.Files[0].ResultSnapshot {
								t.Fatal(d)
							}
						}
						if out.Files[0].ResultSnapshot == nil || out.Files[0].ResultSnapshot.SHA256 != digest || out.Files[0].ResultSnapshot.SizeBytes != len(candidate) {
							t.Fatal(out.Files)
						}
						if pass == 0 {
							c := out.Changes[0]
							if !reflect.DeepEqual(c.Rules, []string{"MD047"}) || c.EditCount != 1 || c.Candidate.SHA256 != digest || c.Candidate.SizeBytes != len(candidate) || c.Before != *out.Files[0].InputSnapshot || c.Candidate != *out.Files[0].ResultSnapshot || c.Disposition != "proposed" || c.Diff == nil || len(c.Diff.Hunks) != 1 {
								t.Fatal(c)
							}
							lines := []result.DiffLine{{Kind: "context", Text: "# A", LineEnding: strings.ToLower(ending.name)}, {Kind: "remove", Text: "# A", LineEnding: "none"}, {Kind: "add", Text: "# A", LineEnding: strings.ToLower(ending.name)}}
							if !reflect.DeepEqual(c.Diff.Hunks[0].Lines, lines) {
								t.Fatal(c.Diff)
							}
						}
						if !reflect.DeepEqual(before, configurationBytes(t, root)) {
							t.Fatal("dryrun changed source/config")
						}
					}
				})
			}
		}
	}
}
