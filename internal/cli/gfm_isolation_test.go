//go:build mdchk_conformance

package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/parser"
)

// Exercise production admission before and after every closed test-only scope.
func TestGFMPrivateScopesPreserveProductionAdmission(t *testing.T) {
	root := project(t)
	put(t, filepath.Join(root, "doc.md"), "- [X] ~~www.a.b~~\n\n| a |\n| - |\n\n<title>x</title>\n")
	_, _, baseline, err := configuredProviders()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(baseline, config.Capabilities{CommonMark: true, GFM: true, DirectiveProcessing: true, FrontmatterYAML: true, FrontmatterTOML: true, MetadataInterpretation: true}) {
		t.Fatal("production capabilities changed", baseline)
	}
	check := func() {
		t.Helper()
		for _, c := range []struct {
			body string
			want int
		}{
			{"version: 1\n", 15}, {"version: 1\nflavor: gfm\n", 15},
			{availableConfig, 15}, {availableConfig + "frontmatter: {enabled: false}\n", 15},
			{availableConfig + "frontmatter: {enabled: true}\n", 15},
			{availableConfig + "extensions: {tables: true}\n", 12},
			{"version: 1\nflavor: tables-only\n", 12},
		} {
			put(t, filepath.Join(root, config.Filename), c.body)
			var out, stderr bytes.Buffer
			got := Run([]string{"lint", "doc.md", "--format=json"}, &out, &stderr, "dev")
			actual := validateEmitted(t, t.TempDir(), out.Bytes())
			if got != c.want || (got == 15 && (len(actual.CheckedFiles) != 1 || len(actual.Coverage) == 0 || len(actual.Errors) != 0)) || (got == 12 && len(actual.CheckedFiles) != 0) {
				t.Fatalf("admission after scope: body=%q status=%d checked=%v", c.body, got, actual.CheckedFiles)
			}
		}
		_, _, after, err := configuredProviders()
		if err != nil || !reflect.DeepEqual(after, baseline) {
			t.Fatal("private scope mutated capabilities", err)
		}
	}
	check()
	for _, scope := range []string{"core", "tables-only", "tasks-only", "strike-only", "bare-only", "filter-only", "integrated"} {
		op, err := parser.NewOperationBudget(parser.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		r, err := parser.ParseConformance(context.Background(), parser.NewSource([]byte("- [x] ~~www.a.b~~ <title>\n\n| a |\n| - |\n")), op, scope)
		if err != nil || r.Status != parser.Complete {
			t.Fatal("actual closed scope", scope, err, r.Status)
		}
		check()
	}
}
