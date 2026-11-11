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

func blockquoteFixConfiguration(ids ...string) string {
	config := strings.Replace(fixConfiguration(), "MD047: {enabled: true}", "MD047: {enabled: false}", 1)
	for _, id := range ids {
		config = strings.Replace(config, id+": {enabled: false}", id+": {enabled: true}", 1)
	}
	return config + "directives: {enabled: true}\n"
}

func readBlockquoteDoc(root string) ([]byte, error) {
	//nolint:gosec // Access only the synthetic test fixture.
	return os.ReadFile(filepath.Join(root, "doc.md"))
}

// runBlockquoteFix previews, saves and reruns fix on source, returning the
// preview text output, the saved bytes and the rules of the preview change.
func runBlockquoteFix(t *testing.T, config, source string) (string, []byte, []string) {
	t.Helper()
	root := project(t)
	put(t, filepath.Join(root, ".mdchk.yaml"), config)
	put(t, filepath.Join(root, "doc.md"), source)
	t.Chdir(root)
	var stdout, stderr bytes.Buffer
	if status := Run([]string{"fix", "--dry-run", "doc.md"}, &stdout, &stderr, "test"); status != 0 {
		t.Fatal(status, stdout.String(), stderr.String())
	}
	text := stdout.String()
	stdout.Reset()
	if status := Run([]string{"fix", "--dry-run", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 {
		t.Fatal(status, stdout.String(), stderr.String())
	}
	preview := validateEmitted(t, root, stdout.Bytes())
	if len(preview.Changes) != 1 || len(preview.Diagnostics) != 0 {
		t.Fatal(preview)
	}
	if data, _ := readBlockquoteDoc(root); string(data) != source {
		t.Fatalf("preview wrote %q", data)
	}
	stdout.Reset()
	if status := Run([]string{"fix", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 {
		t.Fatal(status, stdout.String(), stderr.String())
	}
	data, _ := readBlockquoteDoc(root)
	if preview.Changes[0].Candidate.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
		t.Fatalf("saved %q differs from preview", data)
	}
	stdout.Reset()
	if status := Run([]string{"fix", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 {
		t.Fatal(status, stdout.String())
	}
	if again := validateEmitted(t, root, stdout.Bytes()); len(again.Changes) != 0 || len(again.Diagnostics) != 0 {
		t.Fatal("second run changed", again.Changes, again.Diagnostics)
	}
	return text, data, preview.Changes[0].Rules
}

func TestMD027FixPreviewMatchesSavedResult(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"LF", ">   Text\n", "> Text\n"},
		{"CRLF", ">   Text\r\n>    more\r\n", "> Text\r\n> more\r\n"},
		{"Unicode", ">   雪 é\n", "> 雪 é\n"},
		{"nested", "> >   x\n", "> > x\n"},
		{"list", "- >   x\n", "- > x\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := project(t)
			put(t, filepath.Join(root, ".mdchk.yaml"), blockquoteFixConfiguration("MD027"))
			put(t, filepath.Join(root, "doc.md"), tc.source)
			t.Chdir(root)
			var stdout, stderr bytes.Buffer
			if status := Run([]string{"lint", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 10 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
			for _, d := range validateEmitted(t, root, stdout.Bytes()).Diagnostics {
				if d.Rule != "MD027" || !d.FixAvailable {
					t.Fatal(d)
				}
			}
			text, data, rules := runBlockquoteFix(t, blockquoteFixConfiguration("MD027"), tc.source)
			if string(data) != tc.want || !slices.Equal(rules, []string{"MD027"}) || !strings.Contains(text, "validated edits (MD027)") {
				t.Fatalf("saved %q, rules %v, preview %s", data, rules, text)
			}
		})
	}
}

func TestMD027FixComposesWithTrailingSpaceAndFinalNewline(t *testing.T) {
	config := strings.Replace(blockquoteFixConfiguration("MD009", "MD027"), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	_, data, rules := runBlockquoteFix(t, config, ">   Text \n>   more")
	if string(data) != "> Text\n> more\n" || !slices.Equal(rules, []string{"MD009", "MD027", "MD047"}) {
		t.Fatalf("saved %q, rules %v", data, rules)
	}
}

func TestMD027FixDisabledLeavesFile(t *testing.T) {
	for _, tc := range []struct{ config, source string }{
		{blockquoteFixConfiguration(), ">   Text\n"},
		{blockquoteFixConfiguration("MD027"), "<!-- mdchk:disable MD027 -->\n\n>   Text\n"},
	} {
		config, source := tc.config, tc.source
		root := project(t)
		put(t, filepath.Join(root, ".mdchk.yaml"), config)
		put(t, filepath.Join(root, "doc.md"), source)
		t.Chdir(root)
		var stdout, stderr bytes.Buffer
		if status := Run([]string{"fix", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 {
			t.Fatal(status, stdout.String(), stderr.String())
		}
		if out := validateEmitted(t, root, stdout.Bytes()); len(out.Changes) != 0 || len(out.Diagnostics) != 0 {
			t.Fatal(out)
		}
		if data, _ := readBlockquoteDoc(root); string(data) != source {
			t.Fatalf("disabled rule wrote %q", data)
		}
	}
}
