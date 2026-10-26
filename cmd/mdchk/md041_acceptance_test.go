package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func TestCompiledPlatformMD041ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	for _, tc := range []struct {
		name, source, option string
		point                *result.ByteRange
		position             result.Position
		incomplete           bool
	}{
		{"published", "Introduction text.", "", &result.ByteRange{Start: 0, End: 0}, result.Position{Line: 1, Column: 1}, false},
		{"default", "Intro", "default", &result.ByteRange{Start: 0, End: 0}, result.Position{Line: 1, Column: 1}, false},
		{"empty_mapping", "Intro", "empty", &result.ByteRange{Start: 0, End: 0}, result.Position{Line: 1, Column: 1}, false},
		{"disabled", "Intro", "off", nil, result.Position{}, false},
		{"valid", "# A", "", nil, result.Position{}, false},
		{"wrong_level", "## A", "", &result.ByteRange{Start: 0, End: 0}, result.Position{Line: 1, Column: 1}, false},
		{"level2", "## A", "level2", nil, result.Position{}, false},
		{"nested", "> # A", "", &result.ByteRange{Start: 0, End: 0}, result.Position{Line: 1, Column: 1}, false},
		{"comments", "<!--a--><!--b-->\n# A", "", nil, result.Position{}, false},
		{"comment_tail", "<!--a--> prose\n# A", "", &result.ByteRange{Start: 0, End: 0}, result.Position{Line: 1, Column: 1}, false},
		{"unicode_eof", "<!--雪-->", "", &result.ByteRange{Start: 10, End: 10}, result.Position{Line: 1, Column: 9}, false},
		{"unclosed_comment", "<!-- x", "", &result.ByteRange{Start: 6, End: 6}, result.Position{Line: 1, Column: 7}, false},
		{"reference_eof", "[r]: /x", "", &result.ByteRange{Start: 7, End: 7}, result.Position{Line: 1, Column: 8}, false},
		{"empty", "", "", &result.ByteRange{Start: 0, End: 0}, result.Position{Line: 1, Column: 1}, false},
		{"metadata_level6", "---\ntitle: Guide\n---\nIntro", "title6", nil, result.Position{}, false},
		{"unknown_metadata", "---\ntitle: A\ntitle: B\n---\nIntro", "title", nil, result.Position{}, true},
		{"unknown_satisfied", "---\ntitle: A\ntitle: B\n---\n# A", "title", nil, result.Position{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conf := strings.Replace(cleanConfiguration(), "MD041: {enabled: false}", "MD041: {enabled: true}", 1) + "frontmatter: {enabled: true}\n"
			switch tc.option {
			case "default":
				conf = strings.Replace(conf, "MD041: {enabled: true}", "", 1)
			case "empty":
				conf = strings.Replace(conf, "MD041: {enabled: true}", "MD041: {}", 1)
			case "off":
				conf = strings.Replace(conf, "MD041: {enabled: true}", "MD041: {enabled: false}", 1)
			case "level2":
				conf = strings.Replace(conf, "MD041: {enabled: true}", "MD041: {enabled: true, level: 2}", 1)
			case "title":
				conf = strings.Replace(conf, "MD041: {enabled: true}", "MD041: {enabled: true, frontmatter_title: true}", 1)
			case "title6":
				conf = strings.Replace(conf, "MD041: {enabled: true}", "MD041: {enabled: true, frontmatter_title: true, level: 6}", 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := configurationBytes(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
				got := runConfigurationProcess(t, binary, root, args...)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
				status, want := 0, 0
				if tc.point != nil {
					status, want = 10, 1
				}
				if tc.incomplete {
					status = 15
				}
				if got.Status != status || out.Complete == tc.incomplete || len(out.Errors) != 0 || len(out.Diagnostics) != want || len(out.Changes) != 0 || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				if tc.point != nil {
					d := out.Diagnostics[0]
					if d.Rule != "MD041" || d.FixAvailable || d.ByteRange != *tc.point || d.Range.Start != tc.position || d.Range.End != tc.position || d.Message != "Document must begin with a heading at the configured level." {
						t.Fatal(d)
					}
				}
				if tc.incomplete {
					found := false
					for _, c := range out.Coverage {
						found = found || c.Rule == "MD041" && c.Reason == result.ContextUnavailable
					}
					if !found {
						t.Fatal(out.Coverage)
					}
				}
			}
			seen, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
			if seen.Status != 0 {
				t.Fatal(seen)
			}
			found := false
			for _, rule := range *p.Rules.Data {
				if rule.Rule == "MD041" {
					found = true
					expected := "complete"
					if tc.incomplete {
						expected = "partial"
					}
					if rule.Implementation != "available" || (tc.option != "off" && rule.ActiveEvaluation != expected) {
						t.Fatal(rule)
					}
				}
			}
			if !found || !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("missing admission or source/config changed")
			}
		})
	}
	for _, option := range []string{"{level: 0}", "{level: 7}", "{level: 1.5}", "{level: '1'}", "{level: true}", "{level: null}", "{enabled: false, frontmatter_title: null}", "{frontmatter_title: 'false'}", "{enabled: false, unknown: true}", "{enabled: null}", "{allow_directives: null}", "null"} {
		conf := strings.Replace(cleanConfiguration(), "MD041: {enabled: false}", "MD041: "+option, 1)
		putConfigurationFile(t, root, ".mdchk.yaml", conf)
		got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
		out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
		if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 {
			t.Fatal(got)
		}
	}
}
