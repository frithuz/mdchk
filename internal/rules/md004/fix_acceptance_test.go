package md004_test

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

// bulletMarkerConfiguration enables MD004, directive processing and the fixture's
// variation over the ordinary production configuration.
func bulletMarkerConfiguration(flavor, variant string) string {
	config := strings.ReplaceAll(acceptance.CleanConfiguration(), "directives: {enabled: false}", "directives: {enabled: true}")
	config = strings.Replace(config, "flavor: commonmark", "flavor: "+flavor, 1)
	marker := "consistent"
	switch variant {
	case "asterisk", "plus", "dash":
		marker = variant
	case "frontmatter":
		config = strings.Replace(config, "rules:\n", "frontmatter: {enabled: true}\nrules:\n", 1)
	case "all":
		for _, rule := range []string{"MD009", "MD019", "MD021", "MD047"} {
			config = strings.Replace(config, rule+": {enabled: false}", rule+": {enabled: true}", 1)
		}
	}
	return strings.Replace(config, "MD004: {enabled: false}", "MD004: {enabled: true, marker: "+marker+"}", 1)
}

func TestCompiledBulletMarkerFixWorkflow(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	data, err := os.ReadFile("testdata/md004_corrections.json")
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
				acceptance.PutFile(t, root, ".mdchk.yaml", bulletMarkerConfiguration(flavor, tc.Config))
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				changed := tc.Source != tc.Candidate

				lint := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
				for _, d := range acceptance.ValidateOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics {
					if !changed && d.Rule == "MD004" && d.FixAvailable {
						t.Fatal("manual finding advertised", d)
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
				for _, d := range r.Diagnostics {
					if d.Rule == "MD004" && d.FixAvailable {
						t.Fatal("remaining finding advertised", d)
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

				path := filepath.Join(root, "doc.md")
				identity, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				for _, args := range [][]string{{"fix", "--dry-run", "doc.md", "--format=json"}, {"fix", "doc.md", "--format=json"}} {
					second := acceptance.RunProcess(t, binary, root, args...)
					if again := acceptance.ValidateOperationJSON(t, []byte(second.Stdout), "fix"); len(again.Changes) != 0 {
						t.Fatalf("second run changed file: %s", second.Stdout)
					}
				}
				after, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if acceptance.Saved(t, root) != tc.Candidate || !os.SameFile(identity, after) || !identity.ModTime().Equal(after.ModTime()) {
					t.Fatal("second run rewrote file")
				}
			})
		}
	}
}

// TestCompiledBulletMarkerMergeStaysReported proves that a bullet whose
// correction would merge lists stays reported, before and after fix.
func TestCompiledBulletMarkerMergeStaysReported(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, source := range []string{"- a\n* b\n", "- a\n- b\n\n* c\n"} {
		root := acceptance.Project(t)
		acceptance.PutFile(t, root, ".mdchk.yaml", bulletMarkerConfiguration("commonmark", ""))
		acceptance.PutFile(t, root, "doc.md", source)
		for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "doc.md", "--format=json"}} {
			run := acceptance.RunProcess(t, binary, root, args...)
			var found []result.Diagnostic
			for _, d := range acceptance.ValidateOperationJSON(t, []byte(run.Stdout), args[0]).Diagnostics {
				if d.Rule == "MD004" {
					found = append(found, d)
				}
			}
			if len(found) != 1 || found[0].FixAvailable || found[0].ByteRange.End-found[0].ByteRange.Start != 1 {
				t.Fatalf("%s %q: %+v", args[0], source, found)
			}
		}
		if got := acceptance.Saved(t, root); got != source {
			t.Fatalf("saved %q", got)
		}
	}
}
