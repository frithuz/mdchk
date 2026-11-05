package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func metadataOrderingConfiguration(entry string) string {
	config := strings.Replace(fixConfiguration(), "MD047: {enabled: true}", "MD047: {enabled: false}", 1)
	config = strings.Replace(config, "MD094: {enabled: true}", "MD094: {enabled: false}", 1)
	return strings.Replace(config, "MD072: {enabled: false}", entry, 1) + "frontmatter: {enabled: true}\n"
}

func TestMD072ProductionAdmission(t *testing.T) {
	for _, entry := range []string{
		"MD072: {enabled: true, allow_directives: true}",
		"MD072: {enabled: true, allow_directives: false}",
		"MD072: {enabled: true, recursive: true, case_sensitive: false}",
	} {
		t.Run(entry, func(t *testing.T) {
			root := project(t)
			put(t, filepath.Join(root, ".mdchk.yaml"), metadataOrderingConfiguration(entry))
			put(t, filepath.Join(root, "doc.md"), "---\na: 1\nz: 2\n---\n")
			var stdout, stderr bytes.Buffer
			if status := Run([]string{"lint", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 || stderr.Len() != 0 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
			if out := validateEmitted(t, root, stdout.Bytes()); !out.Complete || len(out.Diagnostics) != 0 {
				t.Fatal(out)
			}
		})
	}
	for _, entry := range []string{
		"MD072: {enabled: false, recursive: 'yes'}",
		"MD072: {enabled: false, case_sensitive: 1}",
		"MD072: {enabled: false, allow_directives: null}",
		"MD072: {enabled: false, order: asc}",
		"MD072: {enabled: true, recursive: 'yes'}",
		"MD072: {enabled: true, case_sensitive: 1}",
		"MD072: {enabled: true, allow_directives: null}",
		"MD072: {enabled: true, order: asc}",
	} {
		t.Run(entry, func(t *testing.T) {
			root := project(t)
			put(t, filepath.Join(root, ".mdchk.yaml"), availableConfig+"rules: {"+entry+"}\n")
			var stdout, stderr bytes.Buffer
			if status := Run([]string{"lint", "missing.md", "--format=json"}, &stdout, &stderr, "test"); status != 12 || stderr.Len() != 0 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
			if out := validateEmitted(t, root, stdout.Bytes()); len(out.Diagnostics) != 0 || len(out.Errors) == 0 {
				t.Fatal(out)
			}
		})
	}
}

func TestMD072ReportOnlyFix(t *testing.T) {
	const source = "---\nz: 1\na: 2\n---\n"
	root := project(t)
	put(t, filepath.Join(root, ".mdchk.yaml"), metadataOrderingConfiguration("MD072: {enabled: true}"))
	put(t, filepath.Join(root, "doc.md"), source)
	var stdout, stderr bytes.Buffer
	if status := Run([]string{"lint", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 10 || stderr.Len() != 0 {
		t.Fatal(status, stdout.String(), stderr.String())
	}
	out := validateEmitted(t, root, stdout.Bytes())
	if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD072" || out.Diagnostics[0].FixAvailable || out.Diagnostics[0].Message != `Metadata key "a" should come before "z".` || out.Diagnostics[0].ByteRange.Start != 9 || out.Diagnostics[0].ByteRange.End != 10 {
		t.Fatal(out)
	}
	stdout.Reset()
	stderr.Reset()
	if status := Run([]string{"fix", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 10 || stderr.Len() != 0 {
		t.Fatal(status, stdout.String(), stderr.String())
	}
	if fixed := validateEmitted(t, root, stdout.Bytes()); len(fixed.Changes) != 0 || len(fixed.Diagnostics) != 1 || fixed.Diagnostics[0].FixAvailable {
		t.Fatal(fixed)
	}
	if data, err := os.ReadFile("doc.md"); err != nil || string(data) != source {
		t.Fatal("fix changed metadata", string(data), err)
	}
}
