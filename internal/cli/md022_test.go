package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func blankHeadingConfiguration() string {
	config := strings.Replace(fixConfiguration(), "MD047: {enabled: true}", "MD047: {enabled: false}", 1)
	return strings.Replace(config, "MD022: {enabled: false}", "MD022: {enabled: true, allow_directives: true}", 1)
}

func TestMD022InheritedDirectiveDenials(t *testing.T) {
	for _, global := range []bool{false, true} {
		name := "rule"
		if global {
			name = "global"
		}
		t.Run(name, func(t *testing.T) {
			root := project(t)
			parent := blankHeadingConfiguration()
			if global {
				parent += "directives: {enabled: false}\n"
			} else {
				parent = strings.Replace(parent, "allow_directives: true}", "allow_directives: false}", 1)
			}
			put(t, filepath.Join(root, ".mdchk.yaml"), parent)
			child := blankHeadingConfiguration() + "directives: {enabled: true}\n"
			put(t, filepath.Join(root, "nested", ".mdchk.yaml"), child)
			put(t, filepath.Join(root, "nested", "doc.md"), "Text\n<!-- mdchk:disable MD022 -->\n### Active\nText\n")
			t.Chdir(filepath.Join(root, "nested"))
			var stdout, stderr bytes.Buffer
			if status := Run([]string{"lint", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 10 || stderr.Len() != 0 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
			out := validateEmitted(t, root, stdout.Bytes())
			if !out.Complete || len(out.Diagnostics) != 2 || out.Diagnostics[0].Rule != "MD022" || len(out.SuppressedDiagnostics) != 0 {
				t.Fatal(out)
			}
		})
	}
}

func blankFixConfiguration(options string, ids ...string) string {
	config := strings.Replace(fixConfiguration(), "MD047: {enabled: true}", "MD047: {enabled: false}", 1)
	for _, id := range ids {
		config = strings.Replace(config, id+": {enabled: false}", id+": {enabled: true}", 1)
	}
	return strings.Replace(config, "MD022: {enabled: false}", "MD022: {enabled: true, allow_directives: true"+options+"}", 1)
}

func TestMD022FixPreviewAndSave(t *testing.T) {
	for _, tc := range []struct {
		name, options, extra, source, want string
		ids                                []string
	}{
		{"published", "", "", "Text\n## Install\nText\n", "Text\n\n## Install\n\nText\n", nil},
		{"crlf", "", "", "Text\r\n## Install\r\nText\r\n", "Text\r\n\r\n## Install\r\n\r\nText\r\n", nil},
		{"quote", "", "", "> Text\n> ## H\n> Text\n", "> Text\n>\n> ## H\n>\n> Text\n", nil},
		{"consecutive", "", "", "# A\n# B\n", "# A\n\n# B\n", nil},
		{"before edge", ", before: 2, after: 2", "", "\n# H\n\nText\n", "\n# H\n\n\nText\n", nil},
		{"before zero", ", before: 0", "", "Text\n# H\nText\n", "Text\n# H\n\nText\n", nil},
		{"after zero", ", after: 0", "", "Text\n# H\nText\n", "Text\n\n# H\nText\n", nil},
		{"setext", "", "", "Alpha\nBeta\n===\nTail\n", "Alpha\nBeta\n===\n\nTail\n", nil},
		{"unicode", "", "", "雪\n# 雪 ü\n本\n", "雪\n\n# 雪 ü\n\n本\n", nil},
		{"composition", "", "", "Text \n#  A  #\n# B\nend", "Text\n\n# A #\n\n# B\n\nend\n", []string{"MD009", "MD019", "MD021", "MD047"}},
		{"recognized front matter body", "", "frontmatter: {enabled: true}\n", "---\nt: y\n---\n\nText\n# H\n", "---\nt: y\n---\n\nText\n\n# H\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := project(t)
			put(t, filepath.Join(root, ".mdchk.yaml"), blankFixConfiguration(tc.options, tc.ids...)+tc.extra)
			put(t, filepath.Join(root, "doc.md"), tc.source)
			t.Chdir(root)
			var stdout, stderr bytes.Buffer
			if status := Run([]string{"fix", "--dry-run", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 || stderr.Len() != 0 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
			preview := validateEmitted(t, root, stdout.Bytes())
			if !preview.Complete || len(preview.Changes) != 1 || len(preview.Diagnostics) != 0 || read(t, filepath.Join(root, "doc.md")) != tc.source {
				t.Fatal(stdout.String())
			}
			stdout.Reset()
			if status := Run([]string{"fix", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 || stderr.Len() != 0 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
			if got := read(t, filepath.Join(root, "doc.md")); got != tc.want {
				t.Fatalf("saved %q want %q", got, tc.want)
			}
			for _, args := range [][]string{{"fix", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
				stdout.Reset()
				if status := Run(args, &stdout, &stderr, "test"); status != 0 || stderr.Len() != 0 {
					t.Fatal(status, stdout.String(), stderr.String())
				}
				if out := validateEmitted(t, root, stdout.Bytes()); len(out.Changes) != 0 || len(out.Diagnostics) != 0 || read(t, filepath.Join(root, "doc.md")) != tc.want {
					t.Fatal("second run changed the file", stdout.String())
				}
			}
		})
	}
}

func TestMD022FixDefers(t *testing.T) {
	for _, tc := range []struct{ name, extra, source string }{
		{"fence", "", "Text\n\n```\nx\n```\n# H\n"},
		{"html", "", "# H\n<div>\nx\n</div>\n"},
		{"recognized front matter", "frontmatter: {enabled: true}\n", "---\nt: y\n---\n# H\n"},
		{"unrecognized front matter", "", "---\nt: y\n---\nText\n# H\n"},
		{"lazy", "", "> Text\nlazy\n> ## H\n"},
		{"list item", "", "- a\n  # H\n  b\n"},
		{"directive", "", "Text\n<!-- mdchk:disable MD022 -->\n# H\nText\n<!-- mdchk:enable MD022 -->\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := project(t)
			put(t, filepath.Join(root, ".mdchk.yaml"), blankFixConfiguration("")+tc.extra)
			put(t, filepath.Join(root, "doc.md"), tc.source)
			t.Chdir(root)
			var stdout, stderr bytes.Buffer
			// A suppressed finding leaves the file clean; its record still has no fix.
			suppressed := tc.name == "directive"
			want := 10
			if suppressed {
				want = 0
			}
			if status := Run([]string{"fix", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != want || stderr.Len() != 0 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
			out := validateEmitted(t, root, stdout.Bytes())
			if !out.Complete || len(out.Changes) != 0 || (len(out.Diagnostics) == 0) != suppressed || (len(out.SuppressedDiagnostics) == 0) == suppressed || bytes.Contains(stdout.Bytes(), []byte(`"fix_available":true`)) || read(t, filepath.Join(root, "doc.md")) != tc.source {
				t.Fatal(stdout.String())
			}
			for _, d := range out.Diagnostics {
				if d.Rule != "MD022" || d.FixAvailable {
					t.Fatalf("%+v", d)
				}
			}
		})
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	//nolint:gosec // Read back only the test-owned fixture.
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
