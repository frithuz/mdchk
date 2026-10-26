package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func TestCompiledPlatformMD063ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, tc := range []struct {
				name, source, mode, style string
				names                     []string
				span                      *result.ByteRange
				start, end                result.Position
				incomplete                bool
			}{
				{"published", "## installation guide", "", "sentence", nil, &result.ByteRange{Start: 3, End: 4}, result.Position{Line: 1, Column: 4}, result.Position{Line: 1, Column: 5}, false},
				{"valid", "## Installation guide", "", "sentence", nil, nil, result.Position{}, result.Position{}, false},
				{"title", "## Installation guide", "", "title", nil, &result.ByteRange{Start: 16, End: 17}, result.Position{Line: 1, Column: 17}, result.Position{Line: 1, Column: 18}, false},
				{"name", "# iPhone guide", "", "sentence", []string{"iPhone"}, nil, result.Position{}, result.Position{}, false},
				{"possessive", "# iPhone's guide", "", "sentence", []string{"iPhone"}, nil, result.Position{}, result.Position{}, false},
				{"name_boundary", "# iPhone_case", "", "sentence", []string{"iPhone"}, &result.ByteRange{Start: 2, End: 3}, result.Position{Line: 1, Column: 3}, result.Position{Line: 1, Column: 4}, false},
				{"code_sentence", "# `lower` guide", "", "sentence", nil, nil, result.Position{}, result.Position{}, false},
				{"code_title", "# `lower` guide", "", "title", nil, &result.ByteRange{Start: 10, End: 11}, result.Position{Line: 1, Column: 11}, result.Position{Line: 1, Column: 12}, false},
				{"entity", "# &#97;bc", "", "sentence", nil, &result.ByteRange{Start: 2, End: 7}, result.Position{Line: 1, Column: 3}, result.Position{Line: 1, Column: 8}, false},
				{"unicode", "# ßeta", "", "sentence", nil, &result.ByteRange{Start: 2, End: 4}, result.Position{Line: 1, Column: 3}, result.Position{Line: 1, Column: 4}, false},
				{"nested", "雪\r\n\r\n> ## lower", "", "sentence", nil, &result.ByteRange{Start: 12, End: 13}, result.Position{Line: 3, Column: 6}, result.Position{Line: 3, Column: 7}, false},
				{"raw_closer", "# <script>*payload</script>*lower", "", "sentence", nil, &result.ByteRange{Start: 28, End: 29}, result.Position{Line: 1, Column: 29}, result.Position{Line: 1, Column: 30}, false},
				{"apostrophe", "# Don't Stop", "", "title", nil, nil, result.Position{}, result.Position{}, false},
				{"connector", "# A_b", "", "title", nil, nil, result.Position{}, result.Position{}, false},
				{"empty", "#", "", "sentence", nil, nil, result.Position{}, result.Position{}, false},
				{"disabled", "# lower", "off", "", nil, nil, result.Position{}, result.Position{}, false},
				{"empty_mapping", "# lower", "mapping", "", nil, nil, result.Position{}, result.Position{}, false},
				{"unreliable", "---\n# lower", "", "sentence", nil, nil, result.Position{}, result.Position{}, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if tc.names == nil {
						tc.names = []string{}
					}
					raw, err := json.Marshal(map[string]any{"enabled": true, "style": tc.style, "proper_names": tc.names})
					if err != nil {
						t.Fatal(err)
					}
					conf := strings.Replace(cleanConfiguration(), "MD063: {enabled: false}", "MD063: "+string(raw), 1) + "frontmatter: {enabled: true}\n"
					if tc.mode == "off" {
						conf = strings.Replace(cleanConfiguration(), "MD063: {enabled: false}", "", 1)
					}
					if tc.mode == "mapping" {
						conf = strings.Replace(cleanConfiguration(), "MD063: {enabled: false}", "MD063: {}", 1)
					}
					conf = strings.Replace(conf, "flavor: commonmark", "flavor: "+flavor, 1)
					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					putConfigurationFile(t, root, "doc.md", tc.source)
					before := configurationBytes(t, root)
					for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
						got := runConfigurationProcess(t, binary, root, args...)
						out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
						status, want := 0, 0
						if tc.span != nil {
							status, want = 10, 1
						}
						if tc.incomplete {
							status = 15
						}
						if got.Status != status || out.Complete == tc.incomplete || len(out.Errors) != 0 || len(out.Diagnostics) != want || len(out.Changes) != 0 || len(out.Documents) != 0 {
							t.Fatal(got)
						}
						if tc.span != nil {
							d := out.Diagnostics[0]
							if d.Rule != "MD063" || d.FixAvailable || d.ByteRange != *tc.span || d.Range.Start != tc.start || d.Range.End != tc.end || d.Message != "Heading must follow the configured capitalization style." {
								t.Fatal(d)
							}
						}
					}
					seen, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
					if seen.Status != 0 {
						t.Fatal(seen)
					}
					found := false
					if tc.incomplete {
						if p.Rules.Data != nil || p.Rules.Availability != "unavailable" || len(p.Rules.Reasons) == 0 {
							t.Fatal(p.Rules)
						}
						found = true
					} else if p.Rules.Data == nil {
						t.Fatal(p.Rules)
					}
					if p.Rules.Data != nil {
						for _, rule := range *p.Rules.Data {
							if rule.Rule == "MD063" {
								found = true
								expected := "complete"
								if tc.incomplete {
									expected = "partial"
								}
								if rule.Implementation != "available" || (tc.mode == "" && rule.ActiveEvaluation != expected) {
									t.Fatal(rule)
								}
							}
						}
					}
					if !found || !reflect.DeepEqual(before, configurationBytes(t, root)) {
						t.Fatal("missing admission or mutated bytes")
					}
				})
			}
			for _, option := range []string{`{style: other}`, `{style: null}`, `{style: 1}`, `{enabled: false, style: other}`, `{proper_names: null}`, `{proper_names: iPhone}`, `{proper_names: [iPhone, 1]}`, `{enabled: null}`, `{allow_directives: null}`, `{unknown: true}`, `null`} {
				conf := strings.Replace(cleanConfiguration(), "MD063: {enabled: false}", "MD063: "+option, 1)
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
