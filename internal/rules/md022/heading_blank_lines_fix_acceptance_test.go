package md022_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
)

// headingBlankConfiguration enables MD022, directive processing and the fixture's
// variation over the ordinary production configuration.
func headingBlankConfiguration(flavor, variant string) string {
	config := strings.ReplaceAll(acceptance.CleanConfiguration(), "directives: {enabled: false}", "directives: {enabled: true}")
	config = strings.Replace(config, "flavor: commonmark", "flavor: "+flavor, 1)
	options := ""
	switch variant {
	case "two":
		options = ", before: 2, after: 2"
	case "before-zero":
		options = ", before: 0"
	case "after-zero":
		options = ", after: 0"
	case "frontmatter":
		config = strings.Replace(config, "rules:\n", "frontmatter: {enabled: true}\nrules:\n", 1)
	case "all":
		for _, id := range []string{"MD009", "MD019", "MD021", "MD047"} {
			config = strings.Replace(config, id+": {enabled: false}", id+": {enabled: true}", 1)
		}
	}
	return strings.Replace(config, "MD022: {enabled: false}", "MD022: {enabled: true, allow_directives: true"+options+"}", 1)
}

func TestCompiledHeadingBlankLinesFixWorkflow(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	data, err := os.ReadFile("testdata/heading_blank_lines_corrections.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Source, Candidate, Config string
		Rules                           []string
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range cases {
			t.Run(flavor+"/"+tc.Name, func(t *testing.T) {
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", headingBlankConfiguration(flavor, tc.Config))
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				changed := tc.Source != tc.Candidate

				lint := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
				for _, d := range acceptance.ValidateOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics {
					if !changed && d.Rule == "MD022" && d.FixAvailable {
						t.Fatal("manual finding advertised", d)
					}
				}

				preview := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json").Result
				if !preview.Complete || len(preview.Changes) > 0 != changed || preview.Files[0].ResultSnapshot.SHA256 != acceptance.Digest(tc.Candidate) || acceptance.Saved(t, root) != tc.Source {
					t.Fatalf("preview: %+v", preview)
				}
				if changed && !reflect.DeepEqual(preview.Changes[0].Rules, tc.Rules) {
					t.Fatalf("change: %+v", preview.Changes[0])
				}
				for _, d := range preview.Diagnostics {
					if d.Rule == "MD022" && (changed || d.FixAvailable) {
						t.Fatal("remaining finding", d)
					}
				}

				saved := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
				out := acceptance.ValidateOperationJSON(t, []byte(saved.Stdout), "fix")
				if !out.Complete || len(out.Changes) > 0 != changed || acceptance.Saved(t, root) != tc.Candidate {
					t.Fatalf("saved %q: %s", acceptance.Saved(t, root), saved.Stdout)
				}

				identity, err := os.Stat(filepath.Join(root, "doc.md"))
				if err != nil {
					t.Fatal(err)
				}
				for _, args := range [][]string{{"fix", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
					second := acceptance.RunProcess(t, binary, root, args...)
					again := acceptance.ValidateOperationJSON(t, []byte(second.Stdout), "fix")
					after, err := os.Stat(filepath.Join(root, "doc.md"))
					if err != nil {
						t.Fatal(err)
					}
					if len(again.Changes) != 0 || acceptance.Saved(t, root) != tc.Candidate || !os.SameFile(identity, after) || !identity.ModTime().Equal(after.ModTime()) {
						t.Fatalf("second run changed file: %s", second.Stdout)
					}
				}
			})
		}
	}
}
