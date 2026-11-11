package md023_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
	"github.com/frithuz/mdchk/internal/result"
)

// headingIndentationConfiguration enables MD023, directive processing and the
// fixture's variation over the ordinary production configuration.
func headingIndentationConfiguration(flavor, variant string) string {
	config := strings.ReplaceAll(acceptance.CleanConfiguration(), "directives: {enabled: false}", "directives: {enabled: true}")
	config = strings.Replace(config, "flavor: commonmark", "flavor: "+flavor, 1)
	config = strings.Replace(config, "MD023: {enabled: false}", "MD023: {enabled: true, allow_directives: true}", 1)
	if variant == "compose" {
		for _, id := range []string{"MD009", "MD019", "MD021", "MD047"} {
			config = strings.Replace(config, id+": {enabled: false}", id+": {enabled: true}", 1)
		}
	}
	if variant == "blanks" {
		config = strings.Replace(config, "MD022: {enabled: false}", "MD022: {enabled: true}", 1)
	}
	if variant == "frontmatter" {
		config = strings.Replace(config, "rules:\n", "frontmatter: {enabled: true}\nrules:\n", 1)
	}
	return config
}

func TestCompiledHeadingIndentationFixWorkflow(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
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
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", headingIndentationConfiguration(flavor, tc.Config))
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				changed := tc.Source != tc.Candidate

				lint := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
				seen, _ := acceptance.RunInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
				for _, diagnostics := range [][]result.Diagnostic{acceptance.ValidateOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics, seen.Result.Diagnostics} {
					for _, d := range diagnostics {
						if !changed && d.Rule == "MD023" && d.FixAvailable {
							t.Fatal("manual finding advertised", d)
						}
					}
				}

				text := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=text")
				if strings.Contains(text.Stdout, "proposed doc.md") != changed {
					t.Fatal(text.Stdout)
				}
				preview := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
				r := preview.Result
				if !r.Complete || len(r.Files) != 1 || len(r.Changes) > 0 != changed || r.Files[0].ResultSnapshot.SHA256 != acceptance.Digest(tc.Candidate) {
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

				saved := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
				out := acceptance.ValidateOperationJSON(t, []byte(saved.Stdout), "fix")
				if !out.Complete || len(out.Changes) > 0 != changed {
					t.Fatalf("saved: %s", saved.Stdout)
				}
				path := filepath.Join(root, "doc.md")
				if got := acceptance.Saved(t, root); got != tc.Candidate {
					t.Fatalf("saved %q want %q", got, tc.Candidate)
				}
				if changed && (out.Changes[0].Disposition != "written" || out.Changes[0].EditCount != tc.EditCount || !reflect.DeepEqual(out.Changes[0].Rules, tc.Rules)) {
					t.Fatal(out.Changes[0])
				}

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
				if len(again.Changes) != 0 || acceptance.Saved(t, root) != tc.Candidate || !os.SameFile(identity, after) || !identity.ModTime().Equal(after.ModTime()) {
					t.Fatalf("second run changed file: %s", second.Stdout)
				}
				final := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
				for _, d := range acceptance.ValidateOperationJSON(t, []byte(final.Stdout), "lint").Diagnostics {
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
	binary := acceptance.SharedExecutable(t)
	root := acceptance.Project(t)
	acceptance.PutFile(t, root, ".mdchk.yaml", headingIndentationConfiguration("commonmark", ""))
	acceptance.PutFile(t, root, "doc.md", " ## Install\n")
	text := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=text").Stdout
	if !strings.Contains(text, "- ## Install [lf]\n+## Install [lf]\n") || !strings.Contains(text, "1 validated edits (MD023)") {
		t.Fatal(text)
	}
	if acceptance.Saved(t, root) != " ## Install\n" {
		t.Fatal("preview wrote the file")
	}
}

// Issue #377: MD022's blank line and MD023's indentation deletion start at the
// same offset. MD022 wins the conflict by catalog order, and MD023's deletion
// waits for the next round instead of being dropped.
func TestCompiledHeadingIndentationAndBlankLinesConflict(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	const source, want = "Text\n ## Install\nText\n", "Text\n\n## Install\n\nText\n"
	for _, flavor := range []string{"commonmark", "gfm"} {
		root := acceptance.Project(t)
		acceptance.PutFile(t, root, ".mdchk.yaml", headingIndentationConfiguration(flavor, "blanks"))
		acceptance.PutFile(t, root, "doc.md", source)
		path := filepath.Join(root, "doc.md")
		first := acceptance.RunFix(t, binary, root, "fix", "doc.md", "--format=json")
		//nolint:gosec // Access only synthetic test fixtures.
		saved, err := os.ReadFile(path)
		if err != nil || string(saved) != want || first.Status != 0 || len(first.Result.Changes) != 1 || !reflect.DeepEqual(first.Result.Changes[0].Rules, []string{"MD022", "MD023"}) {
			t.Fatalf("saved %q want %q: %v %s", saved, want, err, first.Stdout)
		}
		for _, d := range first.Result.Diagnostics {
			if d.Rule == "MD022" || d.Rule == "MD023" {
				t.Fatalf("finding left: %+v", d)
			}
		}
		if len(first.Result.DeferredFixes) != 0 {
			t.Fatalf("deferred: %+v", first.Result.DeferredFixes)
		}
		second := acceptance.RunFix(t, binary, root, "fix", "doc.md", "--format=json")
		//nolint:gosec // Access only synthetic test fixtures.
		again, err := os.ReadFile(path)
		if err != nil || string(again) != want || second.Status != 0 || len(second.Result.Changes) != 0 {
			t.Fatalf("second run %q: %v %s", again, err, second.Stdout)
		}
	}
}
