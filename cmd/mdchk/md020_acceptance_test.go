package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func TestCompiledPlatformMD020ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	for _, tc := range []struct {
		name, source                                string
		status, point, end, line, column, endColumn int
	}{
		{"plain", "## Install##", 10, 10, 10, 1, 11, 11},
		{"nested original unicode", "> - ## 雪##\r\n", 10, 10, 10, 1, 9, 9},
		{"long unequal", "# x########", 10, 3, 3, 1, 4, 4},
		{"escaped whole run", "# x\\###", 0, 0, 0, 0, 0, 0},
		{"even parity", "# x\\\\###", 10, 5, 5, 1, 6, 6},
		{"valid closer", "## Install ##\n", 0, 0, 0, 0, 0, 0},
		{"real closer literal content", "# literal### ##", 0, 0, 0, 0, 0, 0},
		{"protected active raw", "# <script>x###", 0, 0, 0, 0, 0, 0},
		{"adjacent code", "# `x`###", 10, 5, 5, 1, 6, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configuration := strings.Replace(cleanConfiguration(), "MD020: {enabled: false}", "MD020: {enabled: true}", 1)
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
					if d.Rule != "MD020" || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: tc.point, End: tc.end}) || d.Range.Start != (result.Position{Line: tc.line, Column: tc.column}) || d.Range.End != (result.Position{Line: tc.line, Column: tc.endColumn}) {
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
				if rule.Rule == "MD020" {
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
		configuration := strings.Replace(cleanConfiguration(), "MD020: {enabled: false}", "MD020: {enabled: false, "+option+"}", 1)
		putConfigurationFile(t, root, ".mdchk.yaml", configuration)
		got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
		out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
		if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 {
			t.Fatal(got)
		}
	}
	putConfigurationFile(t, root, ".mdchk.yaml", strings.Replace(cleanConfiguration(), "MD020: {enabled: false}", "MD020: null", 1))
	got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
	if got.Status != 12 {
		t.Fatal(got)
	}
}
