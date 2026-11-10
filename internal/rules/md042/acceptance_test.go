package md042_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
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

type md042NativeLiteral struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Frontmatter  string
	Findings     []struct {
		Bytes, Start, End [2]int
		Expected, Message string
	}
}

func md042NativeLiterals(t *testing.T) []md042NativeLiteral {
	t.Helper()
	b, e := os.ReadFile("testdata/md042.json")
	if e != nil {
		t.Fatal(e)
	}
	var v struct{ Cases []md042NativeLiteral }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) == 0 {
		t.Fatal(e)
	}
	return v.Cases
}
func md042NativeAssert(t *testing.T, out result.Result, tc md042NativeLiteral, view string) {
	t.Helper()
	if !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.DeferredFixes)+len(out.Documents)+len(out.SuppressedDiagnostics) != 0 || len(out.Diagnostics) != len(tc.Findings) {
		t.Fatal(out, tc.Findings)
	}
	for i, d := range out.Diagnostics {
		w := tc.Findings[i]
		if d.Rule != "MD042" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range != (result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}) || d.Snapshot != (result.Snapshot{View: view, SizeBytes: tc.Bytes, SHA256: tc.SHA}) {
			t.Fatal(d, w)
		}
	}
}
func TestCompiledPlatformMD042AnalysisFixtures(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, tc := range md042NativeLiterals(t) {
		if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
			t.Fatal("fixture source identity")
		}
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				conf = strings.Replace(conf, "MD042: {enabled: false}", "MD042: {enabled: true}", 1)
				if tc.Frontmatter != "" {
					conf += "frontmatter: {enabled: true, formats: [" + tc.Frontmatter + "]}\n"
				}
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				before := acceptance.ReadinessState(t, root)
				for _, command := range []string{"lint", "inspect", "fix", "apply"} {
					operation := command
					if command == "apply" {
						operation = "fix"
					}
					args := []string{operation, "doc.md", "--format=json"}
					view := "original"
					if command == "apply" {
						view = "saved"
					}
					if command == "fix" {
						args = append(args, "--dry-run")
						view = "candidate"
					}
					got := acceptance.MD014NativeRun(t, binary, root, args...)
					want := 0
					if command != "inspect" && len(tc.Findings) > 0 {
						want = 10
					}
					out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), operation)
					if command == "apply" && got.Status == 13 {
						if !acceptance.MD038NativeRefusal(t, got, out) {
							t.Fatal(got, out)
						}
						acceptance.ReadinessAssertEqual(t, "unsupported manual apply", before, acceptance.ReadinessState(t, root))
						continue
					}
					if got.Status != want {
						t.Fatal(got)
					}
					md042NativeAssert(t, out, tc, view)
					acceptance.ReadinessAssertEqual(t, "MD042 analysis/manual preview", before, acceptance.ReadinessState(t, root))
				}
			})
		}
	}
}
func TestCompiledPlatformMD042DefaultOnAndInvalidAdmission(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, value := range []string{"omitted", "{}", "{enabled: false}", "{enabled: false, unknown: true}", "{allow_directives: 1}"} {
			t.Run(flavor+"/"+value, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				if value == "omitted" {
					conf = strings.Replace(conf, "  MD042: {enabled: false}\n", "", 1)
				} else {
					conf = strings.Replace(conf, "MD042: {enabled: false}", "MD042: "+value, 1)
				}
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", "[x](#)")
				before := acceptance.ReadinessState(t, root)
				got := acceptance.MD014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
				out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "lint")
				want, count := 10, 1
				if value == "{enabled: false}" {
					want, count = 0, 0
				}
				if strings.Contains(value, "unknown") || strings.Contains(value, "allow_directives") {
					want, count = 12, 0
				}
				if got.Status != want || len(out.Diagnostics) != count || len(out.Changes)+len(out.Documents) != 0 {
					t.Fatal(got, out)
				}
				if want == 12 && len(out.Files) != 0 {
					t.Fatal("configuration failure acquired targets", out)
				}
				acceptance.ReadinessAssertEqual(t, "MD042 default/settings", before, acceptance.ReadinessState(t, root))
			})
		}
	}
}

