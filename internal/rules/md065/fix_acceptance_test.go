package md065_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
	"github.com/frithuz/mdchk/internal/result"
)

// md065FixConfiguration enables MD065 in flavor with the given rule settings,
// and adds the top-level setting or replaces the directives one.
func md065FixConfiguration(flavor, top string, settings map[string]string) string {
	config := strings.Replace(md065Configuration(flavor), "MD065: {}", "MD065: {enabled: true}", 1)
	for id, setting := range settings {
		config = strings.Replace(config, id+": {enabled: false}", id+": "+setting, 1)
	}
	switch {
	case strings.HasPrefix(top, "directives:"):
		config = strings.Replace(config, "directives: {enabled: false}", top, 1)
	case top != "":
		config = strings.Replace(config, "rules:\n", top+"\nrules:\n", 1)
	}
	return config
}

func TestCompiledMD065FixWorkflow(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	on := "{enabled: true}"
	for _, tc := range []struct {
		name, flavor, top, source, candidate string
		settings                             map[string]string
		rules                                []string
		edits                                int
		// fixable lists the byte offsets of fix-eligible findings in the source.
		fixable []int
		// remaining lists the findings left in the candidate, as rule@offset.
		remaining []string
	}{
		{"both sides", "", "", "Text\n***\nMore\n", "Text\n\n***\n\nMore\n", nil, []string{"MD065"}, 2, []int{5, 8}, nil},
		{"crlf", "", "", "Text\r\n***\r\nMore\r\n", "Text\r\n\r\n***\r\n\r\nMore\r\n", nil, []string{"MD065"}, 2, []int{6, 9}, nil},
		{"unicode", "", "", "雪の日\n***\n終わり ❄\n", "雪の日\n\n***\n\n終わり ❄\n", nil, []string{"MD065"}, 2, []int{10, 13}, nil},
		{"blockquote", "", "", "> a\n> ***\n> b\n", "> a\n>\n> ***\n>\n> b\n", nil, []string{"MD065"}, 2, []int{6, 9}, nil},
		{"quote in list", "", "", "1. a\n   > q\n   > ***\n", "1. a\n   > q\n   >\n   > ***\n", nil, []string{"MD065"}, 1, []int{17}, nil},
		{"loose list item", "", "", "- a\n\n  b\n  ***\n- c\n", "- a\n\n  b\n\n  ***\n- c\n", nil, []string{"MD065"}, 1, []int{11}, nil},
		{"shared gap", "", "", "Text\n\n***\n***\n\nMore\n", "Text\n\n***\n\n***\n\nMore\n", nil, []string{"MD065"}, 1, []int{9, 10}, nil},
		{"tight list", "", "", "- a\n  ***\n- b\n", "- a\n  ***\n- b\n", nil, nil, 0, nil, []string{"MD065@6"}},
		{"lazy neighbour", "", "", "> a\nb\n> ***\n", "> a\nb\n> ***\n", nil, nil, 0, nil, []string{"MD065@8"}},
		{"tab prefix", "", "", "> a\n>\t***\n", "> a\n>\t***\n", nil, nil, 0, nil, []string{"MD065@6"}},
		{"fenced code", "", "", "```\nx\n```\n***\n\nMore\n", "```\nx\n```\n***\n\nMore\n", nil, nil, 0, nil, []string{"MD065@10"}},
		{"html comment", "", "", "<!-- c -->\n***\n\nMore\n", "<!-- c -->\n***\n\nMore\n", nil, nil, 0, nil, []string{"MD065@11"}},
		{"unrecognized front matter", "", "", "---\ntitle: x\n---\nText\n***\nMore\n", "---\ntitle: x\n---\nText\n***\nMore\n", nil, nil, 0, nil, []string{"MD065@3", "MD065@22", "MD065@25"}},
		{"recognized front matter", "", "frontmatter: {enabled: true}", "---\ntitle: x\n---\nText\n***\nMore\n", "---\ntitle: x\n---\nText\n\n***\n\nMore\n", nil, []string{"MD065"}, 2, []int{22, 25}, nil},
		{"directive interval", "", "directives: {enabled: true}", "<!-- mdchk:disable MD065 -->\n\nText\n***\nMore\n", "<!-- mdchk:disable MD065 -->\n\nText\n***\nMore\n", nil, nil, 0, nil, nil},
		{"md009 md047", "", "", "Text \n***\nMore", "Text\n\n***\n\nMore\n", map[string]string{"MD009": on, "MD047": on}, []string{"MD009", "MD047", "MD065"}, 4, []int{6, 9}, nil},
		{"md009 strict md047", "", "", "Text  \n***\nMore", "Text\n\n***\n\nMore\n", map[string]string{"MD009": "{enabled: true, strict: true}", "MD047": on}, []string{"MD009", "MD047", "MD065"}, 4, []int{7, 10}, nil},
		// Exactly hard_break_spaces trailing spaces are allowed when strict is off.
		{"md009 allowance md047", "", "", "Text  \n***\nMore", "Text  \n\n***\n\nMore\n", map[string]string{"MD009": on, "MD047": on}, []string{"MD047", "MD065"}, 3, []int{7, 10}, nil},
		{"md022", "", "", "# H\n***\nText\n", "# H\n\n***\n\nText\n", map[string]string{"MD022": on}, []string{"MD022", "MD065"}, 2, []int{4, 7}, nil},
		{"md032", "", "", "- a\n***\nText\n", "- a\n\n***\n\nText\n", map[string]string{"MD032": on}, []string{"MD032", "MD065"}, 2, []int{4, 7}, nil},
		{"md012", "", "", "Text\n\n\n***\nMore\n", "Text\n\n***\n\nMore\n", map[string]string{"MD012": on}, []string{"MD012", "MD065"}, 2, []int{10}, nil},
		{"md035", "", "", "Text\n* * *\nMore\n", "Text\n\n---\n\nMore\n", map[string]string{"MD035": "{enabled: true, style: dash}"}, []string{"MD035", "MD065"}, 3, []int{5, 10}, nil},
		{"md058 stays manual", "gfm", "", "# T\n| a |\n| - |\n", "# T\n| a |\n| - |\n", map[string]string{"MD058": on}, nil, 0, nil, []string{"MD058@4"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flavor := tc.flavor
			if flavor == "" {
				flavor = "commonmark"
			}
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", md065FixConfiguration(flavor, tc.top, tc.settings))
			acceptance.PutFile(t, root, "doc.md", tc.source)
			changed := tc.source != tc.candidate
			remaining := func(diagnostics []result.Diagnostic) {
				t.Helper()
				var got []string
				for _, d := range diagnostics {
					if d.FixAvailable {
						t.Fatal("remaining finding advertised", d)
					}
					got = append(got, fmt.Sprintf("%s@%d", d.Rule, d.ByteRange.Start))
				}
				if !reflect.DeepEqual(got, tc.remaining) {
					t.Fatalf("remaining %v want %v", got, tc.remaining)
				}
			}

			lint := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
			var fixable []int
			for _, d := range acceptance.ValidateOperationJSON(t, []byte(lint.Stdout), "lint").Diagnostics {
				if d.Rule == "MD065" && d.FixAvailable {
					fixable = append(fixable, d.ByteRange.Start)
				}
			}
			if !reflect.DeepEqual(fixable, tc.fixable) {
				t.Fatalf("fixable %v want %v", fixable, tc.fixable)
			}

			text := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=text")
			if strings.Contains(text.Stdout, "proposed doc.md") != changed {
				t.Fatal(text.Stdout)
			}
			preview := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json").Result
			if !preview.Complete || len(preview.Files) != 1 || len(preview.Changes) > 0 != changed || len(preview.DeferredFixes) != 0 || preview.Files[0].ResultSnapshot.SHA256 != acceptance.Digest(tc.candidate) {
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
			if acceptance.Saved(t, root) != tc.source {
				t.Fatal("preview wrote the file")
			}

			saved := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
			out := acceptance.ValidateOperationJSON(t, []byte(saved.Stdout), "fix")
			if !out.Complete || len(out.Changes) > 0 != changed || len(out.DeferredFixes) != 0 || changed && out.Changes[0].Disposition != "written" {
				t.Fatalf("saved: %s", saved.Stdout)
			}
			if got := acceptance.Saved(t, root); got != tc.candidate {
				t.Fatalf("saved %q want %q", got, tc.candidate)
			}
			remaining(out.Diagnostics)

			path := filepath.Join(root, "doc.md")
			identity, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"fix", "--dry-run", "doc.md", "--format=json"}, {"fix", "doc.md", "--format=json"}} {
				second := acceptance.RunProcess(t, binary, root, args...)
				again := acceptance.ValidateOperationJSON(t, []byte(second.Stdout), "fix")
				after, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if len(again.Changes) != 0 || acceptance.Saved(t, root) != tc.candidate || !os.SameFile(identity, after) || !identity.ModTime().Equal(after.ModTime()) {
					t.Fatalf("second run changed file: %s", second.Stdout)
				}
				remaining(again.Diagnostics)
			}
		})
	}
}
