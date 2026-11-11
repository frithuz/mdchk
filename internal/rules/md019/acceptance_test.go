package md019_test

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

func TestCompiledPlatformMD019ProductionAnalysis(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	root := acceptance.Project(t)
	for _, tc := range []struct {
		name, source                                string
		status, point, end, line, column, endColumn int
		fixable                                     bool
	}{
		{"plain", "##  Install", 10, 3, 4, 1, 4, 5, true},
		{"nested original unicode", "> - ##   雪\r\n", 10, 7, 9, 1, 8, 10, true},
		{"mixed excess", "#  \t text", 10, 2, 5, 1, 3, 6, false},
		{"separated spaces", "# \t Title", 10, 3, 4, 1, 4, 5, false},
		{"two tabs", "#\t\tTitle", 0, 0, 0, 0, 0, 0, false},
		{"valid", "## Install\n", 0, 0, 0, 0, 0, 0, false},
		{"raw empty closed", "#   ###", 0, 0, 0, 0, 0, 0, false},
		{"empty visible", "#  <!-- x -->", 10, 2, 3, 1, 3, 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configuration := strings.Replace(acceptance.CleanConfiguration(), "MD019: {enabled: false}", "MD019: {enabled: true}", 1)
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
				// fix removes a correctable finding; its dry run reports the proposal.
				if args[0] == "fix" && tc.fixable {
					if got.Status != 0 || !out.Complete || len(out.Errors) != 0 || len(out.Coverage) != 0 || len(out.Diagnostics) != 0 || len(out.Changes) != 1 || len(out.Documents) != 0 {
						t.Fatal(got)
					}
					continue
				}
				if got.Status != tc.status || !out.Complete || len(out.Errors) != 0 || len(out.Coverage) != 0 || len(out.Diagnostics) != want || len(out.Changes) != 0 || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				if want > 0 {
					d := out.Diagnostics[0]
					if d.Rule != "MD019" || d.FixAvailable != tc.fixable || d.ByteRange != (result.ByteRange{Start: tc.point, End: tc.end}) || d.Range.Start != (result.Position{Line: tc.line, Column: tc.column}) || d.Range.End != (result.Position{Line: tc.line, Column: tc.endColumn}) {
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
				if rule.Rule == "MD019" {
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
		configuration := strings.Replace(acceptance.CleanConfiguration(), "MD019: {enabled: false}", "MD019: {enabled: false, "+option+"}", 1)
		acceptance.PutFile(t, root, ".mdchk.yaml", configuration)
		got := acceptance.RunProcess(t, binary, root, "lint", "missing.md", "--format=json")
		out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
		if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 {
			t.Fatal(got)
		}
	}
	acceptance.PutFile(t, root, ".mdchk.yaml", strings.Replace(acceptance.CleanConfiguration(), "MD019: {enabled: false}", "MD019: null", 1))
	got := acceptance.RunProcess(t, binary, root, "lint", "missing.md", "--format=json")
	if got.Status != 12 {
		t.Fatal(got)
	}
}
