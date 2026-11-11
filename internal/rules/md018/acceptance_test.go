package md018_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
	"github.com/frithuz/mdchk/internal/result"
)

// TestMain removes this process's private builds once every test has finished
// with them.
func TestMain(m *testing.M) {
	m.Run()
	acceptance.Cleanup()
}

func TestCompiledPlatformMD018ProductionAnalysis(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	root := acceptance.Project(t)
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
			configuration := strings.Replace(acceptance.CleanConfiguration(), "MD018: {enabled: false}", "MD018: {enabled: true}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", configuration)
			acceptance.PutFile(t, root, "doc.md", tc.source)
			before := acceptance.ConfigurationBytes(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
				got := acceptance.RunProcess(t, binary, root, args...)
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
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
			seen, p := acceptance.RunInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
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
			if !found || !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
				t.Fatal("missing admission or changed source/config")
			}
		})
	}
	for _, option := range []string{"unknown: true", "allow_directives: null", "allow_directives: 1", "allow_directives: 'true'"} {
		configuration := strings.Replace(acceptance.CleanConfiguration(), "MD018: {enabled: false}", "MD018: {enabled: false, "+option+"}", 1)
		acceptance.PutFile(t, root, ".mdchk.yaml", configuration)
		got := acceptance.RunProcess(t, binary, root, "lint", "missing.md", "--format=json")
		out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
		if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 {
			t.Fatal(got)
		}
	}
	acceptance.PutFile(t, root, ".mdchk.yaml", strings.Replace(acceptance.CleanConfiguration(), "MD018: {enabled: false}", "MD018: null", 1))
	got := acceptance.RunProcess(t, binary, root, "lint", "missing.md", "--format=json")
	if got.Status != 12 {
		t.Fatal(got)
	}
}
