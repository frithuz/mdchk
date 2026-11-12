package md088_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
)

// correctionConfiguration enables MD088 plus the fixture's extra rules, front
// matter or directive processing, over the clean configuration.
func correctionConfiguration(flavor, variant string) string {
	config := md088Configuration(flavor)
	switch variant {
	case "directives":
		config = strings.Replace(config, "directives: {enabled: false}", "directives: {enabled: true}", 1)
	case "frontmatter":
		config = strings.Replace(config, "rules:\n", "frontmatter: {enabled: true}\nrules:\n", 1)
	}
	for _, id := range caseRules(variant) {
		config = strings.Replace(config, id+": {enabled: false}", id+": {enabled: true}", 1)
	}
	return config
}

func TestCompiledMD088FixWorkflow(t *testing.T) {
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
					if d.Rule == "MD088" && !d.FixAvailable {
						manual++
					}
				}
				if manual != tc.Manual {
					t.Fatalf("preview manual %d: %+v", manual, preview.Diagnostics)
				}
				for _, d := range preview.DeferredFixes {
					if d.Rule == "MD088" {
						t.Fatalf("deferred MD088 fix: %+v", d)
					}
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

// lint reports the page example's three findings as fixable, a dry run with
// MD047 previews the corrected content without writing it, fix writes it and a
// second fix leaves the file untouched.
func TestCompiledMD088PageExample(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	root := acceptance.Project(t)
	acceptance.PutFile(t, root, ".mdchk.yaml", correctionConfiguration("commonmark", "md047"))
	acceptance.PutFile(t, root, "doc.md", "“Guide”—example")
	lint := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
	found := acceptance.ValidateOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics
	var scalars [][2]int
	for _, d := range found {
		if d.Rule == "MD088" {
			if !d.FixAvailable {
				t.Fatal(lint.Stdout)
			}
			scalars = append(scalars, [2]int{d.ByteRange.Start, d.ByteRange.End})
		}
	}
	if len(scalars) != 3 || scalars[0] != [2]int{0, 3} || scalars[1] != [2]int{8, 11} || scalars[2] != [2]int{11, 14} {
		t.Fatal(lint.Stdout)
	}
	preview := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json").Result
	if !preview.Complete || len(preview.Changes) != 1 || preview.Files[0].ResultSnapshot.SHA256 != acceptance.Digest("\"Guide\"--example\n") {
		t.Fatalf("preview: %+v", preview)
	}
	if acceptance.Saved(t, root) != "“Guide”—example" {
		t.Fatal("preview changed the file")
	}
	saved := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
	if out := acceptance.ValidateOperationJSON(t, []byte(saved.Stdout), "fix"); !out.Complete || len(out.Changes) != 1 || len(out.Diagnostics) != 0 || acceptance.Saved(t, root) != "\"Guide\"--example\n" {
		t.Fatalf("saved: %s", saved.Stdout)
	}
	path := filepath.Join(root, "doc.md")
	identity, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	second := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if out := acceptance.ValidateOperationJSON(t, []byte(second.Stdout), "fix"); len(out.Changes) != 0 || !identity.ModTime().Equal(after.ModTime()) {
		t.Fatalf("second run: %s", second.Stdout)
	}
}

// Deferred and suppressed findings stay reported and manual in lint and fix,
// with no MD088 deferral record and the file unchanged.
func TestCompiledMD088ManualFindings(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, tc := range []struct {
		name, source, flavor string
		manual               int
	}{
		{"list marker", "– item\n", "", 1},
		{"setext underline", "a\n———\n", "", 3},
		{"thematic break", "– – –\n", "", 3},
		{"table delimiter cell", "| a |\n| – |\n", "gfm", 1},
		{"comment opener", "<!—x\n", "", 1},
		{"escape", "\\“a\n", "", 1},
		{"autolink domain", "www.example.com.—x\n", "gfm", 1},
		{"autolink email", "a—b@example.com\n", "gfm", 1},
		{"autolink owns the dash", "www.example.com—x\n", "gfm", 0},
		{"heading dash", "# A — B\n", "", 1},
		{"suppressed", "<!-- mdchk:disable MD088 -->\n“a”\n", "", 0},
	} {
		for _, flavor := range []string{"commonmark", "gfm"} {
			if tc.flavor != "" && tc.flavor != flavor {
				continue
			}
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", correctionConfiguration(flavor, "directives"))
				acceptance.PutFile(t, root, "doc.md", tc.source)
				for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"fix", "doc.md", "--format=json"}} {
					got := acceptance.RunProcess(t, binary, root, args...)
					out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
					if !out.Complete || len(out.Changes) != 0 || len(out.DeferredFixes) != 0 || acceptance.Saved(t, root) != tc.source {
						t.Fatal(got)
					}
					if tc.name == "suppressed" && len(out.SuppressedDiagnostics) != 2 {
						t.Fatal(got)
					}
					if len(out.Diagnostics) != tc.manual {
						t.Fatal(got)
					}
					for _, d := range out.Diagnostics {
						if d.Rule != "MD088" || d.FixAvailable {
							t.Fatal(got)
						}
					}
				}
			})
		}
	}
}
