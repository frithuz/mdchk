package md044_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
)

// TestMain removes this process's private builds once every test has finished
// with them.
func TestMain(m *testing.M) {
	m.Run()
	acceptance.Cleanup()
}

func md044Configuration(flavor string) string {
	return strings.Replace(strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD044: {enabled: false}", "MD044: {enabled: true, names: [GitHub]}", 1)
}

func TestCompiledPlatformMD044ProductionAnalysis(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, tc := range []struct {
		id, source, flavor string
		ranges             [][2]int
	}{
		{"basic", "Use github.", "commonmark", [][2]int{{4, 10}}},
		{"scalar", "π github!", "gfm", [][2]int{{3, 9}}},
		{"entity-barrier", "git&#104;ub github", "commonmark", [][2]int{{12, 18}}},
		{"commonmark-url-text", "https://github.com", "commonmark", [][2]int{{8, 14}}},
		{"gfm-bare-link", "https://github.com", "gfm", nil},
	} {
		t.Run(tc.id, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", md044Configuration(tc.flavor))
			acceptance.PutFile(t, root, "doc.md", tc.source)
			before := acceptance.ReadinessState(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"fix", "doc.md", "--format=json"}} {
				got := acceptance.RunProcess(t, binary, root, args...)
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
				if args[0] == "fix" && args[1] == "doc.md" && acceptance.MD038NativeRefusal(t, got, out) {
					continue
				}
				status := 0
				if len(tc.ranges) > 0 && args[0] != "inspect" {
					status = 10
				}
				if got.Status != status || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(tc.ranges) || len(out.Changes)+len(out.DeferredFixes)+len(out.Documents) != 0 {
					t.Fatal(got)
				}
				for i, d := range out.Diagnostics {
					w := tc.ranges[i]
					if args[0] != "fix" && d.Snapshot.View != "original" || args[0] == "fix" && d.Snapshot.View != "candidate" && d.Snapshot.View != "saved" {
						t.Fatal(d.Snapshot)
					}
					if d.Rule != "MD044" || d.Message != "Configured name uses different capitalization." || d.ByteRange.Start != w[0] || d.ByteRange.End != w[1] || d.FixAvailable || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.source))) || d.Snapshot.SizeBytes != len(tc.source) {
						t.Fatal(d)
					}
				}
			}
			acceptance.ReadinessAssertEqual(t, "manual MD044", before, acceptance.ReadinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD044OptionsAndInheritance(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, setting := range []string{"{enabled: false, names: null}", "{enabled: false, names: [GitHub, 1]}", "{enabled: false, code_blocks: 1}", "{enabled: false, unknown: true}", "{enabled: false, allow_directives: null}"} {
		t.Run(setting, func(t *testing.T) {
			root := acceptance.Project(t)
			conf := strings.Replace(md044Configuration("gfm"), "MD044: {enabled: true, names: [GitHub]}", "MD044: "+setting, 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			got := acceptance.RunProcess(t, binary, root, "lint", "missing.md", "--format=json")
			out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
			if got.Status != 12 || len(out.CheckedFiles) != 0 {
				t.Fatal(got)
			}
		})
	}
	for _, denial := range []string{"allowed", "global", "rule"} {
		t.Run(denial, func(t *testing.T) {
			root := acceptance.Project(t)
			parent := strings.Replace(md044Configuration("commonmark"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			if denial == "global" {
				parent = strings.Replace(parent, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			if denial == "rule" {
				parent = strings.Replace(parent, "names: [GitHub]", "names: [GitHub], allow_directives: false", 1)
			}
			child := strings.Replace(md044Configuration("commonmark"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			child = strings.Replace(child, "names: [GitHub]", "names: [GitHub], allow_directives: true", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", parent)
			acceptance.PutFile(t, root, "nested/.mdchk.yaml", child)
			acceptance.PutFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD044 -->\ngithub")
			got, p := acceptance.RunInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=2", "--format=json")
			active, hidden := 1, 0
			if denial == "allowed" {
				active, hidden = 0, 1
			}
			if got.Status != 0 || len(got.Result.Diagnostics) != active || len(got.Result.SuppressedDiagnostics) != hidden || p.File.SelectedLines.Start != 2 {
				t.Fatal(got, p)
			}
		})
	}
}

func TestCompiledPlatformMD044NoCGOApply(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedNoCGOExecutable(t)
	root := acceptance.Project(t)
	acceptance.PutFile(t, root, ".mdchk.yaml", md044Configuration("gfm"))
	acceptance.PutFile(t, root, "doc.md", "github")
	before := acceptance.ReadinessState(t, root)
	got := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
	out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
	acceptance.AssertCGO0Apply(t, got, out, before, acceptance.ReadinessState(t, root))
}

func TestCompiledPlatformMD044MD047Composition(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, source := range []string{"`github`", "`github` tail"} {
		t.Run(source, func(t *testing.T) {
			root := acceptance.Project(t)
			conf := strings.Replace(md044Configuration("gfm"), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			acceptance.PutFile(t, root, "doc.md", source)
			before := acceptance.ReadinessState(t, root)
			dry := acceptance.RunProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
			preview := acceptance.ValidateOperationJSON(t, []byte(dry.Stdout), "fix")
			protected := source == "`github`"
			changes, deferred := 1, 0
			if protected {
				changes, deferred = 0, 1
			}
			if dry.Status != 10 && protected || dry.Status != 0 && !protected || !preview.Complete || len(preview.Changes) != changes || len(preview.DeferredFixes) != deferred {
				t.Fatal(dry)
			}
			readyness := acceptance.ReadinessState(t, root)
			acceptance.ReadinessAssertEqual(t, "preview", before, readyness)
			got := acceptance.RunProcess(t, binary, root, "fix", "doc.md", "--format=json")
			out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
			if acceptance.MD038NativeRefusal(t, got, out) {
				acceptance.ReadinessAssertEqual(t, "unsupported", before, acceptance.ReadinessState(t, root))
				return
			}
			//nolint:gosec // Fixed isolated test-owned document.
			data, err := os.ReadFile(filepath.Join(root, "doc.md"))
			if err != nil {
				t.Fatal(err)
			}
			want := source
			status := 10
			if !protected {
				want += "\n"
				status = 0
			}
			if got.Status != status || string(data) != want || len(out.Changes) != changes || len(out.Diagnostics) != deferred {
				t.Fatal(got, string(data))
			}
			if !protected && (len(out.Changes[0].Rules) != 1 || out.Changes[0].Rules[0] != "MD047" || out.Changes[0].Disposition != "written") {
				t.Fatal(out.Changes)
			}
		})
	}
}
