package md034_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
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

type md034NativeLiteral struct {
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

func md034NativeLiterals(t *testing.T) []md034NativeLiteral {
	t.Helper()
	b, e := os.ReadFile("testdata/md034.json")
	if e != nil {
		t.Fatal(e)
	}
	var v struct{ Cases []md034NativeLiteral }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) == 0 {
		t.Fatal(e)
	}
	return v.Cases
}
func md034NativeAssert(t *testing.T, out result.Result, tc md034NativeLiteral, view string) {
	t.Helper()
	if !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.DeferredFixes)+len(out.Documents)+len(out.SuppressedDiagnostics) != 0 || len(out.Diagnostics) != len(tc.Findings) {
		t.Fatal(out, tc.Findings)
	}
	for i, d := range out.Diagnostics {
		w := tc.Findings[i]
		if d.Rule != "MD034" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range != (result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}) || d.Snapshot != (result.Snapshot{View: view, SizeBytes: tc.Bytes, SHA256: tc.SHA}) {
			t.Fatal(d, w)
		}
	}
}
func TestCompiledPlatformMD034AnalysisFixtures(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, tc := range md034NativeLiterals(t) {
		if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
			t.Fatal("fixture source identity")
		}
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				conf = strings.Replace(conf, "MD034: {enabled: false}", "MD034: {enabled: true}", 1)
				if tc.Frontmatter != "" {
					conf += "frontmatter: {enabled: true, formats: [" + tc.Frontmatter + "]}\n"
				}
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", tc.Source)
				before := acceptance.ReadinessState(t, root)
				for _, command := range []string{"lint", "inspect", "fix"} {
					args := []string{command, "doc.md", "--format=json"}
					view := "original"
					if command == "fix" {
						args = append(args, "--dry-run")
						view = "candidate"
					}
					got := acceptance.MD014NativeRun(t, binary, root, args...)
					want := 0
					if command != "inspect" && len(tc.Findings) > 0 {
						want = 10
					}
					out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), command)
					if got.Status != want {
						t.Fatal(got)
					}
					md034NativeAssert(t, out, tc, view)
					acceptance.ReadinessAssertEqual(t, "MD034 analysis/manual preview", before, acceptance.ReadinessState(t, root))
				}
			})
		}
	}
}
func TestCompiledPlatformMD034DefaultOnAndInvalidAdmission(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, value := range []string{"omitted", "{}", "{enabled: false}", "{enabled: false, unknown: true}", "{allow_directives: 1}"} {
			t.Run(flavor+"/"+value, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				if value == "omitted" {
					conf = strings.Replace(conf, "  MD034: {enabled: false}\n", "", 1)
				} else {
					conf = strings.Replace(conf, "MD034: {enabled: false}", "MD034: "+value, 1)
				}
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", "https://example.org")
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
				acceptance.ReadinessAssertEqual(t, "MD034 default/settings", before, acceptance.ReadinessState(t, root))
			})
		}
	}
}
