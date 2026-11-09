package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func TestCompiledPlatformMD003ProductionAnalysis(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	root := configurationProject(t)
	for _, tc := range []struct {
		name, source, style                                  string
		status, start, end, line, column, endLine, endColumn int
		fixable                                              bool
	}{
		{"consistent open", "# First\n## Next\n", "consistent", 0, 0, 0, 0, 0, 0, 0, false},
		{"consistent closed", "# First #\n## Next\n", "consistent", 10, 10, 18, 2, 1, 3, 1, true},
		{"explicit open first", "# First #\n", "atx", 10, 0, 10, 1, 1, 2, 1, true},
		{"explicit closed", "# First\n", "atx_closed", 10, 0, 8, 1, 1, 2, 1, true},
		{"setext no H3 fallback", "### Third\n", "setext", 10, 0, 10, 1, 1, 2, 1, false},
		{"nested original unicode", "# Root\r\n\r\n> ## 雪é\t #", "atx", 10, 12, 24, 3, 3, 3, 12, true},
		{"multiline setext", "Title\nsecond\n======\n", "atx", 10, 0, 20, 1, 1, 4, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configuration := strings.Replace(cleanConfiguration(), "MD003: {enabled: false}", "MD003: {enabled: true, style: "+tc.style+"}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", configuration)
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := configurationBytes(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
				got := runConfigurationProcess(t, binary, root, args...)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
				want, status, changes := 0, tc.status, 0
				if tc.status == 10 {
					want = 1
				}
				// A dry run proposes an eligible correction and reports the clean candidate.
				if tc.fixable && args[0] == "fix" {
					want, status, changes = 0, 0, 1
				}
				if got.Status != status || !out.Complete || len(out.Errors) != 0 || len(out.Coverage) != 0 || len(out.Diagnostics) != want || len(out.Changes) != changes || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				if want > 0 {
					d := out.Diagnostics[0]
					if d.Rule != "MD003" || d.FixAvailable != tc.fixable || d.ByteRange != (result.ByteRange{Start: tc.start, End: tc.end}) || d.Range.Start != (result.Position{Line: tc.line, Column: tc.column}) || d.Range.End != (result.Position{Line: tc.endLine, Column: tc.endColumn}) {
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
				if rule.Rule == "MD003" {
					found = true
					if rule.Implementation != "available" || rule.ActiveEvaluation != "complete" {
						t.Fatal(rule)
					}
				}
			}
			if !found || !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("missing registration or changed files")
			}
		})
	}
	for _, option := range []string{"style: ATX", "style: null", "style: true", "style: 1", "unknown: true"} {
		configuration := strings.Replace(cleanConfiguration(), "MD003: {enabled: false}", "MD003: {enabled: false, "+option+"}", 1)
		putConfigurationFile(t, root, ".mdchk.yaml", configuration)
		got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
		out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
		if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 {
			t.Fatal(got)
		}
	}
}
