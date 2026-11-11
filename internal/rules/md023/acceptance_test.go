package md023_test

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

func TestCompiledPlatformMD023ProductionAnalysis(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	root := acceptance.Project(t)
	for _, tc := range []struct {
		name, source string
		points       []result.ByteRange
		positions    []result.Position
		fixable      bool
	}{
		{"published", " ## Install", []result.ByteRange{{Start: 1, End: 3}}, []result.Position{{Line: 1, Column: 2}}, true},
		{"nested content relative", "> - # H", nil, nil, false},
		{"partial tab", ">\t# H", []result.ByteRange{{Start: 2, End: 3}}, []result.Position{{Line: 1, Column: 3}}, false},
		{"Setext text trigger marker", "Alpha\n    Beta\n===", []result.ByteRange{{Start: 15, End: 18}}, []result.Position{{Line: 3, Column: 1}}, true},
		{"valid", "# H", nil, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configuration := strings.Replace(acceptance.CleanConfiguration(), "MD023: {enabled: false}", "MD023: {enabled: true}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", configuration)
			acceptance.PutFile(t, root, "doc.md", tc.source)
			before := acceptance.ConfigurationBytes(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
				got := acceptance.RunProcess(t, binary, root, args...)
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
				status, points, changes := 0, tc.points, 0
				if len(tc.points) > 0 {
					status = 10
				}
				// A fixable finding's preview corrects it, leaving nothing reported.
				if args[0] == "fix" && tc.fixable {
					status, points, changes = 0, nil, 1
				}
				if got.Status != status || !out.Complete || len(out.Errors) != 0 || len(out.Coverage) != 0 || len(out.Diagnostics) != len(points) || len(out.Changes) != changes || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				for i, d := range out.Diagnostics {
					if d.Rule != "MD023" || d.FixAvailable != tc.fixable || d.ByteRange != tc.points[i] || d.Range.Start != tc.positions[i] || d.Range.End != (result.Position{Line: tc.positions[i].Line, Column: tc.positions[i].Column + tc.points[i].End - tc.points[i].Start}) || d.Message != "Heading must not be indented." {
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
				if rule.Rule == "MD023" {
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
	for _, option := range []string{"{enabled: false, unknown: true}", "{enabled: null}", "{enabled: false, allow_directives: 1}", "{enabled: false, allow_directives: null}", "{enabled: false, allow_directives: 'false'}", "null"} {
		configuration := strings.Replace(acceptance.CleanConfiguration(), "MD023: {enabled: false}", "MD023: "+option, 1)
		acceptance.PutFile(t, root, ".mdchk.yaml", configuration)
		got := acceptance.RunProcess(t, binary, root, "lint", "missing.md", "--format=json")
		out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
		if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 {
			t.Fatal(got)
		}
	}
}
