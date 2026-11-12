package md049_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
)

// correctionConfiguration enables MD049 with style, plus the fixture's extra
// rules or directive processing, over the clean configuration.
func correctionConfiguration(flavor, style, variant string) string {
	config := strings.Replace(md049Configuration(flavor), "MD049: {enabled: true, style: consistent}", "MD049: {enabled: true, style: "+style+"}", 1)
	if variant == "directives" {
		config = strings.Replace(config, "directives: {enabled: false}", "directives: {enabled: true}", 1)
	}
	for _, id := range caseRules(variant) {
		config = strings.Replace(config, id+": {enabled: false}", id+": {enabled: true}", 1)
	}
	return config
}

func TestCompiledMD049FixWorkflow(t *testing.T) {
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

// A dry run previews exactly the two marker edits and writes nothing.
func TestCompiledMD049DryRunPreviewsOnePair(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	root := acceptance.Project(t)
	acceptance.PutFile(t, root, ".mdchk.yaml", md049Configuration("commonmark"))
	acceptance.PutFile(t, root, "doc.md", "*one* and _two_\n")
	lint := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
	found := acceptance.ValidateOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics
	if len(found) != 1 || found[0].ByteRange.Start != 10 || found[0].ByteRange.End != 11 || !found[0].FixAvailable {
		t.Fatal(lint.Stdout)
	}
	preview := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json").Result
	if !preview.Complete || len(preview.Changes) != 1 || preview.Changes[0].EditCount != 2 || preview.Files[0].ResultSnapshot.SHA256 != acceptance.Digest("*one* and *two*\n") {
		t.Fatalf("preview: %+v", preview)
	}
	if acceptance.Saved(t, root) != "*one* and _two_\n" {
		t.Fatal("preview changed the file")
	}
}

// Unsafe rewrites and suppressed or split directive italics stay reported and
// manual.
func TestCompiledMD049ManualFindings(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, tc := range []struct {
		name, style, source string
		suppressed          int
	}{
		{"intraword", "underscore", "a*b*c\n", 0},
		{"flanking interaction", "consistent", "*a* _x *y_ z*\n", 0},
		{"strong around italic", "consistent", "*a* **_b_**\n", 0},
		{"suppressed", "consistent", "*a*\n\n<!-- mdchk:disable MD049 -->\n_b_\n", 1},
		{"closing disabled", "consistent", "*a*\n\n_b <!-- mdchk:disable MD049 --> c_\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", correctionConfiguration("commonmark", tc.style, "directives"))
			acceptance.PutFile(t, root, "doc.md", tc.source)
			got := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
			out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
			if len(out.Changes) != 0 || acceptance.Saved(t, root) != tc.source || len(out.SuppressedDiagnostics) != tc.suppressed {
				t.Fatal(got)
			}
			if tc.suppressed > 0 {
				return
			}
			if len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD049" || out.Diagnostics[0].FixAvailable {
				t.Fatal(got)
			}
		})
	}
}
