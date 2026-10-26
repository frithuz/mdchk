package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func TestCompiledPlatformMD025ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	for _, tc := range []struct {
		name, source string
		points       []result.ByteRange
		positions    []result.Position
	}{
		{"published", "# Guide\n\n# Usage", []result.ByteRange{{Start: 11, End: 16}}, []result.Position{{Line: 3, Column: 3}}},
		{"empty point", "#\n\n#", []result.ByteRange{{Start: 4, End: 4}}, []result.Position{{Line: 3, Column: 2}}},
		{"Setext raw label", "# A B\n\n*a*\nb\n===", []result.ByteRange{{Start: 7, End: 12}}, []result.Position{{Line: 3, Column: 1}}},
		{"containers", "# A\n\n> # B", []result.ByteRange{{Start: 9, End: 10}}, []result.Position{{Line: 3, Column: 5}}},
		{"metadata level3", "---\ntitle: Guide\n---\n### Body", []result.ByteRange{{Start: 25, End: 29}}, []result.Position{{Line: 4, Column: 5}}},
		{"metadata optout", "---\ntitle: Guide\n---\n### Body", nil, nil},
		{"unknown metadata", "---\ntitle: [\n---\n# First\n# Second", []result.ByteRange{{Start: 27, End: 33}}, []result.Position{{Line: 5, Column: 3}}},
		{"valid", "# H", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configuration := strings.Replace(cleanConfiguration(), "MD025: {enabled: false}", "MD025: {enabled: true}", 1)
			configuration += "frontmatter: {enabled: true}\n"
			if tc.name == "metadata level3" {
				configuration = strings.Replace(configuration, "MD025: {enabled: true}", "MD025: {enabled: true, level: 3, frontmatter_title: true}", 1)
			}
			if tc.name == "unknown metadata" {
				configuration = strings.Replace(configuration, "MD025: {enabled: true}", "MD025: {enabled: true, frontmatter_title: true}", 1)
			}
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
				if tc.name == "unknown metadata" {
					status = 15
				}
				if got.Status != status || out.Complete != (status != 15) || len(out.Errors) != 0 || (status == 15 && len(out.Coverage) != 1) || len(out.Diagnostics) != len(tc.points) || len(out.Changes) != 0 || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				for i, d := range out.Diagnostics {
					if d.Rule != "MD025" || d.FixAvailable || d.ByteRange != tc.points[i] || d.Range.Start != tc.positions[i] || d.Range.End != md025ExpectedEnd(tc.name, tc.positions[i], tc.points[i]) || d.Message != "Document must not contain multiple titles." {
						t.Fatal(d)
					}
				}
			}
			seen, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
			if seen.Status != 0 && tc.name != "unknown metadata" {
				t.Fatal(seen)
			}
			found := false
			for _, rule := range *p.Rules.Data {
				if rule.Rule == "MD025" {
					found = true
					if rule.Implementation != "available" || (tc.name != "unknown metadata" && rule.ActiveEvaluation != "complete") {
						t.Fatal(rule)
					}
				}
			}
			if !found || !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("missing admission or changed source/config")
			}
		})
	}
	for _, option := range []string{"{level: null}", "{level: 0}", "{level: 7}", "{level: -1}", "{level: 1.5}", "{level: true}", "{level: '1'}", "{level: []}", "{enabled: false, level: {}}", "{frontmatter_title: null}", "{frontmatter_title: 'false'}", "{enabled: false, unknown: true}", "{enabled: null}", "{enabled: false, allow_directives: 1}", "{enabled: false, allow_directives: null}", "{enabled: false, allow_directives: 'false'}", "null"} {
		configuration := strings.Replace(cleanConfiguration(), "MD025: {enabled: false}", "MD025: "+option, 1)
		putConfigurationFile(t, root, ".mdchk.yaml", configuration)
		got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
		out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
		if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 {
			t.Fatal(got)
		}
	}
}

func md025ExpectedEnd(name string, start result.Position, span result.ByteRange) result.Position {
	if name == "Setext raw label" {
		return result.Position{Line: 4, Column: 2}
	}
	return result.Position{Line: start.Line, Column: start.Column + span.End - span.Start}
}
