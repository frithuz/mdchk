//go:build darwin && arm64 && cgo

package cli

import (
	"path/filepath"
	"testing"

	fixtures "github.com/frithuz/mdchk/internal/testutil/frontmatter"
)

func TestFrontmatterOperationParityNativeApply(t *testing.T) {
	rows, err := fixtures.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if !row.Fix || row.Header == nil {
			continue
		}
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(row.ID+"/"+flavor, func(t *testing.T) {
				root := project(t)
				path := filepath.Join(root, "doc.md")
				put(t, filepath.Join(root, ".mdchk.yaml"), frontmatterParityConfiguration(t, row, flavor))
				put(t, path, row.Source)
				for pass, wantChanges := range []int{1, 0} {
					status, out := frontmatterParityRun(t, root, "fix", "doc.md", "--format=json")
					if status != 0 || !out.Complete || len(out.Changes) != wantChanges || len(out.Diagnostics) != 0 {
						t.Fatal(pass, status, out)
					}
					assertFrontmatterParityBytes(t, path, row.Candidate)
					status, view := frontmatterParityRun(t, root, "inspect", "doc.md", "--format=json")
					if status != 0 {
						t.Fatal(status, view)
					}
					saved := row
					saved.Source = row.Candidate
					assertFrontmatterParityInspection(t, saved, view)
					assertFrontmatterParityBytes(t, path, row.Candidate)
				}
			})
		}
	}
}
