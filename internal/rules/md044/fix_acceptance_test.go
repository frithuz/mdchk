package md044_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
)

// correctionConfiguration enables MD044 with names and code_blocks, plus the
// fixture's extra rules or directive processing, over the clean configuration.
func correctionConfiguration(flavor string, names []string, code bool, variant string) string {
	quoted, _ := json.Marshal(names)
	config := strings.Replace(md044Configuration(flavor), "MD044: {enabled: true, names: [GitHub]}", fmt.Sprintf("MD044: {enabled: true, names: %s, code_blocks: %t}", quoted, code), 1)
	if variant == "directives" {
		config = strings.Replace(config, "directives: {enabled: false}", "directives: {enabled: true}", 1)
	}
	for _, id := range caseRules(variant) {
		config = strings.Replace(config, id+": {enabled: false}", id+": {enabled: true}", 1)
	}
	return config
}

func TestCompiledMD044FixWorkflow(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range correctionCases(t) {
			if tc.Flavor != "" && tc.Flavor != flavor {
				continue
			}
			t.Run(flavor+"/"+tc.Name, func(t *testing.T) {
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", correctionConfiguration(flavor, tc.Names, tc.CodeBlocks, tc.Config))
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

// A dry run previews exactly the one reported edit and writes nothing.
func TestCompiledMD044DryRunPreviewsOneEdit(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	root := acceptance.Project(t)
	acceptance.PutFile(t, root, ".mdchk.yaml", md044Configuration("commonmark"))
	acceptance.PutFile(t, root, "doc.md", "Use github.")
	lint := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
	found := acceptance.ValidateOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics
	if len(found) != 1 || found[0].ByteRange.Start != 4 || found[0].ByteRange.End != 10 || !found[0].FixAvailable {
		t.Fatal(lint.Stdout)
	}
	preview := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json").Result
	if !preview.Complete || len(preview.Changes) != 1 || preview.Changes[0].EditCount != 1 || preview.Files[0].ResultSnapshot.SHA256 != acceptance.Digest("Use GitHub.") {
		t.Fatalf("preview: %+v", preview)
	}
	if acceptance.Saved(t, root) != "Use github." {
		t.Fatal("preview changed the file")
	}
}

// Ambiguous spellings and code with code_blocks=true stay reported and manual.
func TestCompiledMD044ManualFindings(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, tc := range []struct {
		name, setting, source string
	}{
		{"ambiguous", "{enabled: true, names: [GitHub, Github]}", "Use GITHUB.\n"},
		{"code block", "{enabled: true, names: [GitHub], code_blocks: true}", "```\ngithub\n```\n"},
		{"suppressed", "{enabled: true, names: [GitHub]}", "<!-- mdchk:disable MD044 -->\ngithub\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := acceptance.Project(t)
			config := strings.Replace(md044Configuration("commonmark"), "MD044: {enabled: true, names: [GitHub]}", "MD044: "+tc.setting, 1)
			config = strings.Replace(config, "directives: {enabled: false}", "directives: {enabled: true}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", config)
			acceptance.PutFile(t, root, "doc.md", tc.source)
			got := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
			out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
			if len(out.Changes) != 0 || acceptance.Saved(t, root) != tc.source {
				t.Fatal(got)
			}
			if tc.name == "suppressed" {
				if len(out.SuppressedDiagnostics) != 1 {
					t.Fatal(got)
				}
				return
			}
			if len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD044" || out.Diagnostics[0].FixAvailable {
				t.Fatal(got)
			}
		})
	}
}
