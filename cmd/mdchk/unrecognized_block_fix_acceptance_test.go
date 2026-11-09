package main

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

// unrecognizedBlockConfiguration returns the fixture's configuration. "default"
// means no file for GFM and the minimal file for CommonMark, whose recognition is
// the default in both cases.
func unrecognizedBlockConfiguration(flavor, variant string) string {
	config := "version: 1\nflavor: " + flavor + "\n"
	switch variant {
	case "toml-only":
		return config + "frontmatter: {enabled: true, formats: [toml]}\n"
	case "yaml-only":
		return config + "frontmatter: {enabled: true, formats: [yaml]}\n"
	case "enabled":
		return config + "frontmatter: {enabled: true}\n"
	}
	if flavor == "gfm" {
		return ""
	}
	return config
}

// checkWithheld requires every listed finding, and refuses any correction flag on
// the rules the guard withholds.
func checkWithheld(t *testing.T, label string, diagnostics []result.Diagnostic, withheld []string) {
	t.Helper()
	seen := map[string]bool{}
	for _, d := range diagnostics {
		seen[d.Rule] = true
		if (d.Rule == "MD009" || d.Rule == "MD019" || d.Rule == "MD021" || d.Rule == "MD022") && d.FixAvailable {
			t.Fatalf("%s advertised a withheld correction: %+v", label, d)
		}
	}
	for _, rule := range withheld {
		if !seen[rule] {
			t.Fatalf("%s lost the %s finding: %+v", label, rule, diagnostics)
		}
	}
}

func TestCompiledUnrecognizedBlockFix(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	data, err := os.ReadFile("testdata/unrecognized_block_corrections.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Source, Candidate, Config string
		Guarded                         bool
		Withheld                        []string
		FixStatus                       *int `json:"fix_status"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range cases {
			t.Run(flavor+"/"+tc.Name, func(t *testing.T) {
				root := configurationProject(t)
				if config := unrecognizedBlockConfiguration(flavor, tc.Config); config != "" {
					putConfigurationFile(t, root, ".mdchk.yaml", config)
				}
				putConfigurationFile(t, root, "doc.md", tc.Source)
				changed := tc.Source != tc.Candidate

				lint := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
				if tc.Guarded {
					checkWithheld(t, "lint", validateConfigurationOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics, tc.Withheld)
				}

				preview := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json").Result
				if !preview.Complete || len(preview.Changes) > 0 != changed || preview.Files[0].ResultSnapshot.SHA256 != spacingDigest(tc.Candidate) {
					t.Fatalf("preview: %+v", preview)
				}
				if tc.Guarded {
					checkWithheld(t, "preview", preview.Diagnostics, tc.Withheld)
					if len(preview.DeferredFixes) != 0 {
						t.Fatalf("preview deferred: %+v", preview.DeferredFixes)
					}
					for _, change := range preview.Changes {
						if !slices.Equal(change.Rules, []string{"MD047"}) {
							t.Fatalf("guarded change credits other rules: %+v", change)
						}
					}
				}

				run := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
				saved := validateConfigurationOperationJSON(t, []byte(run.Stdout), "fix")
				if got := spacingSaved(t, root); got != tc.Candidate {
					t.Fatalf("saved %q want %q", got, tc.Candidate)
				}
				if !saved.Complete || len(saved.Changes) > 0 != changed {
					t.Fatalf("saved: %s", run.Stdout)
				}
				if tc.FixStatus != nil && run.Status != *tc.FixStatus {
					t.Fatalf("fix status %d want %d", run.Status, *tc.FixStatus)
				}
				if tc.Guarded {
					checkWithheld(t, "saved", saved.Diagnostics, tc.Withheld)
					if len(saved.DeferredFixes) != 0 {
						t.Fatalf("saved deferred: %+v", saved.DeferredFixes)
					}
				}

				again := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
				if out := validateConfigurationOperationJSON(t, []byte(again.Stdout), "fix"); len(out.Changes) != 0 || spacingSaved(t, root) != tc.Candidate {
					t.Fatalf("second run changed file: %s", again.Stdout)
				}
			})
		}
	}
}

func TestCompiledUnrecognizedBlockInspection(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	root := configurationProject(t)
	putConfigurationFile(t, root, "doc.md", "---\n#  x\ntitle: y\n---\n\nbody\n")
	seen, projection := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
	checkWithheld(t, "inspect", seen.Result.Diagnostics, []string{"MD019"})
	if projection.AST.Data == nil || projection.AST.Data.Frontmatter != nil {
		t.Fatalf("inspection gained a front matter record: %s", seen.Stdout)
	}
}
