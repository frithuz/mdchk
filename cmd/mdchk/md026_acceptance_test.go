package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func TestCompiledPlatformMD026ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	for _, tc := range []struct {
		name, source, option string
		points               []result.ByteRange
		positions            []result.Position
	}{
		{"published", "## Installation!", "", []result.ByteRange{{Start: 15, End: 16}}, []result.Position{{Line: 1, Column: 16}}},
		{"disabled", "# A!", "off", nil, nil},
		{"empty", "# A!", "empty", nil, nil},
		{"unicode", "# 雪！", "unicode", []result.ByteRange{{Start: 5, End: 8}}, []result.Position{{Line: 1, Column: 4}}},
		{"entity", "# A&#33;", "", []result.ByteRange{{Start: 3, End: 8}}, []result.Position{{Line: 1, Column: 4}}},
		{"code", "# A! `?`", "", nil, nil},
		{"escape", "# A!\\?", "", nil, nil},
		{"Setext", "A!\n===", "", []result.ByteRange{{Start: 1, End: 2}}, []result.Position{{Line: 1, Column: 2}}},
		{"containers", "> # A!", "", []result.ByteRange{{Start: 5, End: 6}}, []result.Position{{Line: 1, Column: 6}}},
		{"valid", "# A", "", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configuration := strings.Replace(cleanConfiguration(), "MD026: {enabled: false}", "MD026: {enabled: true}", 1)
			switch tc.option {
			case "off":
				configuration = strings.Replace(cleanConfiguration(), "MD026: {enabled: false}", "", 1)
			case "empty":
				configuration = strings.Replace(configuration, "MD026: {enabled: true}", "MD026: {enabled: true, punctuation: ''}", 1)
			case "unicode":
				configuration = strings.Replace(configuration, "MD026: {enabled: true}", "MD026: {enabled: true, punctuation: '！'}", 1)
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

				if got.Status != status || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(tc.points) || len(out.Changes) != 0 || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				for i, d := range out.Diagnostics {
					if d.Rule != "MD026" || d.FixAvailable || d.ByteRange != tc.points[i] || d.Range.Start != tc.positions[i] || d.Range.End != md026ExpectedEnd(tc.name, tc.positions[i], tc.points[i]) || d.Message != "Heading must not end with configured punctuation." {
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
				if rule.Rule == "MD026" {
					found = true
					if rule.Implementation != "available" || (tc.option != "off" && rule.ActiveEvaluation != "complete") {
						t.Fatal(rule)
					}
				}
			}
			if !found || !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("missing admission or changed source/config")
			}
		})
	}
	for _, option := range []string{"{punctuation: null}", "{punctuation: 1}", "{punctuation: true}", "{punctuation: []}", "{enabled: false, punctuation: {}}", "{enabled: false, unknown: true}", "{enabled: null}", "{enabled: false, allow_directives: null}", "{enabled: false, allow_directives: 'false'}", "null"} {
		configuration := strings.Replace(cleanConfiguration(), "MD026: {enabled: false}", "MD026: "+option, 1)
		putConfigurationFile(t, root, ".mdchk.yaml", configuration)
		got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
		out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
		if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 {
			t.Fatal(got)
		}
	}
}

func md026ExpectedEnd(name string, start result.Position, span result.ByteRange) result.Position {
	width := span.End - span.Start
	if name == "unicode" {
		width = 1
	}
	return result.Position{Line: start.Line, Column: start.Column + width}
}