func md042Configuration(flavor string) string {
	return strings.Replace(strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD042: {enabled: false}", "MD042: {}", 1)
}

func TestCompiledPlatformMD042FullSourceInspectionAndReset(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, tc := range []struct {
		name, source, setting, line string
		active, hidden              int
	}{
		{"suppressed", "<!-- mdchk:disable MD042 -->\n[x](#)", "{}", "2", 0, 1},
		{"enabled", "<!-- mdchk:enable MD042 -->\n[x](#)", "{enabled: false}", "2", 1, 0},
		{"denied", "<!-- mdchk:disable MD042 -->\n[x](#)", "{allow_directives: false}", "2", 1, 0},
		{"raw-context", "A <script>\n\n[x](#)\n\n</script>\n\n[y]()", "{}", "3", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := acceptance.Project(t)
			conf := strings.Replace(md042Configuration("gfm"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			conf = strings.Replace(conf, "MD042: {}", "MD042: "+tc.setting, 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			acceptance.PutFile(t, root, "doc.md", tc.source)
			before := acceptance.ReadinessState(t, root)
			got, p := acceptance.RunInspection(t, binary, root, root, "inspect", "doc.md", "--lines="+tc.line, "--format=json")
			if got.Status != 0 || len(got.Result.Diagnostics) != tc.active || len(got.Result.SuppressedDiagnostics) != tc.hidden || p.File.SelectedLines.Start == 0 {
				t.Fatal(got, p)
			}
			acceptance.ReadinessAssertEqual(t, "full source before inspection", before, acceptance.ReadinessState(t, root))
		})
	}
	root := acceptance.Project(t)
	conf := strings.Replace(md042Configuration("gfm"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
	acceptance.PutFile(t, root, ".mdchk.yaml", conf)
	acceptance.PutFile(t, root, "first.md", "<!-- mdchk:disable MD042 -->\n[x](#)")
	acceptance.PutFile(t, root, "second.md", "[x](#)")
	got := acceptance.RunProcess(t, binary, root, "lint", "first.md", "second.md", "--format=json")
	out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
	if got.Status != 10 || len(out.Diagnostics) != 1 || out.Diagnostics[0].Path != "second.md" || len(out.SuppressedDiagnostics) != 1 {
		t.Fatal(got)
	}
}

func TestCompiledPlatformMD042HeadersAndInheritedPermissions(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, tc := range []struct {
		name, source, extra string
		want, status        int
	}{
		{"yaml", "\ufeff---\nbad: [\n---\n[x](#)", "frontmatter: {enabled: true}\n", 1, 10},
		{"toml", "+++\nbad = [\n+++\n[x](#)", "frontmatter: {enabled: true}\n", 1, 10},
		{"unselected", "---\n[x](#)\n---\n[y]()", "frontmatter: {enabled: true, formats: [toml]}\n", 2, 10},
		{"incomplete", "+++\n[x](#)", "frontmatter: {enabled: true}\n", 0, 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := acceptance.Project(t)
			acceptance.PutFile(t, root, ".mdchk.yaml", md042Configuration("gfm")+tc.extra)
			acceptance.PutFile(t, root, "doc.md", tc.source)
			before := acceptance.ReadinessState(t, root)
			got := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
			out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
			if got.Status != tc.status || len(out.Diagnostics) != tc.want || out.Complete != (tc.status != 15) || len(out.Errors) != 0 {
				t.Fatal(got)
			}
			acceptance.ReadinessAssertEqual(t, "opaque source headers", before, acceptance.ReadinessState(t, root))
		})
	}
	for _, policy := range []string{"allowed", "global-denied", "rule-denied"} {
		t.Run(policy, func(t *testing.T) {
			root := acceptance.Project(t)
			parent := strings.Replace(md042Configuration("commonmark"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			if policy == "global-denied" {
				parent = strings.Replace(parent, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			if policy == "rule-denied" {
				parent = strings.Replace(parent, "MD042: {}", "MD042: {allow_directives: false}", 1)
			}
			child := strings.Replace(md042Configuration("commonmark"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			child = strings.Replace(child, "MD042: {}", "MD042: {allow_directives: true}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", parent)
			acceptance.PutFile(t, root, "nested/.mdchk.yaml", child)
			acceptance.PutFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD042 -->\n[x](#)")
			got, p := acceptance.RunInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=2", "--format=json")
			active, hidden := 1, 0
			if policy == "allowed" {
				active, hidden = 0, 1
			}
			if got.Status != 0 || len(got.Result.Diagnostics) != active || len(got.Result.SuppressedDiagnostics) != hidden || p.File.SelectedLines.Start != 2 {
				t.Fatal(got, p)
			}
		})
	}
}
