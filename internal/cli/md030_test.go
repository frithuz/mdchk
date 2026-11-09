package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func markerSpacingConfiguration() string {
	config := strings.Replace(fixConfiguration(), "MD047: {enabled: true}", "MD047: {enabled: false}", 1)
	return strings.Replace(config, "MD030: {enabled: false}", "MD030: {enabled: true, allow_directives: true}", 1)
}

func TestMD030InheritedDirectiveDenials(t *testing.T) {
	for _, global := range []bool{false, true} {
		name := "rule"
		if global {
			name = "global"
		}
		t.Run(name, func(t *testing.T) {
			root := project(t)
			parent := markerSpacingConfiguration()
			if global {
				parent += "directives: {enabled: false}\n"
			} else {
				parent = strings.Replace(parent, "allow_directives: true", "allow_directives: false", 1)
			}
			put(t, filepath.Join(root, ".mdchk.yaml"), parent)
			child := markerSpacingConfiguration() + "directives: {enabled: true}\n"
			put(t, filepath.Join(root, "nested", ".mdchk.yaml"), child)
			put(t, filepath.Join(root, "nested", "doc.md"), "<!-- mdchk:disable MD030 -->\n- first\n-  later\n")
			t.Chdir(filepath.Join(root, "nested"))
			var stdout, stderr bytes.Buffer
			if status := Run([]string{"lint", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 10 || stderr.Len() != 0 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
			out := validateEmitted(t, root, stdout.Bytes())
			if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD030" || len(out.SuppressedDiagnostics) != 0 {
				t.Fatal(out)
			}
		})
	}
}

func TestMD030FixPreviewAndSave(t *testing.T) {
	root := project(t)
	put(t, filepath.Join(root, ".mdchk.yaml"), markerSpacingConfiguration())
	put(t, filepath.Join(root, "doc.md"), "-  One\r\n")
	t.Chdir(root)
	var stdout, stderr bytes.Buffer
	if status := Run([]string{"fix", "--dry-run", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 || stderr.Len() != 0 {
		t.Fatal(status, stdout.String(), stderr.String())
	}
	out := validateEmitted(t, root, stdout.Bytes())
	if len(out.Changes) != 1 || len(out.Diagnostics) != 0 || len(out.DeferredFixes) != 0 || out.Files[0].WriteStatus != "proposed" {
		t.Fatal(out)
	}
	c := out.Changes[0]
	if !reflect.DeepEqual(c.Rules, []string{"MD030"}) || c.EditCount != 1 || c.Diff == nil || len(c.Diff.Hunks) != 1 {
		t.Fatal(c)
	}
	lines := c.Diff.Hunks[0].Lines
	if len(lines) != 2 || lines[0].Kind != "remove" || lines[0].Text != "-  One" || lines[0].LineEnding != "crlf" || lines[1].Kind != "add" || lines[1].Text != "- One" || lines[1].LineEnding != "crlf" {
		t.Fatal(lines)
	}
	//nolint:gosec // Read only the test-owned source fixture.
	if content, _ := os.ReadFile(filepath.Join(root, "doc.md")); string(content) != "-  One\r\n" {
		t.Fatalf("dry run wrote %q", content)
	}
	for pass, want := range []string{"written", "not-needed"} {
		stdout.Reset()
		if status := Run([]string{"fix", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 || stderr.Len() != 0 {
			t.Fatal(status, stdout.String(), stderr.String())
		}
		out := validateEmitted(t, root, stdout.Bytes())
		if out.Files[0].WriteStatus != want || len(out.Changes) != 1-pass || len(out.Diagnostics) != 0 {
			t.Fatal(pass, out)
		}
		//nolint:gosec // Read only the test-owned source fixture.
		if content, _ := os.ReadFile(filepath.Join(root, "doc.md")); string(content) != "- One\r\n" {
			t.Fatalf("saved %q", content)
		}
	}
}

func TestMD030ManualFindingsStayUnchanged(t *testing.T) {
	for _, tc := range []struct{ name, source, option string }{
		{"continuation", "-  One\n   two\n", ""},
		{"child", "-  One\n\n   - child\n", ""},
		{"tab", "-\tOne\n", ", spaces: 2"},
		{"fallback", "-      code\n", ", spaces: 2"},
		{"spaces 5", "- One\n", ", spaces: 5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := project(t)
			config := strings.Replace(markerSpacingConfiguration(), "allow_directives: true}", "allow_directives: true"+tc.option+"}", 1)
			put(t, filepath.Join(root, ".mdchk.yaml"), config)
			put(t, filepath.Join(root, "doc.md"), tc.source)
			t.Chdir(root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "doc.md", "--format=json"}} {
				var stdout, stderr bytes.Buffer
				if status := Run(args, &stdout, &stderr, "test"); status != 10 || stderr.Len() != 0 {
					t.Fatal(status, stdout.String(), stderr.String())
				}
				out := validateEmitted(t, root, stdout.Bytes())
				if len(out.Changes) != 0 || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD030" || out.Diagnostics[0].FixAvailable {
					t.Fatal(out)
				}
			}
			//nolint:gosec // Read only the test-owned source fixture.
			if content, _ := os.ReadFile(filepath.Join(root, "doc.md")); string(content) != tc.source {
				t.Fatalf("changed %q", content)
			}
		})
	}
}
