package md050_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
)

// correctionConfiguration enables MD050 with style, plus the fixture's extra
// rules or directive processing, over the clean configuration.
func correctionConfiguration(flavor, style, variant string) string {
	config := strings.Replace(md050Configuration(flavor), "MD050: {enabled: true, style: consistent}", "MD050: {enabled: true, style: "+style+"}", 1)
	if variant == "directives" {
		config = strings.Replace(config, "directives: {enabled: false}", "directives: {enabled: true}", 1)
	}
	for _, id := range caseRules(variant) {
		config = strings.Replace(config, id+": {enabled: false}", id+": {enabled: true}", 1)
	}
	return config
}

func TestCompiledMD050FixWorkflow(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range correctionCases(t) {
			if tc.Flavor != "" && tc.Flavor != flavor {
				continue
			}
			t.Run(flavor+"/"+tc.Name, func(t *testing.T) {
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", correctionConfiguration(flavor, tc.Style, tc.Config))
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				changed := tc.Source != tc.Candidate

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
				manual := 0
				for _, d := range preview.Diagnostics {
					if d.Rule == "MD050" && !d.FixAvailable {
						manual++
					}
				}
				if manual != tc.Manual {
					t.Fatalf("preview manual %d: %+v", manual, preview.Diagnostics)
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

				// A second preview and a second saved run change nothing.
				again := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json").Result
				if len(again.Changes) != 0 || again.Files[0].ResultSnapshot.SHA256 != acceptance.Digest(tc.Candidate) {
					t.Fatalf("second preview: %+v", again)
				}
				path := filepath.Join(root, "doc.md")
				identity, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				second := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
				final := acceptance.ValidateOperationJSON(t, []byte(second.Stdout), "fix")
				after, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if len(final.Changes) != 0 || acceptance.Saved(t, root) != tc.Candidate || !identity.ModTime().Equal(after.ModTime()) {
					t.Fatalf("second run changed file: %s", second.Stdout)
				}
				for _, d := range final.Diagnostics {
					if d.FixAvailable {
						t.Fatal("finding after fix advertises correction", d)
					}
				}
			})
		}
	}
}

// lint and a dry run agree on fix availability, and the dry run previews
// exactly the two marker edits of each corrected Strong.
func TestCompiledMD050DryRunPreviewsTwoEdits(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	root := acceptance.Project(t)
	acceptance.PutFile(t, root, ".mdchk.yaml", md050Configuration("commonmark"))
	acceptance.PutFile(t, root, "doc.md", "**one** and __two__\n")
	lint := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
	found := acceptance.ValidateOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics
	if len(found) != 1 || found[0].ByteRange.Start != 12 || found[0].ByteRange.End != 14 || !found[0].FixAvailable {
		t.Fatal(lint.Stdout)
	}
	preview := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json").Result
	if !preview.Complete || len(preview.Changes) != 1 || preview.Changes[0].EditCount != 2 || preview.Files[0].ResultSnapshot.SHA256 != acceptance.Digest("**one** and **two**\n") {
		t.Fatalf("preview: %+v", preview)
	}
	if acceptance.Saved(t, root) != "**one** and __two__\n" {
		t.Fatal("preview changed the file")
	}
}

// Deferred and suppressed findings stay reported and manual in lint and fix.
func TestCompiledMD050ManualFindings(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, tc := range []struct {
		name, style, source string
	}{
		{"intraword", "underscore", "foo**bar**baz\n"},
		{"triple run", "consistent", "__a__ ***x***\n"},
		{"suppressed", "asterisk", "<!-- mdchk:disable MD050 -->\n__a__\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", correctionConfiguration("commonmark", tc.style, "directives"))
			acceptance.PutFile(t, root, "doc.md", tc.source)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"fix", "doc.md", "--format=json"}} {
				got := acceptance.RunProcess(t, binary, root, args...)
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
				if len(out.Changes) != 0 || acceptance.Saved(t, root) != tc.source {
					t.Fatal(got)
				}
				if tc.name == "suppressed" {
					if len(out.Diagnostics) != 0 || len(out.SuppressedDiagnostics) != 1 {
						t.Fatal(got)
					}
					continue
				}
				if len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD050" || out.Diagnostics[0].FixAvailable {
					t.Fatal(got)
				}
			}
		})
	}
}
