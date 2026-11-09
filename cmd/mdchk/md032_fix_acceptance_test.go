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

// md032FixConfiguration enables MD032 with directives, plus the variant's
// composed rules or front matter recognition.
func md032FixConfiguration(flavor, variant string) string {
	config := strings.ReplaceAll(md032Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}")
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

func TestCompiledMD032FixWorkflow(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, tc := range []struct {
		name, variant, source, candidate string
		rules                            []string
		edits                            int
		// remaining lists the byte offsets of MD032 findings left in the candidate.
		remaining []int
	}{
		{"before", "", "Text\n- One\n", "Text\n\n- One\n", []string{"MD032"}, 1, nil},
		{"after", "", "- One\n# H\n", "- One\n\n# H\n", []string{"MD032"}, 1, nil},
		{"both sides", "", "Text\n- One\n# H\n", "Text\n\n- One\n\n# H\n", []string{"MD032"}, 2, nil},
		{"crlf", "", "Text\r\n- One\r\n# H\r\n", "Text\r\n\r\n- One\r\n\r\n# H\r\n", []string{"MD032"}, 2, nil},
		{"unicode", "", "雪の日\n- 雪 ❄\n# 終わり\n", "雪の日\n\n- 雪 ❄\n\n# 終わり\n", []string{"MD032"}, 2, nil},
		{"blockquote", "", "> Text\n> - One\n", "> Text\n>\n> - One\n", []string{"MD032"}, 1, nil},
		{"loose nested list", "", "- a\n\n  - b\n  # H\n", "- a\n\n  - b\n\n  # H\n", []string{"MD032"}, 1, nil},
		{"quote in list", "", "1. a\n   > q\n   > - b\n   > # H\n", "1. a\n   > q\n   >\n   > - b\n   >\n   > # H\n", []string{"MD032"}, 2, nil},
		{"fenced code", "", "```\n- x\n```\n- One\n", "```\n- x\n```\n\n- One\n", []string{"MD032"}, 1, nil},
		{"html block before", "", "<!-- c -->\n- One\n", "<!-- c -->\n- One\n", nil, 0, []int{11}},
		{"tightness change", "", "- a\n  - b\n  # H\n", "- a\n  - b\n  # H\n", nil, 0, []int{12}},
		{"directive interval", "", "<!-- mdchk:disable MD032 -->\n\nText\n- One\n", "<!-- mdchk:disable MD032 -->\n\nText\n- One\n", nil, 0, nil},
		{"unrecognized leading block", "", "---\ntitle: x\n---\nText\n- One\n", "---\ntitle: x\n---\nText\n- One\n", nil, 0, []int{22}},
		{"recognized front matter", "frontmatter", "---\ntitle: x\n---\nText\n- One\n", "---\ntitle: x\n---\nText\n\n- One\n", []string{"MD032"}, 1, nil},
		{"composition", "compose", "#  Title  #\n\nText \n- One\n## H", "# Title #\n\nText\n\n- One\n\n## H\n", []string{"MD009", "MD019", "MD021", "MD032", "MD047"}, 6, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md032FixConfiguration("commonmark", tc.variant))
			putConfigurationFile(t, root, "doc.md", tc.source)
			changed := tc.source != tc.candidate
			remaining := func(diagnostics []result.Diagnostic) {
				t.Helper()
				var got []int
				for _, d := range diagnostics {
					if d.Rule == "MD032" {
						if d.FixAvailable {
							t.Fatal("remaining finding advertised", d)
						}
						got = append(got, d.ByteRange.Start)
					}
				}
				if !reflect.DeepEqual(got, tc.remaining) {
					t.Fatalf("remaining %v want %v", got, tc.remaining)
				}
			}

			text := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=text")
			if strings.Contains(text.Stdout, "proposed doc.md") != changed {
				t.Fatal(text.Stdout)
			}
			preview := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json").Result
			if !preview.Complete || len(preview.Files) != 1 || len(preview.Changes) > 0 != changed || preview.Files[0].ResultSnapshot.SHA256 != spacingDigest(tc.candidate) {
				t.Fatalf("preview: %+v", preview)
			}
			if changed && (preview.Changes[0].EditCount != tc.edits || !reflect.DeepEqual(preview.Changes[0].Rules, tc.rules)) {
				t.Fatalf("change: %+v", preview.Changes[0])
			}
			remaining(preview.Diagnostics)
			for _, raw := range preview.SuppressedDiagnostics {
				var suppressed result.SuppressedDiagnostic
				if err := json.Unmarshal(raw, &suppressed); err != nil || suppressed.Diagnostic.FixAvailable {
					t.Fatal("suppressed finding advertised", string(raw), err)
				}
			}
			if spacingSaved(t, root) != tc.source {
				t.Fatal("preview wrote the file")
			}

			saved := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
			out := validateConfigurationOperationJSON(t, []byte(saved.Stdout), "fix")
			if !out.Complete || len(out.Changes) > 0 != changed || changed && out.Changes[0].Disposition != "written" {
				t.Fatalf("saved: %s", saved.Stdout)
			}
			if got := spacingSaved(t, root); got != tc.candidate {
				t.Fatalf("saved %q want %q", got, tc.candidate)
			}
			remaining(out.Diagnostics)

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
			if len(again.Changes) != 0 || spacingSaved(t, root) != tc.candidate || !os.SameFile(identity, after) || !identity.ModTime().Equal(after.ModTime()) {
				t.Fatalf("second run changed file: %s", second.Stdout)
			}
			remaining(again.Diagnostics)
		})
	}
}
