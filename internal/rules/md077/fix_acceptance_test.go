package md077_test

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

// md077FixConfiguration enables MD077 with the rules it composes with and
// directive processing, plus the fixture's variation.
func md077FixConfiguration(flavor, variant string) string {
	config := strings.ReplaceAll(md077Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}")
	for _, rule := range []string{"MD009", "MD019", "MD021", "MD047"} {
		config = strings.Replace(config, rule+": {enabled: false}", rule+": {enabled: true}", 1)
	}
	if variant == "frontmatter" {
		config += "frontmatter: {enabled: true}\n"
	}
	return config
}

func TestCompiledMD077FixWorkflow(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	data, err := os.ReadFile("testdata/md077_corrections.json")
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
				acceptance.PutFile(t, root, ".mdchk.yaml", md077FixConfiguration(flavor, tc.Config))
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				changed := tc.Source != tc.Candidate

				lint := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
				reported := false
				for _, d := range acceptance.ValidateOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics {
					if d.Rule == "MD077" {
						reported = true
						if !changed && d.FixAvailable {
							t.Fatal("manual finding advertised", d)
						}
					}
				}
				if !reported && !strings.Contains(tc.Source, "mdchk:disable MD077") {
					t.Fatal("fixture has no MD077 finding", lint.Stdout)
				}

				before := acceptance.ConfigurationBytes(t, root)
				preview := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
				r := preview.Result
				if !r.Complete || len(r.Files) != 1 || len(r.Changes) > 0 != changed || r.Files[0].ResultSnapshot.SHA256 != acceptance.Digest(tc.Candidate) {
					t.Fatalf("preview: %+v", r)
				}
				if changed && (r.Changes[0].EditCount != tc.EditCount || !reflect.DeepEqual(r.Changes[0].Rules, tc.Rules)) {
					t.Fatalf("change: %+v", r.Changes[0])
				}
				if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal("dry run wrote the file")
				}
				for _, d := range r.Diagnostics {
					if d.Rule == "MD077" && d.FixAvailable {
						t.Fatal("remaining finding advertised", d)
					}
				}
				for _, raw := range r.SuppressedDiagnostics {
					var suppressed result.SuppressedDiagnostic
					if err := json.Unmarshal(raw, &suppressed); err != nil || suppressed.Diagnostic.FixAvailable {
						t.Fatal("suppressed finding advertised", string(raw), err)
					}
				}
				text := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=text")
				if strings.Contains(text.Stdout, "proposed doc.md") != changed {
					t.Fatal(text.Stdout)
				}

				saved := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
				out := acceptance.ValidateOperationJSON(t, []byte(saved.Stdout), "fix")
				if !out.Complete || len(out.Changes) > 0 != changed {
					t.Fatalf("saved: %s", saved.Stdout)
				}
				if got := acceptance.Saved(t, root); got != tc.Candidate {
					t.Fatalf("saved %q want %q", got, tc.Candidate)
				}
				if changed && out.Changes[0].Disposition != "written" {
					t.Fatal(out.Changes[0])
				}

				path := filepath.Join(root, "doc.md")
				identity, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				for _, args := range [][]string{{"fix", "--dry-run", "doc.md", "--format=json"}, {"fix", "doc.md", "--format=json"}} {
					second := acceptance.RunProcess(t, binary, root, args...)
					again := acceptance.ValidateOperationJSON(t, []byte(second.Stdout), "fix")
					if len(again.Changes) != 0 {
						t.Fatalf("second run changed file: %s", second.Stdout)
					}
				}
				after, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if acceptance.Saved(t, root) != tc.Candidate || !os.SameFile(identity, after) || !identity.ModTime().Equal(after.ModTime()) {
					t.Fatal("second run rewrote the file")
				}
			})
		}
	}
}
