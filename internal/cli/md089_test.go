package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// lintMD089Clean reports whether lint finds nothing in the saved doc.md.
func lintMD089Clean(t *testing.T, root string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if status := Run([]string{"lint", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 {
		t.Fatal(status, stdout.String(), stderr.String())
	}
	if out := validateEmitted(t, root, stdout.Bytes()); len(out.Diagnostics) != 0 {
		t.Fatal("findings after fix", out.Diagnostics)
	}
}

func TestMD089FixPreviewMatchesSavedResult(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"LF", "版本2\n", "版本 2\n"},
		{"CRLF", "中A\r\nB中\r\n", "中 A\r\nB 中\r\n"},
		{"mixed", "版本2\n\n- 中A and `中A`\n\n> B中 [中A](/中A)\n", "版本 2\n\n- 中 A and `中A`\n\n> B 中 [中 A](/中A)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := project(t)
			put(t, filepath.Join(root, ".mdchk.yaml"), blockquoteFixConfiguration("MD089"))
			put(t, filepath.Join(root, "doc.md"), tc.source)
			t.Chdir(root)
			var stdout, stderr bytes.Buffer
			if status := Run([]string{"lint", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 10 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
			for _, d := range validateEmitted(t, root, stdout.Bytes()).Diagnostics {
				if d.Rule != "MD089" || !d.FixAvailable {
					t.Fatal(d)
				}
			}
			text, data, rules := runBlockquoteFix(t, blockquoteFixConfiguration("MD089"), tc.source)
			if string(data) != tc.want || !slices.Equal(rules, []string{"MD089"}) || !strings.Contains(text, "validated edits (MD089)") {
				t.Fatalf("saved %q, rules %v, preview %s", data, rules, text)
			}
			lintMD089Clean(t, root)
		})
	}
}

func TestMD089FixComposesWithOtherCorrections(t *testing.T) {
	config := strings.Replace(blockquoteFixConfiguration("MD009", "MD027", "MD089"), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	_, data, rules := runBlockquoteFix(t, config, ">   版本2 \n>   中A more\n\nB中 end")
	if string(data) != "> 版本 2\n> 中 A more\n\nB 中 end\n" || !slices.Equal(rules, []string{"MD009", "MD027", "MD047", "MD089"}) {
		t.Fatalf("saved %q, rules %v", data, rules)
	}
	lintMD089Clean(t, ".")
}

func TestMD089FixDisabledLeavesFile(t *testing.T) {
	for _, tc := range []struct{ config, source string }{
		{blockquoteFixConfiguration(), "中A\n"},
		{blockquoteFixConfiguration("MD089"), "<!-- mdchk:disable MD089 -->\n\n中A\n"},
	} {
		root := project(t)
		put(t, filepath.Join(root, ".mdchk.yaml"), tc.config)
		put(t, filepath.Join(root, "doc.md"), tc.source)
		t.Chdir(root)
		var stdout, stderr bytes.Buffer
		if status := Run([]string{"fix", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 {
			t.Fatal(status, stdout.String(), stderr.String())
		}
		if out := validateEmitted(t, root, stdout.Bytes()); len(out.Changes) != 0 || len(out.Diagnostics) != 0 {
			t.Fatal(out)
		}
		if data, _ := readBlockquoteDoc(root); string(data) != tc.source {
			t.Fatalf("disabled rule wrote %q", data)
		}
	}
}

// A document opening with an unrecognized block gets no MD089 edit, and its
// findings keep fix_available false in JSON output.
func TestMD089GuardedDocumentStaysManual(t *testing.T) {
	root := project(t)
	put(t, filepath.Join(root, ".mdchk.yaml"), blockquoteFixConfiguration("MD089"))
	source := "---\n中A\n---\n\n中B\n"
	put(t, filepath.Join(root, "doc.md"), source)
	t.Chdir(root)
	var stdout, stderr bytes.Buffer
	if status := Run([]string{"fix", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 10 {
		t.Fatal(status, stdout.String(), stderr.String())
	}
	out := validateEmitted(t, root, stdout.Bytes())
	if len(out.Changes) != 0 || len(out.Diagnostics) == 0 {
		t.Fatal(out)
	}
	for _, d := range out.Diagnostics {
		if d.Rule != "MD089" || d.FixAvailable {
			t.Fatal(d)
		}
	}
	if data, _ := readBlockquoteDoc(root); string(data) != source {
		t.Fatalf("guarded document wrote %q", data)
	}
}

// Heading text derives the anchor that same-file and cross-file fragments name,
// so fix leaves it alone and reports it with fix_available false.
func TestMD089HeadingAnchorsStay(t *testing.T) {
	root := project(t)
	put(t, filepath.Join(root, ".mdchk.yaml"), blockquoteFixConfiguration("MD089", "MD051"))
	files := map[string]string{"a.md": "# 版本2\n\nSee [v](#版本2).\n", "b.md": "# B\n\nSee [v](a.md#版本2).\n"}
	for name, source := range files {
		put(t, filepath.Join(root, name), source)
	}
	t.Chdir(root)
	var stdout, stderr bytes.Buffer
	if status := Run([]string{"fix", "a.md", "--format=json"}, &stdout, &stderr, "test"); status != 10 {
		t.Fatal(status, stdout.String(), stderr.String())
	}
	out := validateEmitted(t, root, stdout.Bytes())
	if len(out.Changes) != 0 || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD089" || out.Diagnostics[0].FixAvailable {
		t.Fatal(out)
	}
	for name, source := range files {
		//nolint:gosec // Access only the synthetic test fixture.
		if data, _ := os.ReadFile(filepath.Join(root, name)); string(data) != source {
			t.Fatalf("%s rewritten to %q", name, data)
		}
	}
}
