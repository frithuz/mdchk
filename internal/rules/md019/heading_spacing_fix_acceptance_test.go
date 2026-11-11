package md019_test

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

// headingSpacingConfiguration enables both spacing rules, directive processing
// and the fixture's variation over the ordinary production configuration.
func headingSpacingConfiguration(flavor, variant string) string {
	config := strings.ReplaceAll(acceptance.CleanConfiguration(), "directives: {enabled: false}", "directives: {enabled: true}")
	config = strings.Replace(config, "flavor: commonmark", "flavor: "+flavor, 1)
	md019, md021 := "true", "true"
	if variant == "md019-off" || variant == "both-off" {
		md019 = "false"
	}
	if variant == "md021-off" || variant == "both-off" {
		md021 = "false"
	}
	allowed := "true"
	if variant == "no-permission" {
		allowed = "false"
	}
	config = strings.Replace(config, "MD019: {enabled: false}", "MD019: {enabled: "+md019+", allow_directives: "+allowed+"}", 1)
	config = strings.Replace(config, "MD021: {enabled: false}", "MD021: {enabled: "+md021+", allow_directives: "+allowed+"}", 1)
	if variant == "md047" {
		config = strings.Replace(config, "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	}
	if variant == "frontmatter" {
		config = strings.Replace(config, "rules:\n", "frontmatter: {enabled: true}\nrules:\n", 1)
	}
	return config
}

func TestCompiledHeadingSpacingFixWorkflow(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	data, err := os.ReadFile("testdata/heading_spacing_corrections.json")
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
				acceptance.PutFile(t, root, ".mdchk.yaml", headingSpacingConfiguration(flavor, tc.Config))
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				changed := tc.Source != tc.Candidate

				lint := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
				seen, _ := acceptance.RunInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
				for _, diagnostics := range [][]result.Diagnostic{acceptance.ValidateOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics, seen.Result.Diagnostics} {
					for _, d := range diagnostics {
						if !changed && (d.Rule == "MD019" || d.Rule == "MD021") && d.FixAvailable {
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
				for _, d := range r.Diagnostics {
					if d.Snapshot != *r.Files[0].ResultSnapshot {
						t.Fatal("remaining coordinates not candidate", d)
					}
					if (d.Rule == "MD019" || d.Rule == "MD021") && d.FixAvailable {
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
				if changed && out.Changes[0].Disposition != "written" {
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
					if (d.Rule == "MD019" || d.Rule == "MD021") && d.FixAvailable {
						t.Fatal("lint after fix advertises correction", d)
					}
				}
			})
		}
	}
}

func TestCompiledHeadingSpacingSharedEditDiagnostics(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	root := acceptance.Project(t)
	acceptance.PutFile(t, root, ".mdchk.yaml", headingSpacingConfiguration("commonmark", ""))
	acceptance.PutFile(t, root, "doc.md", "##  Title ##\n")
	lint := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
	seen, _ := acceptance.RunInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
	for _, diagnostics := range [][]result.Diagnostic{acceptance.ValidateOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics, seen.Result.Diagnostics} {
		if len(diagnostics) != 2 || diagnostics[0].Rule == diagnostics[1].Rule || !diagnostics[0].FixAvailable || !diagnostics[1].FixAvailable {
			t.Fatalf("%+v", diagnostics)
		}
	}
}

func TestCompiledHeadingSpacingMD047Deferral(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, tc := range []struct {
		name, flavor, source, candidate string
		deferred                        bool
	}{
		{"unterminated fence", "commonmark", "##  Title\n\n```\ncode", "## Title\n\n```\ncode", true},
		{"literal task marker", "gfm", "##  Title\n\n- [x]", "## Title\n\n- [x]", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", headingSpacingConfiguration(tc.flavor, "md047"))
			acceptance.PutFile(t, root, "doc.md", tc.source)
			r := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json").Result
			if len(r.Changes) != 1 || r.Files[0].ResultSnapshot.SHA256 != acceptance.Digest(tc.candidate) || !reflect.DeepEqual(r.Changes[0].Rules, []string{"MD019"}) {
				t.Fatalf("%+v", r)
			}
			found := false
			for _, d := range r.DeferredFixes {
				found = found || d.Rule == "MD047"
			}
			if found != tc.deferred {
				t.Fatalf("deferred: %+v", r.DeferredFixes)
			}
			terminal := false
			for _, d := range r.Diagnostics {
				if d.Rule == "MD047" {
					terminal = true
					if d.FixAvailable {
						t.Fatal("deferred correction advertised", d)
					}
				}
			}
			if !terminal {
				t.Fatal("MD047 no longer reported", r.Diagnostics)
			}
		})
	}
}

func TestCompiledHeadingSpacingScale(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	root := acceptance.Project(t)
	acceptance.PutFile(t, root, ".mdchk.yaml", headingSpacingConfiguration("commonmark", ""))
	source := spacingScaleSource(500)
	acceptance.PutFile(t, root, "doc.md", source)
	got := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
	out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
	want := strings.ReplaceAll(strings.ReplaceAll(source, "##  Title", "## Title"), "x  ##", "x ##")
	if got.Status != 0 || !out.Complete || len(out.Changes) != 1 || out.Changes[0].EditCount != 1000 || !reflect.DeepEqual(out.Changes[0].Rules, []string{"MD019", "MD021"}) {
		t.Fatalf("status %d: %+v", got.Status, out)
	}
	if acceptance.Saved(t, root) != want {
		t.Fatal("not every heading corrected")
	}

	source = spacingScaleSource(5000)
	acceptance.PutFile(t, root, "doc.md", source)
	// Pinning the limits keeps them hard caps; unpinned allowances grow with content.
	got = acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json", "--limit", "work-units=250000000", "--limit", "total-nodes=2000000")
	out = acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
	if got.Status != 15 || out.Complete {
		t.Fatalf("status %d complete %v", got.Status, out.Complete)
	}
	if acceptance.Saved(t, root) != source {
		t.Fatal("over-budget document changed")
	}
}

// Every other heading and list rule stays report-only in this slice.
func TestCompiledHeadingSpacingOtherRulesProduceNothing(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, tc := range []struct {
		rule, options, source string
	}{
		{"MD001", "", "# a\n\n### b\n"},
		{"MD018", "", "#a\n"},
		{"MD020", "", "# a#\n"},
		{"MD024", "", "# a\n\n# a\n"},
		{"MD025", "", "# a\n\n# b\n"},
		{"MD026", "", "# a.\n"},
		{"MD036", "", "**a**\n\nbody text\n"},
		{"MD041", "", "a\n"},
		{"MD043", ", headings: ['# x']", "# a\n"},
		{"MD080", "", "# a b\n\n# a-b\n"},
		{"MD082", "", "# a\n\n## b\n"},
		{"MD093", "", "# *a*\n"},
		{"MD004", "", "- a\n* b\n"},
		{"MD005", "", "- a\n - b\n   c\n"}, // continuation lines keep MD005 manual
		{"MD030", "", "-  a\n   b\n"},
		{"MD069", "", "- - a\n"},
	} {
		t.Run(tc.rule, func(t *testing.T) {
			root := acceptance.Project(t)
			config := strings.Replace(acceptance.CleanConfiguration(), tc.rule+": {enabled: false}", tc.rule+": {enabled: true"+tc.options+"}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", config)
			acceptance.PutFile(t, root, "doc.md", tc.source)
			before := acceptance.ConfigurationBytes(t, root)
			lint := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
			found := acceptance.ValidateOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics
			if len(found) == 0 {
				t.Fatal("fixture does not violate the rule", lint.Stdout)
			}
			saved := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
			out := acceptance.ValidateOperationJSON(t, []byte(saved.Stdout), "fix")
			if len(out.Changes) != 0 || !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
				t.Fatalf("changed: %s", saved.Stdout)
			}
			for _, d := range append(found, out.Diagnostics...) {
				if d.FixAvailable {
					t.Fatal("report-only rule advertised a correction", d)
				}
			}
		})
	}
}
