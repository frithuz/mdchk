package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func styleConfiguration() string {
	config := strings.Replace(fixConfiguration(), "MD047: {enabled: true}", "MD047: {enabled: false}", 1)
	return strings.Replace(config, "MD003: {enabled: false}", "MD003: {enabled: true, style: consistent}", 1)
}

func TestMD003InheritedDirectiveDenials(t *testing.T) {
	for _, global := range []bool{false, true} {
		name := "rule"
		if global {
			name = "global"
		}
		t.Run(name, func(t *testing.T) {
			root := project(t)
			parent := styleConfiguration()
			if global {
				parent += "directives: {enabled: false}\n"
			} else {
				parent = strings.Replace(parent, "style: consistent}", "style: consistent, allow_directives: false}", 1)
			}
			put(t, filepath.Join(root, ".mdchk.yaml"), parent)
			child := strings.Replace(styleConfiguration(), "style: consistent}", "style: consistent, allow_directives: true}", 1) + "directives: {enabled: true}\n"
			put(t, filepath.Join(root, "nested", ".mdchk.yaml"), child)
			put(t, filepath.Join(root, "nested", "doc.md"), "## Root #\n<!-- mdchk:disable MD003 -->\n### Active\n")
			t.Chdir(filepath.Join(root, "nested"))
			var stdout, stderr bytes.Buffer
			if status := Run([]string{"lint", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 10 || stderr.Len() != 0 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
			out := validateEmitted(t, root, stdout.Bytes())
			if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD003" || len(out.SuppressedDiagnostics) != 0 {
				t.Fatal(out)
			}
		})
	}
}

func TestMD003FixPreviewAndApply(t *testing.T) {
	root := project(t)
	put(t, filepath.Join(root, ".mdchk.yaml"), styleConfiguration())
	path := filepath.Join(root, "doc.md")
	source := "# A\n\nB\n-\n"
	put(t, path, source)
	t.Chdir(root)
	var stdout, stderr bytes.Buffer
	if status := Run([]string{"fix", "--dry-run", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 || stderr.Len() != 0 {
		t.Fatal(status, stdout.String(), stderr.String())
	}
	out := validateEmitted(t, root, stdout.Bytes())
	if out.DryRun == nil || !*out.DryRun || len(out.Changes) != 1 || len(out.Diagnostics) != 0 || out.Files[0].WriteStatus != "proposed" {
		t.Fatal(out)
	}
	//nolint:gosec // Read only the test-owned temporary source fixture.
	if data, err := os.ReadFile(path); err != nil || string(data) != source {
		t.Fatal("preview changed the file", err, string(data))
	}
	for pass, wantChanges := range []int{1, 0} {
		stdout.Reset()
		if status := Run([]string{"fix", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 || stderr.Len() != 0 {
			t.Fatal(pass, status, stdout.String(), stderr.String())
		}
		out := validateEmitted(t, root, stdout.Bytes())
		if len(out.Changes) != wantChanges || !out.Complete || len(out.Diagnostics) != 0 {
			t.Fatal(pass, out)
		}
		//nolint:gosec // Read only the test-owned temporary source fixture.
		if data, err := os.ReadFile(path); err != nil || string(data) != "# A\n\n## B\n" {
			t.Fatal(pass, err, string(data))
		}
	}
}

func TestMD003FixComposesWithOtherCorrections(t *testing.T) {
	root := project(t)
	config := styleConfiguration()
	for _, id := range []string{"MD009", "MD019", "MD021"} {
		config = strings.Replace(config, id+": {enabled: false}", id+": {enabled: true}", 1)
	}
	config = strings.Replace(config, "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	put(t, filepath.Join(root, ".mdchk.yaml"), config)
	path := filepath.Join(root, "doc.md")
	put(t, path, "# Title\r\n\r\n##  Spaced\r\n\r\n### Closed   ###\r\n\r\nText   \r\n\r\nSub\r\n---\r\n\r\nend")
	t.Chdir(root)
	for pass, wantChanges := range []int{1, 0} {
		var stdout, stderr bytes.Buffer
		if status := Run([]string{"fix", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 || stderr.Len() != 0 {
			t.Fatal(pass, status, stdout.String(), stderr.String())
		}
		out := validateEmitted(t, root, stdout.Bytes())
		if len(out.Changes) != wantChanges || !out.Complete || len(out.Diagnostics) != 0 || len(out.DeferredFixes) != 0 {
			t.Fatal(pass, out)
		}
		//nolint:gosec // Read only the test-owned temporary source fixture.
		if data, err := os.ReadFile(path); err != nil || string(data) != "# Title\r\n\r\n## Spaced\r\n\r\n### Closed\r\n\r\nText\r\n\r\n## Sub\r\n\r\nend\r\n" {
			t.Fatalf("%d %v %q", pass, err, data)
		}
	}
}
