package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func TestCompiledPlatformMD018ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	for _, tc := range []struct {
		name, source                string
		status, point, line, column int
	}{
		{"plain", "##Install", 10, 2, 1, 3},
		{"nested original unicode", "> - ##雪\r\n", 10, 6, 1, 7},
		{"continuation unicode", "雪é\r\n> ##good", 10, 12, 2, 5},
		{"valid", "## Install\n", 0, 0, 0, 0},
		{"seven hashes", "#######word", 0, 0, 0, 0},
		{"protected neighbor", "text `open\n##`\n##good", 10, 17, 3, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configuration := strings.Replace(cleanConfiguration(), "MD018: {enabled: false}", "MD018: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", configuration)
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := configurationBytes(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
				got := runConfigurationProcess(t, binary, root, args...)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
				want := 0
				if tc.status == 10 {
					want = 1
				}
				if got.Status != tc.status || !out.Complete || len(out.Errors) != 0 || len(out.Coverage) != 0 || len(out.Diagnostics) != want || len(out.Changes) != 0 || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				if want > 0 {
					d := out.Diagnostics[0]
					if d.Rule != "MD018" || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: tc.point, End: tc.point}) || d.Range.Start != (result.Position{Line: tc.line, Column: tc.column}) || d.Range.End != d.Range.Start {
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
				if rule.Rule == "MD018" {
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
	for _, option := range []string{"unknown: true", "allow_directives: null", "allow_directives: 1", "allow_directives: 'true'"} {
		configuration := strings.Replace(cleanConfiguration(), "MD018: {enabled: false}", "MD018: {enabled: false, "+option+"}", 1)
		putConfigurationFile(t, root, ".mdchk.yaml", configuration)
		got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
		out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
		if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 {
			t.Fatal(got)
		}
	}
	putConfigurationFile(t, root, ".mdchk.yaml", strings.Replace(cleanConfiguration(), "MD018: {enabled: false}", "MD018: null", 1))
	got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
	if got.Status != 12 {
		t.Fatal(got)
	}
}
