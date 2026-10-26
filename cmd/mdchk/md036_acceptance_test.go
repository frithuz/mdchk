package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func TestCompiledPlatformMD036ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	for _, tc := range []struct {
		name, source, option string
		points               []result.ByteRange
		positions            []result.Position
	}{
		{"published", "**Install**\n\nRun the command.", "", []result.ByteRange{{Start: 0, End: 11}}, []result.Position{{Line: 1, Column: 1}}},
		{"disabled", "**A**\n\nBody", "off", nil, nil},
		{"empty_mapping", "**A**\n\nBody", "empty", nil, nil},
		{"unicode", "> **雪**\n>\n> Body", "", []result.ByteRange{{Start: 2, End: 9}}, []result.Position{{Line: 1, Column: 3}}},
		{"nested", "***A***\n\nBody", "", []result.ByteRange{{Start: 0, End: 7}}, []result.Position{{Line: 1, Column: 1}}},
		{"same_item", "- **A**\n\n  Body", "", []result.ByteRange{{Start: 2, End: 7}}, []result.Position{{Line: 1, Column: 3}}},
		{"adjacent", "**A** **B**\n\nBody", "", nil, nil},
		{"multiline", "**A**\nBody", "", nil, nil},
		{"reference_gap", "**A**\n\n[r]: /x\n\nBody", "", nil, nil},
		{"code", "**`A`**\n\nBody", "", nil, nil},
		{"raw_nested_closer", "**A**\n\n`x` <script>*payload</script>* tail", "", []result.ByteRange{{Start: 0, End: 5}}, []result.Position{{Line: 1, Column: 1}}},
		{"valid", "## A\n\nBody", "", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configuration := strings.Replace(cleanConfiguration(), "MD036: {enabled: false}", "MD036: {enabled: true}", 1)
			switch tc.option {
			case "off":
				configuration = strings.Replace(cleanConfiguration(), "MD036: {enabled: false}", "", 1)
			case "empty":
				configuration = strings.Replace(cleanConfiguration(), "MD036: {enabled: false}", "MD036: {}", 1)
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
					if d.Rule != "MD036" || d.FixAvailable || d.ByteRange != tc.points[i] || d.Range.Start != tc.positions[i] || d.Range.End != md036ExpectedEnd(tc.name, tc.positions[i], tc.points[i]) || d.Message != "Use a heading instead of an emphasized paragraph before body text." {
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
				if rule.Rule == "MD036" {
					found = true
					if rule.Implementation != "available" || (tc.option == "" && rule.ActiveEvaluation != "complete") {
						t.Fatal(rule)
					}
				}
			}
			if !found || !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("missing admission or changed source/config")
			}
		})
	}
	for _, option := range []string{"{unknown: true}", "{enabled: false, unknown: null}", "{enabled: null}", "{enabled: 1}", "{enabled: 'false'}", "{enabled: false, allow_directives: null}", "{enabled: false, allow_directives: 'false'}", "null"} {
		configuration := strings.Replace(cleanConfiguration(), "MD036: {enabled: false}", "MD036: "+option, 1)
		putConfigurationFile(t, root, ".mdchk.yaml", configuration)
		got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
		out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
		if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 {
			t.Fatal(got)
		}
	}
}

func md036ExpectedEnd(name string, start result.Position, span result.ByteRange) result.Position {
	width := span.End - span.Start
	if name == "unicode" {
		width = 5
	}
	return result.Position{Line: start.Line, Column: start.Column + width}
}
