//go:build darwin && arm64 && cgo

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFrontmatterApplyPreservesHeaderAndConverges(t *testing.T) {
	root := project(t)
	put(t, filepath.Join(root, ".mdchk.yaml"), fixConfiguration()+"frontmatter: {enabled: true}\n")
	header := "\xef\xbb\xbf---\r\nbad: [\r\n---\r\n"
	path := filepath.Join(root, "doc.md")
	put(t, path, header+"body")
	for pass, wantChanges := range []int{1, 0} {
		var stdout, stderr bytes.Buffer
		if status := Run([]string{"fix", "doc.md", "--format=json"}, &stdout, &stderr, "test"); status != 0 || stderr.Len() != 0 {
			t.Fatal(pass, status, stdout.String(), stderr.String())
		}
		out := validateEmitted(t, root, stdout.Bytes())
		if len(out.Changes) != wantChanges || !out.Complete {
			t.Fatal(out)
		}
		//nolint:gosec // Read only the test-owned temporary source fixture.
		data, err := os.ReadFile(path)
		if err != nil || string(data) != header+"body\r\n" {
			t.Fatal("saved source differs from preview candidate", err, string(data))
		}
	}
}
