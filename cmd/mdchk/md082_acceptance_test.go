package main

import (
	"crypto/sha256"
	"fmt"
	"github.com/frithuz/mdchk/internal/result"
	"reflect"
	"strings"
	"testing"
)

type md082NativeExpected struct {
	span       result.ByteRange
	start, end result.Position
}

func TestCompiledPlatformMD082ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, tc := range []struct {
				name, source, mode, style       string
				expected, gfmExpected           []md082NativeExpected
				gfmUnavailable, parserUncertain bool
			}{
				{"published", "## Install\n\n## Usage\nText", "", "github", []md082NativeExpected{{result.ByteRange{Start: 3, End: 10}, result.Position{Line: 1, Column: 4}, result.Position{Line: 1, Column: 11}}}, nil, false, false},
				{"valid", "# A\nText", "", "github", []md082NativeExpected{}, nil, false, false},
				{"child_counts", "# A\n## B\n# C\nText", "", "github", []md082NativeExpected{{result.ByteRange{Start: 7, End: 8}, result.Position{Line: 2, Column: 4}, result.Position{Line: 2, Column: 5}}}, nil, false, false},
				{"empty_label", "#\n#", "", "github", []md082NativeExpected{{result.ByteRange{Start: 1, End: 1}, result.Position{Line: 1, Column: 2}, result.Position{Line: 1, Column: 2}}, {result.ByteRange{Start: 3, End: 3}, result.Position{Line: 2, Column: 2}, result.Position{Line: 2, Column: 2}}}, nil, false, false},
				{"final_allowed", "# A\n## B", "final", "github", []md082NativeExpected{}, nil, false, false},
				{"nonfinal_child", "# A\n## B\n## C", "final", "github", []md082NativeExpected{{result.ByteRange{Start: 7, End: 8}, result.Position{Line: 2, Column: 4}, result.Position{Line: 2, Column: 5}}}, nil, false, false},
				{"nested_comment", "# A\n\n> <!--c-->\n> <!--d-->\n\n# B\nText", "", "github", []md082NativeExpected{{result.ByteRange{Start: 2, End: 3}, result.Position{Line: 1, Column: 3}, result.Position{Line: 1, Column: 4}}}, nil, false, false},
				{"entities", "# A\n\n&nbsp;\n\n# B\nText", "", "github", []md082NativeExpected{{result.ByteRange{Start: 2, End: 3}, result.Position{Line: 1, Column: 3}, result.Position{Line: 1, Column: 4}}}, nil, false, false},
				{"image", "# A\n\n![](x)", "", "github", []md082NativeExpected{}, nil, false, false},
				{"task", "# A\n\n- [ ]", "", "github", []md082NativeExpected{}, nil, false, false},
				{"empty_link", "# A\n\n[](x)", "", "github", []md082NativeExpected{{result.ByteRange{Start: 2, End: 3}, result.Position{Line: 1, Column: 3}, result.Position{Line: 1, Column: 4}}}, nil, false, false},
				{"script_known", "# A\n\n<script id=\"x\">\n</script>", "", "github", []md082NativeExpected{}, nil, false, false},
				{"script_explicit", "# A\n\n<script id=\"x\">\n</script>", "", "explicit", []md082NativeExpected{}, nil, false, false},
				{"code_span", "# A\n\n` `", "", "github", []md082NativeExpected{}, nil, false, false},
				{"fence", "# A\n\n```\n```", "", "github", []md082NativeExpected{}, nil, false, false},
				{"html_tag", "# A\n\n<span></span>", "", "github", []md082NativeExpected{}, nil, false, false},
				{"thematic", "# A\n\n***", "", "github", []md082NativeExpected{}, nil, false, false},
				{"table", "# A\n\n| A |\n| - |", "", "github", []md082NativeExpected{}, nil, false, false},
				{"comment_tail", "# A\n\n<!--c--> text", "", "github", []md082NativeExpected{}, nil, false, false},
				{"bare_quote", "# A\n\n>", "", "github", []md082NativeExpected{{result.ByteRange{Start: 2, End: 3}, result.Position{Line: 1, Column: 3}, result.Position{Line: 1, Column: 4}}}, nil, false, false},
				{"unreliable", "---\n# A", "", "github", []md082NativeExpected{}, nil, false, true},
				{"disabled", "# A", "off", "github", nil, nil, false, false},
				{"empty_mapping", "# A", "mapping", "github", nil, nil, false, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					conf := strings.Replace(cleanConfiguration(), "MD082: {enabled: false}", "MD082: {enabled: true}", 1) + "frontmatter: {enabled: true}\n"
					if tc.mode == "final" {
						conf = strings.Replace(conf, "MD082: {enabled: true}", "MD082: {enabled: true, allow_final_section: true}", 1)
					}
					if tc.mode == "off" {
						conf = strings.Replace(cleanConfiguration(), "MD082: {enabled: false}", "", 1)
					}
					if tc.mode == "mapping" {
						conf = strings.Replace(cleanConfiguration(), "MD082: {enabled: false}", "MD082: {}", 1)
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
							if d.Rule != "MD082" || d.FixAvailable || d.ByteRange != w.span || d.Range.Start != w.start || d.Range.End != w.end || d.Message != "Section must contain substantive content." {
								t.Fatal(d, w)
							}
						}
						if unavailable {
							found := false
							for _, coverage := range out.Coverage {
								found = found || coverage.Rule == "MD082" && coverage.Reason == result.ContextUnavailable
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
							if rule.Rule == "MD082" {
								found = true
								evaluation := "complete"
								if unavailable {
									evaluation = "partial"
								}
								if rule.Implementation != "available" || (tc.mode == "" || tc.mode == "final") && rule.ActiveEvaluation != evaluation {
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
			for _, option := range []string{"{enabled: false, allow_final_section: null}", "{enabled: false, allow_final_section: 1}", "{allow_final_section: true, unknown: false}", "{unknown: true}", "{enabled: null}", "{allow_directives: null}", "null"} {
				conf := strings.Replace(cleanConfiguration(), "MD082: {enabled: false}", "MD082: "+option, 1)
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

func TestCompiledPlatformMD082NewlinePolicyComposition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, style := range []string{"false", "true"} {
			for _, ending := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}, {"CR", "\r"}} {
				t.Run(flavor+"/"+style+"/"+ending.name, func(t *testing.T) {
					root := configurationProject(t)
					config := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
					config = strings.Replace(config, "MD082: {enabled: false}", "MD082: {enabled: true, allow_final_section: "+style+"}", 1)
					config = strings.Replace(config, "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
					putConfigurationFile(t, root, ".mdchk.yaml", config)
					original := "#" + ending.text + "#"
					candidate := original + ending.text
					digest := fmt.Sprintf("%x", sha256.Sum256([]byte(candidate)))
					want, status := 2, 10
					if style == "true" {
						want = 1
					}
					for pass, source := range []string{original, candidate} {
						putConfigurationFile(t, root, "doc.md", source)
						before := configurationBytes(t, root)
						got := runConfigurationProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
						out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
						if got.Status != status || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != want || len(out.Changes) != 1-pass || len(out.Documents) != 0 || len(out.Files) != 1 {
							t.Fatal(got)
						}
						for i, d := range out.Diagnostics {
							start, line := 1, 1
							if i == 1 {
								start = 2 + len(ending.text)
								line = 2
							}
							if d.Rule != "MD082" || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: start, End: start}) || d.Range.Start != (result.Position{Line: line, Column: 2}) || d.Range.End != (result.Position{Line: line, Column: 2}) || d.Snapshot.SHA256 != digest || d.Snapshot.SizeBytes != len(candidate) || d.Snapshot != *out.Files[0].ResultSnapshot {
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
							lines := []result.DiffLine{{Kind: "context", Text: "#", LineEnding: strings.ToLower(ending.name)}, {Kind: "remove", Text: "#", LineEnding: "none"}, {Kind: "add", Text: "#", LineEnding: strings.ToLower(ending.name)}}
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
