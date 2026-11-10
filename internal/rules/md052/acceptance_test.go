package md052_test

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

type md052NativeLiteral struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Options      struct {
		Enabled     bool
		Frontmatter string
	}
	Findings []struct {
		Bytes, Start, End [2]int
		Expected, Message string
	}
}

func md052NativeLiterals(t *testing.T) []md052NativeLiteral {
	t.Helper()
	b, e := os.ReadFile("testdata/md052.json")
	if e != nil {
		t.Fatal(e)
	}
	var v struct{ Cases []md052NativeLiteral }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) == 0 {
		t.Fatal(e)
	}
	return v.Cases
}

func md052NativeAssert(t *testing.T, out result.Result, tc md052NativeLiteral, view string) {
	t.Helper()
	if !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.DeferredFixes)+len(out.Documents)+len(out.SuppressedDiagnostics) != 0 || len(out.Diagnostics) != len(tc.Findings) {
		t.Fatal(out, tc.Findings)
	}
	for i, d := range out.Diagnostics {
		w := tc.Findings[i]
		if d.Rule != "MD052" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range != (result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}) || d.Snapshot != (result.Snapshot{View: view, SizeBytes: tc.Bytes, SHA256: tc.SHA}) {
			t.Fatal(d, w)
		}
	}
}

func TestCompiledPlatformMD052AnalysisFixtures(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, tc := range md052NativeLiterals(t) {
		if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
			t.Fatal("fixture source identity")
		}
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				if tc.Options.Enabled {
					conf = strings.Replace(conf, "MD052: {enabled: false}", "MD052: {enabled: true}", 1)
				} else {
					tc.Findings = nil
				}
				if tc.Options.Frontmatter != "" {
					conf += "frontmatter: {enabled: true, formats: [" + tc.Options.Frontmatter + "]}\n"
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
					md052NativeAssert(t, out, tc, view)
					acceptance.ReadinessAssertEqual(t, "MD052 analysis/report-only preview", before, acceptance.ReadinessState(t, root))
				}
			})
		}
	}
}

func TestCompiledPlatformMD052DefaultOnAndInvalidAdmission(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, value := range []string{"omitted", "{}", "{enabled: false}", "{allow_directives: false}", "{unknown: 1}", "{enabled: \"yes\"}", "{enabled: false, unknown: true}"} {
			t.Run(flavor+"/"+value, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				if value == "omitted" {
					conf = strings.Replace(conf, "  MD052: {enabled: false}\n", "", 1)
				} else {
					conf = strings.Replace(conf, "MD052: {enabled: false}", "MD052: "+value, 1)
				}
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", "[Guide][manual]\n")
				before := acceptance.ReadinessState(t, root)
				got := acceptance.MD014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
				out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "lint")
				want, count := 10, 1
				if value == "{enabled: false}" {
					want, count = 0, 0
				}
				invalid := strings.Contains(value, "unknown") || strings.Contains(value, "yes")
				if invalid {
					want, count = 12, 0
				}
				if got.Status != want || len(out.Diagnostics) != count || len(out.Changes)+len(out.Documents) != 0 {
					t.Fatal(got, out)
				}
				if count == 1 && (out.Diagnostics[0].Rule != "MD052" || out.Diagnostics[0].Range != (result.Range{Start: result.Position{Line: 1, Column: 1}, End: result.Position{Line: 1, Column: 16}})) {
					t.Fatal(out.Diagnostics)
				}
				if invalid {
					if len(out.Files) != 0 {
						t.Fatal("configuration failure acquired targets", out)
					}
					validated := acceptance.MD014NativeRun(t, binary, root, "config", "validate", ".", "--format=json")
					if validated.Status != 12 {
						t.Fatal(validated)
					}
				}
				acceptance.ReadinessAssertEqual(t, "MD052 default/settings", before, acceptance.ReadinessState(t, root))
			})
		}
	}
}
