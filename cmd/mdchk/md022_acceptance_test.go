package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func TestCompiledPlatformMD022ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	for _, tc := range []struct {
		name, source, options string
		points                []int
		positions             []result.Position
	}{
		{"published", "Text\n## Install\nText", "", []int{5, 15}, []result.Position{{Line: 2, Column: 1}, {Line: 2, Column: 11}}},
		{"actual edges", "\n# H\n\n", ", before: 2, after: 2", nil, nil},
		{"nested prefix blank", "Text\n> -\n> - ## 雪\n>\nText", "", nil, nil},
		{"multiline setext", "前\n\nAlpha\nBeta\n===\nTail", "", []int{19}, []result.Position{{Line: 5, Column: 4}}},
		{"nondefault", "Text\n\n# H\n\nText", ", before: 0, after: 2", []int{9}, []result.Position{{Line: 3, Column: 4}}},
		{"disabled sides", "Text\n# H\nText", ", before: 0, after: 0", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configuration := strings.Replace(cleanConfiguration(), "MD022: {enabled: false}", "MD022: {enabled: true"+tc.options+"}", 1)
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
					if d.Rule != "MD022" || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: tc.points[i], End: tc.points[i]}) || d.Range.Start != tc.positions[i] || d.Range.End != tc.positions[i] {
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
				if rule.Rule == "MD022" {
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
	for _, option := range []string{"before: -1", "after: 1.5", "before: '1'", "after: true", "before: null", "after: [1]", "unknown: true"} {
		configuration := strings.Replace(cleanConfiguration(), "MD022: {enabled: false}", "MD022: {enabled: false, "+option+"}", 1)
		putConfigurationFile(t, root, ".mdchk.yaml", configuration)
		got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
		out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
		if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 {
			t.Fatal(got)
		}
	}
}
