package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tableOfContentsConfiguration(entry string) string {
	config := strings.Replace(fixConfiguration(), "MD047: {enabled: true}", "MD047: {enabled: false}", 1)
	config = strings.Replace(config, "MD094: {enabled: true}", "MD094: {enabled: false}", 1)
	return strings.Replace(config, "MD073: {enabled: false}", entry, 1)
}

func TestMD073ProductionAdmission(t *testing.T) {
	for _, entry := range []string{
		"MD073: {enabled: true, allow_directives: true}",
		"MD073: {enabled: true, allow_directives: false}",
		"MD073: {enabled: true, start_marker: '<!-- toc -->', end_marker: '<!-- /toc -->', min_level: 2, max_level: 2}",
	} {
		t.Run(entry, func(t *testing.T) {
			root := project(t)
			put(t, filepath.Join(root, ".mdchk.yaml"), tableOfContentsConfiguration(entry))
			put(t, filepath.Join(root, "doc.md"), "<!-- toc -->\n- [Install](#install)\n<!-- /toc -->\n\n## Install\n")
			var stdout, stderr bytes.Buffer
			if status := Run([]string{"lint", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 || stderr.Len() != 0 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
			if out := validateEmitted(t, root, stdout.Bytes()); !out.Complete || len(out.Diagnostics) != 0 {
				t.Fatal(out)
			}
		})
	}
	for _, enabled := range []string{"false", "true"} {
		for _, option := range []string{
			"start_marker: toc",
			"end_marker: '<!-- toc -->'",
			"min_level: 0",
			"max_level: 7",
			"min_level: 3, max_level: 2",
			"allow_directives: null",
			"marker: '<!-- toc -->'",
		} {
			entry := "MD073: {enabled: " + enabled + ", " + option + "}"
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
}

func TestMD073ReportOnlyFix(t *testing.T) {
	const source = "<!-- toc -->\n- [Usage](#usage)\n<!-- /toc -->\n\n## Install\n"
	root := project(t)
	put(t, filepath.Join(root, ".mdchk.yaml"), tableOfContentsConfiguration("MD073: {enabled: true}"))
	put(t, filepath.Join(root, "doc.md"), source)
	var stdout, stderr bytes.Buffer
	if status := Run([]string{"lint", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 10 || stderr.Len() != 0 {
		t.Fatal(status, stdout.String(), stderr.String())
	}
	out := validateEmitted(t, root, stdout.Bytes())
	if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD073" || out.Diagnostics[0].FixAvailable || out.Diagnostics[0].Message != `Table of contents entry does not match heading "Install" (#install).` || out.Diagnostics[0].ByteRange.Start != 13 || out.Diagnostics[0].ByteRange.End != 30 {
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
		t.Fatal("fix changed the table of contents", string(data), err)
	}
}
