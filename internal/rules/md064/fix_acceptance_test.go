package md064_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
	"github.com/frithuz/mdchk/internal/result"
)

// correctionConfiguration enables MD064 at maximum, plus the fixture's extra
// rules or directive processing, over the ordinary production configuration.
func correctionConfiguration(flavor string, maximum int, variant string) string {
	config := strings.Replace(md064Configuration(flavor), "MD064: {enabled: true}", fmt.Sprintf("MD064: {enabled: true, maximum: %d}", maximum), 1)
	switch variant {
	case "directives":
		config = strings.Replace(config, "directives: {enabled: false}", "directives: {enabled: true}", 1)
	case "md010":
		config = strings.Replace(config, "MD010: {enabled: false}", "MD010: {enabled: true}", 1)
	case "md009":
		config = strings.Replace(config, "MD009: {enabled: false}", "MD009: {enabled: true}", 1)
		fallthrough
	case "md047":
		config = strings.Replace(config, "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	}
	return config
}

func TestCompiledMD064FixWorkflow(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range correctionCases(t) {
			if tc.Flavor != "" && tc.Flavor != flavor {
				continue
			}
			t.Run(flavor+"/"+tc.Name, func(t *testing.T) {
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", correctionConfiguration(flavor, tc.Maximum, tc.Config))
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				changed := tc.Source != tc.Candidate

				lint := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
				fixable := false
				for _, d := range acceptance.ValidateOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics {
					fixable = fixable || d.Rule == "MD064" && d.FixAvailable
				}
				if fixable != changed {
					t.Fatalf("fix availability %v, changed %v: %s", fixable, changed, lint.Stdout)
				}

				text := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=text")
				if strings.Contains(text.Stdout, "proposed doc.md") != changed {
					t.Fatal(text.Stdout)
				}
				preview := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json").Result
				if !preview.Complete || len(preview.Changes) > 0 != changed || preview.Files[0].ResultSnapshot.SHA256 != acceptance.Digest(tc.Candidate) {
					t.Fatalf("preview: %+v", preview)
				}
				if acceptance.Saved(t, root) != tc.Source {
					t.Fatal("preview changed the file")
				}

				saved := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
				out := acceptance.ValidateOperationJSON(t, []byte(saved.Stdout), "fix")
				if !out.Complete || len(out.Changes) > 0 != changed {
					t.Fatalf("saved: %s", saved.Stdout)
				}
				if got := acceptance.Saved(t, root); got != tc.Candidate {
					t.Fatalf("saved %q want %q", got, tc.Candidate)
				}
				// The preview reports the same candidate the saved run writes.
				if changed && (out.Changes[0].Candidate != preview.Changes[0].Candidate || out.Changes[0].EditCount != preview.Changes[0].EditCount || out.Changes[0].Disposition != "written") {
					t.Fatalf("saved %+v preview %+v", out.Changes[0], preview.Changes[0])
				}

				path := filepath.Join(root, "doc.md")
				identity, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				second := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
				again := acceptance.ValidateOperationJSON(t, []byte(second.Stdout), "fix")
				after, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if len(again.Changes) != 0 || acceptance.Saved(t, root) != tc.Candidate || !identity.ModTime().Equal(after.ModTime()) {
					t.Fatalf("second run changed file: %s", second.Stdout)
				}
				for _, d := range again.Diagnostics {
					if d.FixAvailable {
						t.Fatal("finding after fix advertises correction", d)
					}
				}
			})
		}
	}
}

func TestCompiledMD064FixAvailability(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, tc := range []struct {
		name, source string
		start, end   int
		fixable      bool
	}{
		{"prose", "One  two", 4, 5, true},
		{"code span", "`a  b`", 0, 0, false},
		{"heading", "# One  two\n", 6, 7, false},
		{"unrecognized front matter", "---\nOne  two\n---\n", 8, 9, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", md064Configuration("commonmark"))
			acceptance.PutFile(t, root, "doc.md", tc.source)
			lint := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
			var found []result.Diagnostic
			for _, d := range acceptance.ValidateOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics {
				if d.Rule == "MD064" {
					found = append(found, d)
				}
			}
			if tc.end == 0 {
				if len(found) != 0 {
					t.Fatal(found)
				}
				return
			}
			if len(found) != 1 || found[0].ByteRange.Start != tc.start || found[0].ByteRange.End != tc.end || found[0].FixAvailable != tc.fixable {
				t.Fatal(found)
			}
			if !tc.fixable {
				acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
				if acceptance.Saved(t, root) != tc.source {
					t.Fatal("manual finding corrected")
				}
			}
		})
	}
}

// The suppressed finding stays manual in the saved JSON report too.
func TestCompiledMD064SuppressedStaysManual(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	root := acceptance.Project(t)
	acceptance.PutFile(t, root, ".mdchk.yaml", correctionConfiguration("commonmark", 1, "directives"))
	source := "<!-- mdchk:disable MD064 -->\nOne  two\n"
	acceptance.PutFile(t, root, "doc.md", source)
	got := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
	out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
	if len(out.Changes) != 0 || len(out.SuppressedDiagnostics) != 1 || acceptance.Saved(t, root) != source {
		t.Fatal(got)
	}
	var suppressed result.SuppressedDiagnostic
	if err := json.Unmarshal(out.SuppressedDiagnostics[0], &suppressed); err != nil || suppressed.Diagnostic.FixAvailable {
		t.Fatal(string(out.SuppressedDiagnostics[0]), err)
	}
}
