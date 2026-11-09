package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func numberingConfiguration() string {
	config := strings.Replace(fixConfiguration(), "MD047: {enabled: true}", "MD047: {enabled: false}", 1)
	return strings.Replace(config, "MD029: {enabled: false}", "MD029: {enabled: true, allow_directives: true}", 1)
}

func TestMD029InheritedDirectiveDenials(t *testing.T) {
	for _, global := range []bool{false, true} {
		name := "rule"
		if global {
			name = "global"
		}
		t.Run(name, func(t *testing.T) {
			root := project(t)
			parent := numberingConfiguration()
			if global {
				parent += "directives: {enabled: false}\n"
			} else {
				parent = strings.Replace(parent, "allow_directives: true", "allow_directives: false", 1)
			}
			put(t, filepath.Join(root, ".mdchk.yaml"), parent)
			child := numberingConfiguration() + "directives: {enabled: true}\n"
			put(t, filepath.Join(root, "nested", ".mdchk.yaml"), child)
			put(t, filepath.Join(root, "nested", "doc.md"), "<!-- mdchk:disable MD029 -->\n1. first\n3. later\n")
			t.Chdir(filepath.Join(root, "nested"))
			var stdout, stderr bytes.Buffer
			if status := Run([]string{"lint", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 10 || stderr.Len() != 0 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
			out := validateEmitted(t, root, stdout.Bytes())
			if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD029" || len(out.SuppressedDiagnostics) != 0 {
				t.Fatal(out)
			}
		})
	}
}

func md029FixConfiguration() string {
	config := strings.Replace(fixConfiguration(), "MD029: {enabled: false}", "MD029: {enabled: true}", 1)
	for _, id := range []string{"MD009", "MD019", "MD021"} {
		config = strings.Replace(config, id+": {enabled: false}", id+": {enabled: true}", 1)
	}
	return config
}

func runMD029Fix(t *testing.T, root string, args ...string) (int, result.Result) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	status := Run(append(args, "--format=json"), &stdout, &stderr, "test")
	if stderr.Len() != 0 {
		t.Fatal(stderr.String())
	}
	return status, validateEmitted(t, root, stdout.Bytes())
}

func TestMD029FixPreviewWriteAndSecondRun(t *testing.T) {
	for _, tc := range []struct{ name, source, want, removed, added string }{
		{"lf", "1. a\n3. b\n", "1. a\n2. b\n", "3. b", "2. b"},
		{"crlf unicode", "1. é 日本\r\n3. é 日本\r\n", "1. é 日本\r\n2. é 日本\r\n", "3. é 日本", "2. é 日本"},
		{"composition", "#  Title\n\n1. a \n3. b\n\n## End  ##", "# Title\n\n1. a\n2. b\n\n## End ##\n", "3. b", "2. b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := project(t)
			put(t, filepath.Join(root, ".mdchk.yaml"), md029FixConfiguration())
			put(t, filepath.Join(root, "doc.md"), tc.source)
			t.Chdir(root)
			status, preview := runMD029Fix(t, root, "fix", "--dry-run", "doc.md")
			if status != 0 || len(preview.Changes) != 1 || preview.Changes[0].Diff == nil || len(preview.Diagnostics) != 0 {
				t.Fatal(status, preview)
			}
			removed, added := false, false
			for _, h := range preview.Changes[0].Diff.Hunks {
				for _, l := range h.Lines {
					removed = removed || l.Kind == "remove" && l.Text == tc.removed
					added = added || l.Kind == "add" && l.Text == tc.added
				}
			}
			if !removed || !added {
				t.Fatal("preview", preview.Changes[0].Diff.Hunks)
			}
			//nolint:gosec // Read only the test-owned source fixture.
			if content, _ := os.ReadFile(filepath.Join(root, "doc.md")); string(content) != tc.source {
				t.Fatal("dry run wrote source")
			}
			if status, saved := runMD029Fix(t, root, "fix", "doc.md"); status != 0 || len(saved.Changes) != 1 || len(saved.Diagnostics) != 0 {
				t.Fatal(status, saved)
			}
			//nolint:gosec // Read only the test-owned source fixture.
			if content, _ := os.ReadFile(filepath.Join(root, "doc.md")); string(content) != tc.want {
				t.Fatalf("wrote %q", content)
			}
			for _, args := range [][]string{{"fix", "--dry-run", "doc.md"}, {"fix", "doc.md"}} {
				if status, again := runMD029Fix(t, root, args...); status != 0 || len(again.Changes) != 0 || len(again.Diagnostics) != 0 {
					t.Fatal(args, status, again)
				}
			}
		})
	}
}

func TestMD029FixDefersUnsafeRenumbering(t *testing.T) {
	for _, source := range []string{"9. a\n9. b\n   more\n", "9. a\n9.\tb\n", "999999999. a\n999999999. b\n", "+++\n1. a\n3. b\n+++\n"} {
		t.Run(source, func(t *testing.T) {
			root := project(t)
			put(t, filepath.Join(root, ".mdchk.yaml"), md029FixConfiguration())
			put(t, filepath.Join(root, "doc.md"), source)
			t.Chdir(root)
			_, out := runMD029Fix(t, root, "fix", "doc.md")
			if len(out.Changes) != 0 || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD029" || out.Diagnostics[0].FixAvailable {
				t.Fatal(out)
			}
			//nolint:gosec // Read only the test-owned source fixture.
			if content, _ := os.ReadFile(filepath.Join(root, "doc.md")); string(content) != source {
				t.Fatalf("wrote %q", content)
			}
		})
	}
}
