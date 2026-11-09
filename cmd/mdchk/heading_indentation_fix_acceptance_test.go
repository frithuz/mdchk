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

// headingIndentationConfiguration enables MD023, directive processing and the
// fixture's variation over the ordinary production configuration.
func headingIndentationConfiguration(flavor, variant string) string {
	config := strings.ReplaceAll(cleanConfiguration(), "directives: {enabled: false}", "directives: {enabled: true}")
	config = strings.Replace(config, "flavor: commonmark", "flavor: "+flavor, 1)
	config = strings.Replace(config, "MD023: {enabled: false}", "MD023: {enabled: true, allow_directives: true}", 1)
	if variant == "compose" {
		for _, id := range []string{"MD009", "MD019", "MD021", "MD047"} {
			config = strings.Replace(config, id+": {enabled: false}", id+": {enabled: true}", 1)
		}
	}
	if variant == "frontmatter" {
		config = strings.Replace(config, "rules:\n", "frontmatter: {enabled: true}\nrules:\n", 1)
	}
	return config
}

func TestCompiledHeadingIndentationFixWorkflow(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	data, err := os.ReadFile("testdata/heading_indentation_corrections.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Source, Candidate, Config string
		Rules                           []string
		EditCount                       int `json:"edit_count"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range cases {
			t.Run(flavor+"/"+tc.Name, func(t *testing.T) {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", headingIndentationConfiguration(flavor, tc.Config))
				putConfigurationFile(t, root, "doc.md", tc.Source)
				changed := tc.Source != tc.Candidate

				lint := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
				seen, _ := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
				for _, diagnostics := range [][]result.Diagnostic{validateConfigurationOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics, seen.Result.Diagnostics} {
					for _, d := range diagnostics {
						if !changed && d.Rule == "MD023" && d.FixAvailable {
							t.Fatal("manual finding advertised", d)
						}
					}
				}

				text := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=text")
				if strings.Contains(text.Stdout, "proposed doc.md") != changed {
					t.Fatal(text.Stdout)
				}
				preview := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
				r := preview.Result
				if !r.Complete || len(r.Files) != 1 || len(r.Changes) > 0 != changed || r.Files[0].ResultSnapshot.SHA256 != spacingDigest(tc.Candidate) {
					t.Fatalf("preview: %+v", r)
				}
				if changed && (r.Changes[0].EditCount != tc.EditCount || !reflect.DeepEqual(r.Changes[0].Rules, tc.Rules)) {
					t.Fatalf("change: %+v", r.Changes[0])
				}
				if len(r.DeferredFixes) != 0 {
					t.Fatalf("deferred: %+v", r.DeferredFixes)
				}
				for _, d := range r.Diagnostics {
					if d.Snapshot != *r.Files[0].ResultSnapshot {
						t.Fatal("remaining coordinates not candidate", d)
					}
					if d.Rule == "MD023" && d.FixAvailable {
						t.Fatal("remaining finding advertised", d)
					}
				}
				for _, raw := range r.SuppressedDiagnostics {
					var suppressed result.SuppressedDiagnostic
					if err := json.Unmarshal(raw, &suppressed); err != nil || suppressed.Diagnostic.FixAvailable {
						t.Fatal("suppressed finding advertised", string(raw), err)
					}
				}

				saved := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
				out := validateConfigurationOperationJSON(t, []byte(saved.Stdout), "fix")
				if !out.Complete || len(out.Changes) > 0 != changed {
					t.Fatalf("saved: %s", saved.Stdout)
				}
				path := filepath.Join(root, "doc.md")
				if got := spacingSaved(t, root); got != tc.Candidate {
					t.Fatalf("saved %q want %q", got, tc.Candidate)
				}
				if changed && (out.Changes[0].Disposition != "written" || out.Changes[0].EditCount != tc.EditCount || !reflect.DeepEqual(out.Changes[0].Rules, tc.Rules)) {
					t.Fatal(out.Changes[0])
				}

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
				final := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
				for _, d := range validateConfigurationOperationJSON(t, []byte(final.Stdout), "lint").Diagnostics {
					if d.FixAvailable {
						t.Fatal("lint after fix advertises correction", d)
					}
				}
			})
		}
	}
}

// The fix preview's diff deletes exactly the documented leading space.
func TestCompiledHeadingIndentationPreviewText(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", headingIndentationConfiguration("commonmark", ""))
	putConfigurationFile(t, root, "doc.md", " ## Install\n")
	text := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=text").Stdout
	if !strings.Contains(text, "- ## Install [lf]\n+## Install [lf]\n") || !strings.Contains(text, "1 validated edits (MD023)") {
		t.Fatal(text)
	}
	if spacingSaved(t, root) != " ## Install\n" {
		t.Fatal("preview wrote the file")
	}
}
