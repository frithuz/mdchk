package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func anchorCollisionConfiguration() string {
	config := strings.Replace(fixConfiguration(), "MD047: {enabled: true}", "MD047: {enabled: false}", 1)
	return strings.Replace(config, "MD080: {enabled: false}", "MD080: {enabled: true, allow_directives: true}", 1)
}

func TestMD080InheritedDirectiveDenials(t *testing.T) {
	for _, global := range []bool{false, true} {
		name := "rule"
		if global {
			name = "global"
		}
		t.Run(name, func(t *testing.T) {
			root := project(t)
			parent := anchorCollisionConfiguration()
			if global {
				parent += "directives: {enabled: false}\n"
			} else {
				parent = strings.Replace(parent, "allow_directives: true", "allow_directives: false", 1)
			}
			put(t, filepath.Join(root, ".mdchk.yaml"), parent)
			child := anchorCollisionConfiguration() + "directives: {enabled: true}\n"
			put(t, filepath.Join(root, "nested", ".mdchk.yaml"), child)
			put(t, filepath.Join(root, "nested", "doc.md"), "<!-- mdchk:disable MD080 -->\n## A\n## A\n")
			t.Chdir(filepath.Join(root, "nested"))
			var stdout, stderr bytes.Buffer
			if status := Run([]string{"lint", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 10 || stderr.Len() != 0 {
				t.Fatal(status, stdout.String(), stderr.String())
			}
			out := validateEmitted(t, root, stdout.Bytes())
			if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD080" || len(out.SuppressedDiagnostics) != 0 {
				t.Fatal(out)
			}
		})
	}
}
