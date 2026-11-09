package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

// listSpacingConfiguration enables MD076 at style, directive processing and the
// fixture's variation over the ordinary production configuration.
func listSpacingConfiguration(flavor, style, variant string) string {
	config := strings.ReplaceAll(cleanConfiguration(), "directives: {enabled: false}", "directives: {enabled: true}")
	config = strings.Replace(config, "flavor: commonmark", "flavor: "+flavor, 1)
	config = strings.Replace(config, "MD076: {enabled: false}", "MD076: {enabled: true, style: "+style+"}", 1)
	if variant == "all" {
		for _, id := range []string{"MD009", "MD019", "MD021", "MD047"} {
			config = strings.Replace(config, id+": {enabled: false}", id+": {enabled: true}", 1)
		}
	}
	if variant == "frontmatter" {
		config = strings.Replace(config, "rules:\n", "frontmatter: {enabled: true}\nrules:\n", 1)
	}
	return config
}

// md076Remaining counts the MD076 findings left and fails on any that
// advertises a correction.
func md076Remaining(t *testing.T, diagnostics []result.Diagnostic) int {
	t.Helper()
	count := 0
	for _, d := range diagnostics {
		if d.Rule == "MD076" {
			count++
			if d.FixAvailable {
				t.Fatal("remaining finding advertised", d)
			}
		}
	}
	return count
}

func TestCompiledListSpacingFixWorkflow(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	data, err := os.ReadFile("testdata/list_spacing_corrections.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Style, Source, Candidate, Config string
		Rules                                  []string
		EditCount                              int `json:"edit_count"`
		Reported                               int
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range cases {
			t.Run(flavor+"/"+tc.Name, func(t *testing.T) {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", listSpacingConfiguration(flavor, tc.Style, tc.Config))
				putConfigurationFile(t, root, "doc.md", tc.Source)
				changed := tc.Source != tc.Candidate

				lint := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
				for _, d := range validateConfigurationOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics {
					if !changed && d.Rule == "MD076" && d.FixAvailable {
						t.Fatal("manual finding advertised", d)
					}
				}

				text := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=text")
				if strings.Contains(text.Stdout, "proposed doc.md") != changed {
					t.Fatal(text.Stdout)
				}
				before := configurationBytes(t, root)
				preview := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
				r := preview.Result
				if !r.Complete || len(r.Files) != 1 || len(r.Changes) > 0 != changed || r.Files[0].ResultSnapshot.SHA256 != spacingDigest(tc.Candidate) {
					t.Fatalf("preview: %+v", r)
				}
				if changed && (r.Changes[0].EditCount != tc.EditCount || !reflect.DeepEqual(r.Changes[0].Rules, tc.Rules)) {
					t.Fatalf("change: %+v", r.Changes[0])
				}
				if !reflect.DeepEqual(before, configurationBytes(t, root)) {
					t.Fatal("preview changed bytes")
				}
				for _, d := range r.Diagnostics {
					if d.Snapshot != *r.Files[0].ResultSnapshot {
						t.Fatal("remaining coordinates not candidate", d)
					}
				}
				if got := md076Remaining(t, r.Diagnostics); got != tc.Reported {
					t.Fatalf("remaining MD076 findings %d want %d", got, tc.Reported)
				}

				saved := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
				out := validateConfigurationOperationJSON(t, []byte(saved.Stdout), "fix")
				if !out.Complete || len(out.Changes) > 0 != changed {
					t.Fatalf("saved: %s", saved.Stdout)
				}
				if got := spacingSaved(t, root); got != tc.Candidate {
					t.Fatalf("saved %q want %q", got, tc.Candidate)
				}
				if changed && out.Changes[0].Disposition != "written" {
					t.Fatal(out.Changes[0])
				}
				if got := md076Remaining(t, out.Diagnostics); got != tc.Reported {
					t.Fatalf("saved MD076 findings %d want %d", got, tc.Reported)
				}

				path := filepath.Join(root, "doc.md")
				identity, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				second := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
				again := validateConfigurationOperationJSON(t, []byte(second.Stdout), "fix")
				after, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if len(again.Changes) != 0 || spacingSaved(t, root) != tc.Candidate || !os.SameFile(identity, after) || !identity.ModTime().Equal(after.ModTime()) {
					t.Fatalf("second run changed file: %s", second.Stdout)
				}
				if tc.Config == "all" {
					for _, d := range again.Diagnostics {
						if strings.Contains("MD009 MD019 MD021 MD047 MD076", d.Rule) {
							t.Fatal("finding remains after one run", d)
						}
					}
				}
				if got := md076Remaining(t, again.Diagnostics); got != tc.Reported {
					t.Fatalf("second run MD076 findings %d want %d", got, tc.Reported)
				}
			})
		}
	}
}
