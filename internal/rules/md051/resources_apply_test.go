package md051_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
)

// Apply re-checks the observed saved view: MD051 facts carry the saved snapshot,
// resource lookups create no write target, and every neighbor stays byte-identical.
// The saved-unavailable path needs an induced post-write read failure and is not
// reachable from a compiled-CLI fixture.
func TestCompiledProductionMD051ResourceFragmentsApplySavedView(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, row := range []struct {
		crossFile          string
		findings, coverage int
	}{{"true", 2, 1}, {"false", 1, 0}} {
		t.Run("cross_file="+row.crossFile, func(t *testing.T) {
			root := md051ViewsProject(t, row.crossFile)
			neighbors := map[string]string{}
			for _, name := range []string{"docs/other.md", "docs/broken.md"} {
				data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name))) //nolint:gosec // Test-owned synthetic fixture path.
				if err != nil {
					t.Fatal(err)
				}
				neighbors[name] = string(data)
			}
			got := acceptance.MD014NativeRun(t, binary, root, "fix", "docs/self.md", "--format=json")
			out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "fix")
			findings, coverage := md051Facts(out)
			if len(findings) != row.findings || len(coverage) != row.coverage || len(out.Changes) != 1 || out.Changes[0].Disposition != "written" {
				t.Fatal(findings, coverage, out.Changes, got.Stdout)
			}
			for _, f := range findings {
				if !strings.HasPrefix(f, "saved:") {
					t.Fatal(f)
				}
			}
			for _, c := range out.Coverage {
				if c.Rule == "MD051" && c.Snapshot.View != "saved" {
					t.Fatal(c)
				}
			}
			for name, want := range neighbors {
				after, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name))) //nolint:gosec // Test-owned synthetic fixture path.
				if err != nil || string(after) != want {
					t.Fatal("resource neighbor changed", name, err)
				}
			}
			written, err := os.ReadFile(filepath.Join(root, "docs", "self.md")) //nolint:gosec // Test-owned synthetic fixture path.
			if err != nil || !strings.HasSuffix(string(written), "#x)\n") {
				t.Fatal(string(written), err)
			}
		})
	}
}
