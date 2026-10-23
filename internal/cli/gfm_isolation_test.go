//go:build mdchk_conformance

package cli

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/parser"
)

// Exercise production admission before and after every closed test-only scope.
func TestGFMPrivateScopesPreserveProductionAdmission(t *testing.T) {
	root := project(t)
	_, _, baseline, err := configuredProviders()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(baseline, config.Capabilities{CommonMark: true, DirectiveProcessing: true}) {
		t.Fatal("production capabilities changed", baseline)
	}
	check := func() {
		t.Helper()
		for _, c := range []struct {
			body string
			want int
		}{
			{"version: 1\n", 12}, {"version: 1\nflavor: gfm\n", 12},
			{availableConfig, 0}, {availableConfig + "frontmatter: {enabled: false}\n", 0},
			{availableConfig + "frontmatter: {enabled: true}\n", 12},
			{availableConfig + "extensions: {tables: true}\n", 12},
			{"version: 1\nflavor: tables-only\n", 12},
		} {
			put(t, filepath.Join(root, config.Filename), c.body)
			called := 0
			var out, stderr bytes.Buffer
			got := run([]string{"lint", "not-opened.md"}, &out, &stderr, "dev", prepareLint, func(*lintInvocation, io.Writer, io.Writer) int { called++; return 0 })
			if got != c.want || called != map[bool]int{true: 1, false: 0}[c.want == 0] {
				t.Fatalf("admission after scope: body=%q status=%d called=%d out=%s", c.body, got, called, &out)
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
