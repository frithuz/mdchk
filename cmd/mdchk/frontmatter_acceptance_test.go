package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompiledFrontmatterConsumers(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct{ flavor, format, marker string }{{"commonmark", "yaml", "---"}, {"gfm", "toml", "+++"}} {
		t.Run(tc.flavor, func(t *testing.T) {
			root := configurationProject(t)
			configuration := strings.Replace(md047Configuration(true, true), "flavor: commonmark", "flavor: "+tc.flavor, 1) + "frontmatter: {enabled: true, formats: [" + tc.format + "]}\n"
			putConfigurationFile(t, root, ".mdchk.yaml", configuration)
			header := "\xef\xbb\xbf" + tc.marker + "\r\n<!-- mdchk:disable MD047 -->\r\nbad: [\r\n" + tc.marker + "\r\n"
			original := header + "雪body"
			putConfigurationFile(t, root, "doc.md", original)
			lint := runConfiguration(t, binary, root, root, "lint", "doc.md", "--format=json")
			if lint.Status != 10 || !lint.Result.Complete || len(lint.Result.Diagnostics) != 1 || !lint.Result.Diagnostics[0].FixAvailable || lint.Result.Diagnostics[0].Range.Start.Line != 5 {
				t.Fatal(lint)
			}
			view, projection := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json", "--lines=5")
			if view.Status != 0 || projection.AST.Data == nil || projection.AST.Data.Frontmatter == nil || projection.AST.Data.Frontmatter.Format != tc.format || projection.AST.Data.Frontmatter.BodyOffset != len(header) || !projection.AST.Data.Frontmatter.ContextOnly {
				t.Fatal(view, projection.AST)
			}
			preview := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
			if preview.Status != 0 || len(preview.Result.Changes) != 1 || !preview.Result.Complete {
				t.Fatal(preview)
			}
			//nolint:gosec // Read only the test-owned temporary source fixture.
			data, err := os.ReadFile(filepath.Join(root, "doc.md"))
			if err != nil || string(data) != original {
				t.Fatal("compiled read-only operation changed source", err)
			}
		})
	}
}
