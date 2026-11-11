package md083_test

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
	"github.com/frithuz/mdchk/internal/result"
)

// TestMain removes this process's private builds once every test has finished
// with them.
func TestMain(m *testing.M) {
	m.Run()
	acceptance.Cleanup()
}

func md083Configuration(flavor, setting string) string {
	return acceptance.RuleConfiguration(flavor, "MD083", setting)
}

func TestCompiledPlatformMD083ProductionAnalysis(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	type finding struct {
		bytes      [2]int
		start, end result.Position
	}
	for _, tc := range []struct {
		id, source, flavor, setting string
		findings                    []finding
	}{
		{"basic", "cafÃ©", "commonmark", "{enabled: true}", []finding{{[2]int{3, 5}, result.Position{Line: 1, Column: 4}, result.Position{Line: 1, Column: 5}}}},
		{"accepted", "café", "gfm", "{enabled: true}", nil},
		{"defaults", "Â© donâ€™t", "gfm", "{enabled: true}", []finding{{[2]int{0, 2}, result.Position{Line: 1, Column: 1}, result.Position{Line: 1, Column: 2}}, {[2]int{8, 13}, result.Position{Line: 1, Column: 7}, result.Position{Line: 1, Column: 9}}}},
		{"lowercase", "cafã©", "gfm", "{enabled: true}", nil},
		{"overlap", "cafÃ©", "commonmark", "{enabled: true, sequences: [Ã, Ã©]}", []finding{{[2]int{3, 7}, result.Position{Line: 1, Column: 4}, result.Position{Line: 1, Column: 6}}}},
		{"empty-sequences", "cafÃ©", "commonmark", "{enabled: true, sequences: ['']}", nil},
		{"crlf", "a\r\ncafÃ©", "gfm", "{enabled: true}", []finding{{[2]int{6, 8}, result.Position{Line: 2, Column: 4}, result.Position{Line: 2, Column: 5}}}},
		{"code-span", "Use `cafÃ©`.", "gfm", "{enabled: true}", nil},
	} {
		t.Run(tc.id, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", md083Configuration(tc.flavor, tc.setting))
			acceptance.PutFile(t, root, "doc.md", tc.source)
			before := acceptance.ReadinessState(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"fix", "doc.md", "--format=json"}} {
				got := acceptance.RunProcess(t, binary, root, args...)
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
				if args[0] == "fix" && args[1] == "doc.md" && acceptance.MD038NativeRefusal(t, got, out) {
					continue
				}
				status := 0
				if len(tc.findings) > 0 && args[0] != "inspect" {
					status = 10
				}
				if got.Status != status || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(tc.findings) || len(out.Changes)+len(out.DeferredFixes)+len(out.Documents) != 0 {
					t.Fatal(got)
				}
				for i, d := range out.Diagnostics {
					w := tc.findings[i]
					if args[0] != "fix" && d.Snapshot.View != "original" || args[0] == "fix" && d.Snapshot.View != "candidate" && d.Snapshot.View != "saved" {
						t.Fatal(d.Snapshot)
					}
					if d.Rule != "MD083" || d.Message != "Text may be corrupted by an encoding mismatch." || d.ByteRange.Start != w.bytes[0] || d.ByteRange.End != w.bytes[1] || d.Range.Start != w.start || d.Range.End != w.end || d.FixAvailable || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.source))) || d.Snapshot.SizeBytes != len(tc.source) {
						t.Fatal(d)
					}
				}
			}
			acceptance.ReadinessAssertEqual(t, "report-only MD083", before, acceptance.ReadinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD083DefaultDisabled(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	root := acceptance.Project(t)
	acceptance.PutFile(t, root, ".mdchk.yaml", acceptance.AvailableConfiguration)
	acceptance.PutFile(t, root, "doc.md", "# cafÃ©\n")
	got := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
	out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
	if got.Status != 0 || !out.Complete {
		t.Fatal(got)
	}
	for _, d := range out.Diagnostics {
		if d.Rule == "MD083" {
			t.Fatal(d)
		}
	}
	for _, c := range out.Coverage {
		if c.Rule == "MD083" {
			t.Fatal(c)
		}
	}
}

func TestCompiledPlatformMD083OptionsAndInheritance(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, setting := range []string{"{enabled: false, sequences: null}", "{enabled: false, sequences: Ã}", "{enabled: false, sequences: [Ã, 1]}", "{enabled: false, unknown: true}", "{enabled: false, allow_directives: null}"} {
		t.Run(setting, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", md083Configuration("gfm", setting))
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
			parent := strings.Replace(md083Configuration("commonmark", "{enabled: true}"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			if denial == "global" {
				parent = strings.Replace(parent, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			if denial == "rule" {
				parent = strings.Replace(parent, "MD083: {enabled: true}", "MD083: {enabled: true, allow_directives: false}", 1)
			}
			child := strings.Replace(md083Configuration("commonmark", "{enabled: true, allow_directives: true}"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", parent)
			acceptance.PutFile(t, root, "nested/.mdchk.yaml", child)
			acceptance.PutFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD083 -->\ncafÃ©")
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
