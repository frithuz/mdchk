package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func TestCompiledPlatformMD023ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	for _, tc := range []struct {
		name, source string
		points       []result.ByteRange
		positions    []result.Position
	}{
		{"published", " ## Install", []result.ByteRange{{Start: 1, End: 3}}, []result.Position{{Line: 1, Column: 2}}},
		{"nested content relative", "> - # H", nil, nil},
		{"partial tab", ">\t# H", []result.ByteRange{{Start: 2, End: 3}}, []result.Position{{Line: 1, Column: 3}}},
		{"Setext text trigger marker", "Alpha\n    Beta\n===", []result.ByteRange{{Start: 15, End: 18}}, []result.Position{{Line: 3, Column: 1}}},
		{"valid", "# H", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configuration := strings.Replace(cleanConfiguration(), "MD023: {enabled: false}", "MD023: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", configuration)
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := configurationBytes(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
				got := runConfigurationProcess(t, binary, root, args...)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
				status := 0
				if len(tc.points) > 0 {
					status = 10
				}
				if got.Status != status || !out.Complete || len(out.Errors) != 0 || len(out.Coverage) != 0 || len(out.Diagnostics) != len(tc.points) || len(out.Changes) != 0 || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				for i, d := range out.Diagnostics {
					if d.Rule != "MD023" || d.FixAvailable || d.ByteRange != tc.points[i] || d.Range.Start != tc.positions[i] || d.Range.End != (result.Position{Line: tc.positions[i].Line, Column: tc.positions[i].Column + tc.points[i].End - tc.points[i].Start}) || d.Message != "Heading must not be indented." {
						t.Fatal(d)
					}
				}
			}
			seen, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
			if seen.Status != 0 {
				t.Fatal(seen)
			}
			found := false
			for _, rule := range *p.Rules.Data {
				if rule.Rule == "MD023" {
					found = true
					if rule.Implementation != "available" || rule.ActiveEvaluation != "complete" {
						t.Fatal(rule)
					}
				}
			}
			if !found || !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("missing admission or changed source/config")
			}
		})
	}
	for _, option := range []string{"{enabled: false, unknown: true}", "{enabled: null}", "{enabled: false, allow_directives: 1}", "{enabled: false, allow_directives: null}", "{enabled: false, allow_directives: 'false'}", "null"} {
		configuration := strings.Replace(cleanConfiguration(), "MD023: {enabled: false}", "MD023: "+option, 1)
		putConfigurationFile(t, root, ".mdchk.yaml", configuration)
		got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
		out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
		if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 {
			t.Fatal(got)
		}
	}
}
