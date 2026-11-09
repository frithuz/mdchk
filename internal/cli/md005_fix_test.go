package cli

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func alignmentFixConfiguration(ids ...string) string {
	config := fixConfiguration()
	config = strings.Replace(config, "MD047: {enabled: true}", "MD047: {enabled: false}", 1)
	for _, id := range ids {
		config = strings.Replace(config, id+": {enabled: false}", id+": {enabled: true}", 1)
	}
	return config + "directives: {enabled: true}\n"
}

func readAlignmentDoc(root string) ([]byte, error) {
	//nolint:gosec // Access only the synthetic test fixture.
	return os.ReadFile(filepath.Join(root, "doc.md"))
}

func TestMD005FixPreviewMatchesSavedResult(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"LF", "- One\n - Two\n", "- One\n- Two\n"},
		{"CRLF", "- One\r\n - Two\r\n", "- One\r\n- Two\r\n"},
		{"Unicode", "- é\n - 🐈\n", "- é\n- 🐈\n"},
		{"blockquote", "> - One\n>  - Two\n", "> - One\n> - Two\n"},
		{"nested list", "- one\n  - child\n   - child2\n", "- one\n  - child\n  - child2\n"},
		{"under-indented", "  - One\n - Two\n", "  - One\n  - Two\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := project(t)
			put(t, filepath.Join(root, ".mdchk.yaml"), alignmentFixConfiguration("MD005"))
			path := filepath.Join(root, "doc.md")
			put(t, path, tc.source)
			t.Chdir(root)
			var stdout, stderr bytes.Buffer
			if status := Run([]string{"fix", "--dry-run", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
			preview := validateEmitted(t, root, stdout.Bytes())
			if len(preview.Changes) != 1 || !slices.Equal(preview.Changes[0].Rules, []string{"MD005"}) || len(preview.Diagnostics) != 0 {
				t.Fatal(preview)
			}
			if data, _ := readAlignmentDoc(root); string(data) != tc.source {
				t.Fatalf("preview wrote %q", data)
			}
			stdout.Reset()
			if status := Run([]string{"fix", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
			data, _ := readAlignmentDoc(root)
			if string(data) != tc.want || preview.Changes[0].Candidate.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
				t.Fatalf("saved %q", data)
			}
			stdout.Reset()
			if status := Run([]string{"fix", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 {
				t.Fatal(status, stdout.String())
			}
			if again := validateEmitted(t, root, stdout.Bytes()); len(again.Changes) != 0 {
				t.Fatal("second run changed", again.Changes)
			}
		})
	}
}

func TestMD005FixDeferralsLeaveFile(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"continuation", "- One\n - Two\n   continued\n"},
		{"nested child", "- One\n - Two\n   - child\n"},
		{"lazy line", "- One\n - Two\nlazy\n"},
		{"tab", "- p\n    - One\n\t - Two\n"},
		{"disabled", "<!-- mdchk:disable MD005 -->\n\n- One\n - Two\n"},
		{"unrecognized front matter", "---\n- fake\n - fake\n---\n- one\n - two\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := project(t)
			put(t, filepath.Join(root, ".mdchk.yaml"), alignmentFixConfiguration("MD005"))
			path := filepath.Join(root, "doc.md")
			put(t, path, tc.source)
			t.Chdir(root)
			var stdout, stderr bytes.Buffer
			Run([]string{"lint", "doc.md", "--format=json"}, &stdout, &stderr, "test")
			for _, d := range validateEmitted(t, root, stdout.Bytes()).Diagnostics {
				if d.Rule == "MD005" && d.FixAvailable {
					t.Fatal("fixable", d)
				}
			}
			stdout.Reset()
			Run([]string{"fix", "doc.md", "--format=json"}, &stdout, &stderr, "test")
			if out := validateEmitted(t, root, stdout.Bytes()); len(out.Changes) != 0 {
				t.Fatal(out.Changes)
			}
			if data, _ := readAlignmentDoc(root); string(data) != tc.source {
				t.Fatalf("wrote %q", data)
			}
		})
	}
}

func TestMD005FixComposesInOneRun(t *testing.T) {
	root := project(t)
	config := strings.Replace(alignmentFixConfiguration("MD005", "MD009", "MD019", "MD021"), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	put(t, filepath.Join(root, ".mdchk.yaml"), config)
	path := filepath.Join(root, "doc.md")
	put(t, path, "# Title  #\n\n#  Spaced\n\n- One \n - Two\n\nText   \nend")
	t.Chdir(root)
	var stdout, stderr bytes.Buffer
	if status := Run([]string{"fix", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 {
		t.Fatal(status, stdout.String(), stderr.String())
	}
	out := validateEmitted(t, root, stdout.Bytes())
	if len(out.Changes) != 1 || !slices.Equal(out.Changes[0].Rules, []string{"MD005", "MD009", "MD019", "MD021", "MD047"}) {
		t.Fatal(out.Changes)
	}
	if data, _ := readAlignmentDoc(root); string(data) != "# Title #\n\n# Spaced\n\n- One\n- Two\n\nText  \nend\n" {
		t.Fatalf("%q", data)
	}
	stdout.Reset()
	if status := Run([]string{"fix", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 {
		t.Fatal(status, stdout.String())
	}
	if again := validateEmitted(t, root, stdout.Bytes()); len(again.Changes) != 0 {
		t.Fatal(again.Changes)
	}
}
