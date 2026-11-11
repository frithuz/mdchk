package md038_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
	"github.com/frithuz/mdchk/internal/result"
)

// correctionConfiguration enables MD038 plus the fixture's extra rules or
// directive processing over the ordinary production configuration.
func correctionConfiguration(flavor, variant string) string {
	config := md038Configuration(flavor)
	switch variant {
	case "directives":
		config = strings.Replace(config, "directives: {enabled: false}", "directives: {enabled: true}", 1)
	case "md009":
		config = strings.Replace(config, "MD009: {enabled: false}", "MD009: {enabled: true}", 1)
		fallthrough
	case "md047":
		config = strings.Replace(config, "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	}
	return config
}

func TestCompiledMD038FixWorkflow(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range correctionCases(t) {
			if tc.Flavor != "" && tc.Flavor != flavor {
				continue
			}
			t.Run(flavor+"/"+tc.Name, func(t *testing.T) {
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", correctionConfiguration(flavor, tc.Config))
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				changed := tc.Source != tc.Candidate

				lint := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
				fixable := false
				for _, d := range acceptance.ValidateOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics {
					fixable = fixable || d.Rule == "MD038" && d.FixAvailable
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
				dry := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json").Result
				if len(dry.Changes) != 0 {
					t.Fatalf("second dry run proposes changes: %+v", dry)
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

func TestCompiledMD038FixAvailability(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, tc := range []struct {
		name, source string
		ranges       [][2]int
		fixable      bool
	}{
		{"single pair", "Use ` text ` here.\n", [][2]int{{5, 6}, {10, 11}}, true},
		{"reserved padding", "`  text  `\n", [][2]int{{1, 2}, {8, 9}}, false},
		{"leading backtick", "``  `a`  ``\n", [][2]int{{2, 3}, {8, 9}}, false},
		{"value starts with backtick", "`` `a` ``\n", nil, false},
		{"fenced", "```\n` text `\n```\n", nil, false},
		{"raw html", "<div>\n` text `\n</div>\n", nil, false},
		{"unrecognized leading block", "---\n` x `\n---\n", [][2]int{{5, 6}, {7, 8}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", md038Configuration("commonmark"))
			acceptance.PutFile(t, root, "doc.md", tc.source)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
				got := acceptance.RunProcess(t, binary, root, args...)
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
				var found []result.Diagnostic
				for _, d := range out.Diagnostics {
					if d.Rule == "MD038" {
						found = append(found, d)
					}
				}
				// The preview reports the candidate, which a correction leaves clean.
				want := tc.ranges
				if tc.fixable && args[0] == "fix" {
					want = nil
				}
				if len(found) != len(want) {
					t.Fatal(args, found)
				}
				for i, d := range found {
					if [2]int{d.ByteRange.Start, d.ByteRange.End} != want[i] || d.FixAvailable != tc.fixable {
						t.Fatal(args, d)
					}
				}
				if args[0] == "fix" && len(out.Changes) > 0 != tc.fixable {
					t.Fatal(got)
				}
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

// The suppressed findings stay manual and their bytes untouched in the saved
// JSON report.
func TestCompiledMD038SuppressedStaysManual(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	root := acceptance.Project(t)
	acceptance.PutFile(t, root, ".mdchk.yaml", correctionConfiguration("commonmark", "directives"))
	source := "<!-- mdchk:disable MD038 -->\n` text `\n"
	acceptance.PutFile(t, root, "doc.md", source)
	got := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
	out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
	if len(out.Changes) != 0 || len(out.SuppressedDiagnostics) != 2 || acceptance.Saved(t, root) != source {
		t.Fatal(got)
	}
	for _, raw := range out.SuppressedDiagnostics {
		var suppressed result.SuppressedDiagnostic
		if err := json.Unmarshal(raw, &suppressed); err != nil || suppressed.Diagnostic.FixAvailable {
			t.Fatal(string(raw), err)
		}
	}
}
