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

// md007FixConfiguration enables MD007 with the fixture's options and directive
// processing over the ordinary production configuration. The combined variant
// also enables every other correcting rule.
func md007FixConfiguration(variant string) string {
	config := strings.ReplaceAll(cleanConfiguration(), "directives: {enabled: false}", "directives: {enabled: true}")
	options := map[string]string{"": "", "indent4": ", indent: 4", "start": ", start_indented: true", "start4": ", indent: 4, start_indented: true", "combined": ""}[variant]
	config = strings.Replace(config, "MD007: {enabled: false}", "MD007: {enabled: true, allow_directives: true"+options+"}", 1)
	if variant == "combined" {
		for _, id := range []string{"MD009", "MD019", "MD021", "MD047"} {
			config = strings.Replace(config, id+": {enabled: false}", id+": {enabled: true}", 1)
		}
	}
	return config
}

func md007Saved(t *testing.T, root string) string {
	t.Helper()
	//nolint:gosec // Paths originate in the test-owned fixture tree.
	data, err := os.ReadFile(filepath.Join(root, "doc.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCompiledMD007FixWorkflow(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	data, err := os.ReadFile("testdata/md007_corrections.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Source, Candidate, Config string
		Rules                           []string
		EditCount                       int `json:"edit_count"`
		Fixable, Deferred               bool
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md007FixConfiguration(tc.Config))
			putConfigurationFile(t, root, "doc.md", tc.Source)
			changed := tc.Source != tc.Candidate

			lint := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
			for _, d := range validateConfigurationOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics {
				if d.Rule == "MD007" && d.FixAvailable != tc.Fixable {
					t.Fatal("fix availability", d)
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
			deferred := false
			for _, d := range r.DeferredFixes {
				deferred = deferred || d.Rule == "MD007"
			}
			if deferred != tc.Deferred {
				t.Fatalf("deferred: %+v", r.DeferredFixes)
			}
			for _, d := range r.Diagnostics {
				if d.Rule == "MD007" && changed {
					t.Fatal("remaining finding", d)
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
			if got := md007Saved(t, root); got != tc.Candidate {
				t.Fatalf("saved %q want %q", got, tc.Candidate)
			}
			if changed && out.Changes[0].Disposition != "written" {
				t.Fatal(out.Changes[0])
			}
			second := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
			if again := validateConfigurationOperationJSON(t, []byte(second.Stdout), "fix"); len(again.Changes) != 0 || md007Saved(t, root) != tc.Candidate {
				t.Fatalf("second run changed file: %s", second.Stdout)
			}
		})
	}
}

// One fix run corrects MD007, MD009, MD019, MD021 and MD047 together; the MD007
// shift takes a later round of the same run than the MD009 suffix.
func TestCompiledMD007FixComposesWithOtherCorrections(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	source := "#  Title  #\n\n- a \n   - b\n     more\n- c"
	want := "# Title #\n\n- a\n  - b\n    more\n- c\n"
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md007FixConfiguration("combined"))
	putConfigurationFile(t, root, "doc.md", source)
	lint := validateConfigurationOperationJSON(t, []byte(runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json").Stdout), "lint")
	rules := map[string]bool{}
	for _, d := range lint.Diagnostics {
		rules[d.Rule] = true
	}
	for _, id := range []string{"MD007", "MD009", "MD019", "MD021", "MD047"} {
		if !rules[id] {
			t.Fatal("missing finding", id, lint.Diagnostics)
		}
	}
	preview := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json").Result
	if !preview.Complete || len(preview.Changes) != 1 || preview.Files[0].ResultSnapshot.SHA256 != spacingDigest(want) || !reflect.DeepEqual(preview.Changes[0].Rules, []string{"MD007", "MD009", "MD019", "MD021", "MD047"}) || len(preview.DeferredFixes) != 0 {
		t.Fatalf("preview: %+v", preview)
	}
	saved := validateConfigurationOperationJSON(t, []byte(runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json").Stdout), "fix")
	if !saved.Complete || len(saved.Changes) != 1 || len(saved.Diagnostics) != 0 || md007Saved(t, root) != want {
		t.Fatalf("saved %q: %+v", md007Saved(t, root), saved)
	}
	again := validateConfigurationOperationJSON(t, []byte(runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json").Stdout), "fix")
	if len(again.Changes) != 0 || len(again.Diagnostics) != 0 || md007Saved(t, root) != want {
		t.Fatalf("second run: %+v", again)
	}
}
