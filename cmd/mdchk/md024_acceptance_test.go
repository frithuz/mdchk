package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func TestCompiledPlatformMD024ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	for _, tc := range []struct {
		name, source string
		points       []result.ByteRange
		positions    []result.Position
	}{
		{"published", "## Install\n\n## install", []result.ByteRange{{Start: 15, End: 22}}, []result.Position{{Line: 3, Column: 4}}},
		{"Unicode full fold", "# Straße\n# STRASSE", []result.ByteRange{{Start: 12, End: 19}}, []result.Position{{Line: 2, Column: 3}}},
		{"empty point", "#\n\n## ##", []result.ByteRange{{Start: 5, End: 5}}, []result.Position{{Line: 3, Column: 3}}},
		{"Setext raw label", "# A B\n\n*a*\nb\n===", []result.ByteRange{{Start: 7, End: 12}}, []result.Position{{Line: 3, Column: 1}}},
		{"nested sibling identity", "# P\n> ## Child\n\n# Q\n> ## child", nil, nil},
		{"valid", "# H", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configuration := strings.Replace(cleanConfiguration(), "MD024: {enabled: false}", "MD024: {enabled: true}", 1)
			if tc.name == "nested sibling identity" {
				configuration = strings.Replace(configuration, "MD024: {enabled: true}", "MD024: {enabled: true, siblings_only: true}", 1)
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
				if got.Status != status || !out.Complete || len(out.Errors) != 0 || len(out.Coverage) != 0 || len(out.Diagnostics) != len(tc.points) || len(out.Changes) != 0 || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				for i, d := range out.Diagnostics {
					if d.Rule != "MD024" || d.FixAvailable || d.ByteRange != tc.points[i] || d.Range.Start != tc.positions[i] || d.Range.End != md024ExpectedEnd(tc.name, tc.positions[i], tc.points[i]) || d.Message != "Heading text must not repeat." {
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
				if rule.Rule == "MD024" {
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
	for _, option := range []string{"{siblings_only: null}", "{siblings_only: 1}", "{siblings_only: false, unknown: true}", "{siblings_only: 'false'}", "{siblings_only: []}", "{enabled: false, siblings_only: {}}", "{enabled: false, unknown: true}", "{enabled: null}", "{enabled: false, allow_directives: 1}", "{enabled: false, allow_directives: null}", "{enabled: false, allow_directives: 'false'}", "null"} {
		configuration := strings.Replace(cleanConfiguration(), "MD024: {enabled: false}", "MD024: "+option, 1)
		putConfigurationFile(t, root, ".mdchk.yaml", configuration)
		got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
		out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
		if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 {
			t.Fatal(got)
		}
	}
}

func md024ExpectedEnd(name string, start result.Position, span result.ByteRange) result.Position {
	if name == "Setext raw label" {
		return result.Position{Line: 4, Column: 2}
	}
	return result.Position{Line: start.Line, Column: start.Column + span.End - span.Start}
}
