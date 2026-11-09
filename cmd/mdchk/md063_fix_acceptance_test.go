package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

// headingCaseConfiguration enables MD063 with directive processing, plus the
// named companion rules, over the ordinary production configuration.
func headingCaseConfiguration(t *testing.T, flavor, style string, names []string, frontmatter bool, rules ...string) string {
	t.Helper()
	if names == nil {
		names = []string{}
	}
	raw, err := json.Marshal(map[string]any{"enabled": true, "style": style, "proper_names": names})
	if err != nil {
		t.Fatal(err)
	}
	config := strings.ReplaceAll(cleanConfiguration(), "directives: {enabled: false}", "directives: {enabled: true}")
	config = strings.Replace(config, "flavor: commonmark", "flavor: "+flavor, 1)
	config = strings.Replace(config, "MD063: {enabled: false}", "MD063: "+string(raw), 1)
	for _, id := range rules {
		config = strings.Replace(config, id+": {enabled: false}", id+": {enabled: true}", 1)
	}
	if frontmatter {
		config = strings.Replace(config, "rules:\n", "frontmatter: {enabled: true}\nrules:\n", 1)
	}
	return config
}

func headingCaseFindings(diagnostics []result.Diagnostic) []result.Diagnostic {
	var out []result.Diagnostic
	for _, d := range diagnostics {
		if d.Rule == "MD063" {
			out = append(out, d)
		}
	}
	return out
}

func TestCompiledHeadingCaseFixWorkflow(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	composed := []string{"MD009", "MD019", "MD021", "MD047"}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source, candidate, style string
			names                          []string
			frontmatter                    bool
			rules                          []string
		}{
			{"sentence", "## installation guide\n", "## Installation guide\n", "sentence", nil, false, nil},
			{"title", "# the quick-brown fox's den\n", "# The Quick-Brown Fox's Den\n", "title", nil, false, nil},
			{"crlf", "## installation guide\r\n\r\n### next step\r\n", "## Installation guide\r\n\r\n### Next step\r\n", "sentence", nil, false, nil},
			{"block quote", "> ## intro\n", "> ## Intro\n", "sentence", nil, false, nil},
			{"list item", "- # intro\n", "- # Intro\n", "sentence", nil, false, nil},
			{"setext", "intro\n=====\n", "Intro\n=====\n", "sentence", nil, false, nil},
			{"closed atx", "## intro ##\n", "## Intro ##\n", "sentence", nil, false, nil},
			{"code span", "# `x` guide\n", "# `x` Guide\n", "title", nil, false, nil},
			{"composition", "##  hello world  ##   \ntext", "## Hello world ##\ntext\n", "sentence", nil, false, composed},
			{"entity", "## &#97;bout\n", "## &#97;bout\n", "sentence", nil, false, nil},
			{"recognized front matter", "---\ntitle: a\n---\n# lower\n", "---\ntitle: a\n---\n# lower\n", "sentence", nil, true, nil},
			{"unrecognized front matter", "---\ntitle: a\n---\n# lower\n", "---\ntitle: a\n---\n# lower\n", "sentence", nil, false, nil},
			{"proper name", "## iphone setup\n", "## iphone setup\n", "sentence", []string{"iPhone"}, false, nil},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", headingCaseConfiguration(t, flavor, tc.style, tc.names, tc.frontmatter, tc.rules...))
				putConfigurationFile(t, root, "doc.md", tc.source)
				changed := tc.source != tc.candidate

				lint := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
				found := headingCaseFindings(validateConfigurationOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics)
				if len(found) == 0 {
					t.Fatalf("lint: %s", lint.Stdout)
				}
				for _, d := range found {
					if d.FixAvailable != changed {
						t.Fatalf("lint: %+v", d)
					}
				}

				preview := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
				r := preview.Result
				if !r.Complete || len(r.Files) != 1 || len(r.Changes) > 0 != changed || r.Files[0].ResultSnapshot.SHA256 != spacingDigest(tc.candidate) {
					t.Fatalf("preview: %s", preview.Stdout)
				}
				if spacingSaved(t, root) != tc.source {
					t.Fatal("dry run wrote")
				}

				saved := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
				out := validateConfigurationOperationJSON(t, []byte(saved.Stdout), "fix")
				if !out.Complete || len(out.Changes) > 0 != changed || spacingSaved(t, root) != tc.candidate {
					t.Fatalf("saved %q: %s", spacingSaved(t, root), saved.Stdout)
				}
				if changed && (len(out.Diagnostics) != 0 || len(r.Diagnostics) != 0) {
					t.Fatalf("remaining: %+v %+v", out.Diagnostics, r.Diagnostics)
				}

				again := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
				second := validateConfigurationOperationJSON(t, []byte(again.Stdout), "fix")
				if len(second.Changes) != 0 || spacingSaved(t, root) != tc.candidate || (len(headingCaseFindings(second.Diagnostics)) == 0) != changed {
					t.Fatalf("second run: %s", again.Stdout)
				}
			})
		}
	}
}

func TestCompiledHeadingCaseDryRunPreviewsOneByte(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", headingCaseConfiguration(t, "commonmark", "sentence", nil, false))
	putConfigurationFile(t, root, "doc.md", "## installation guide\n")
	text := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=text")
	if !strings.Contains(text.Stdout, "-## installation guide") || !strings.Contains(text.Stdout, "+## Installation guide") {
		t.Fatal(text.Stdout)
	}
	preview := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
	if c := preview.Result.Changes; len(c) != 1 || c[0].EditCount != 1 || len(c[0].Rules) != 1 || c[0].Rules[0] != "MD063" {
		t.Fatal(preview.Stdout)
	}
}

func TestCompiledHeadingCaseSuppressedFindingStays(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	root := configurationProject(t)
	source := "<!-- mdchk:disable MD063 -->\n## lower\n"
	putConfigurationFile(t, root, ".mdchk.yaml", headingCaseConfiguration(t, "commonmark", "sentence", nil, false))
	putConfigurationFile(t, root, "doc.md", source)
	saved := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
	out := validateConfigurationOperationJSON(t, []byte(saved.Stdout), "fix")
	if len(out.Changes) != 0 || len(headingCaseFindings(out.Diagnostics)) != 0 || len(out.SuppressedDiagnostics) != 1 || spacingSaved(t, root) != source {
		t.Fatal(saved.Stdout)
	}
	var hidden result.SuppressedDiagnostic
	if err := json.Unmarshal(out.SuppressedDiagnostics[0], &hidden); err != nil || hidden.Diagnostic.Rule != "MD063" || hidden.Diagnostic.FixAvailable {
		t.Fatal(string(out.SuppressedDiagnostics[0]), err)
	}
}
