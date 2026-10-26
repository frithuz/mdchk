package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func headingConfiguration(title bool) string {
	config := strings.Replace(fixConfiguration(), "MD047: {enabled: true}", "MD047: {enabled: false}", 1)
	option := "false"
	if title {
		option = "true"
	}
	return strings.Replace(config, "MD001: {enabled: false}", "MD001: {enabled: true, frontmatter_title: "+option+"}", 1) + "frontmatter: {enabled: true}\n"
}

func TestMD001ProductionCLI(t *testing.T) {
	for _, tc := range []struct {
		name, source     string
		title            bool
		findings, status int
	}{
		{"body", "# Guide\n### Install\n", false, 1, 10},
		{"YAML opt-in", "---\ntitle: Guide\n---\n### Install\n", true, 1, 10},
		{"TOML opt-in", "+++\ntitle = \"Guide\"\n+++\n### Install\n", true, 1, 10},
		{"default no title", "---\ntitle: Guide\n---\n### Install\n", false, 0, 0},
		{"unknown partial", "---\ntitle: [\n---\n### First\n##### Later\n", true, 1, 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := project(t)
			put(t, filepath.Join(root, ".mdchk.yaml"), headingConfiguration(tc.title))
			path := filepath.Join(root, "doc.md")
			put(t, path, tc.source)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
				var stdout, stderr bytes.Buffer
				status := Run(args, &stdout, &stderr, "test")
				if status != tc.status || stderr.Len() != 0 {
					t.Fatal(status, stdout.String(), stderr.String())
				}
				out := validateEmitted(t, root, stdout.Bytes())
				if len(out.Diagnostics) != tc.findings || len(out.Changes) != 0 || len(out.Documents) != 0 || out.Complete != (tc.status != 15) {
					t.Fatal(out)
				}
				for _, d := range out.Diagnostics {
					if d.Rule != "MD001" || d.FixAvailable {
						t.Fatal(d)
					}
				}
				//nolint:gosec // Only the fixture's own temporary path is read.
				data, err := os.ReadFile(path)
				if err != nil || string(data) != tc.source {
					t.Fatal("analysis/preview changed source", err)
				}
			}
		})
	}
}

func TestMD001ProductionAdmission(t *testing.T) {
	for _, entry := range []string{
		"MD001: {enabled: false, frontmatter_title: null}",
		"MD001: {enabled: false, frontmatter_title: 'true'}",
		"MD001: {enabled: false, frontmatter_title: 1}",
		"MD001: {enabled: false, unknown: true}",
		"MD001: {enabled: 'true'}",
		"MD001: {enabled: false, allow_directives: null}",
		"MD025: {enabled: true, level: 7}",
		"MD063: {enabled: true}",
		"MD072: {enabled: true}",
	} {
		t.Run(entry, func(t *testing.T) {
			root := project(t)
			put(t, filepath.Join(root, ".mdchk.yaml"), availableConfig+"rules: {"+entry+"}\n")
			var stdout, stderr bytes.Buffer
			if status := Run([]string{"lint", "missing.md", "--format=json"}, &stdout, &stderr, "test"); status != 12 || stderr.Len() != 0 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
			out := validateEmitted(t, root, stdout.Bytes())
			if len(out.Diagnostics) != 0 || len(out.Errors) == 0 {
				t.Fatal(out)
			}
		})
	}
}

func TestMD001InheritedDirectiveDenials(t *testing.T) {
	for _, global := range []bool{false, true} {
		name := "rule"
		if global {
			name = "global"
		}
		t.Run(name, func(t *testing.T) {
			root := project(t)
			parent := headingConfiguration(false)
			if global {
				parent += "directives: {enabled: false}\n"
			} else {
				parent = strings.Replace(parent, "frontmatter_title: false}", "frontmatter_title: false, allow_directives: false}", 1)
			}
			put(t, filepath.Join(root, ".mdchk.yaml"), parent)
			child := strings.Replace(headingConfiguration(false), "frontmatter_title: false}", "frontmatter_title: false, allow_directives: true}", 1) + "directives: {enabled: true}\n"
			put(t, filepath.Join(root, "nested", ".mdchk.yaml"), child)
			put(t, filepath.Join(root, "nested", "doc.md"), "# Root\n<!-- mdchk:disable MD001 -->\n### Active\n")
			t.Chdir(filepath.Join(root, "nested"))
			var stdout, stderr bytes.Buffer
			if status := Run([]string{"lint", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 10 || stderr.Len() != 0 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
			out := validateEmitted(t, root, stdout.Bytes())
			if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD001" || len(out.SuppressedDiagnostics) != 0 {
				t.Fatal(out)
			}
		})
	}
}
