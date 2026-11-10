package md053_test

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

type md053NativeLiteral struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Options      struct {
		Enabled       bool
		IgnoredLabels []string `json:"ignored_labels"`
	}
	Findings []struct {
		Bytes, Start, End [2]int
		Expected, Message string
	}
}

func md053NativeLiterals(t *testing.T) []md053NativeLiteral {
	t.Helper()
	b, e := os.ReadFile("testdata/md053.json")
	if e != nil {
		t.Fatal(e)
	}
	var v struct{ Cases []md053NativeLiteral }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) == 0 {
		t.Fatal(e)
	}
	return v.Cases
}

func md053NativeAssert(t *testing.T, out result.Result, tc md053NativeLiteral, view string) {
	t.Helper()
	if !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.DeferredFixes)+len(out.Documents)+len(out.SuppressedDiagnostics) != 0 || len(out.Diagnostics) != len(tc.Findings) {
		t.Fatal(out, tc.Findings)
	}
	for i, d := range out.Diagnostics {
		w := tc.Findings[i]
		if d.Rule != "MD053" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range != (result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}) || d.Snapshot != (result.Snapshot{View: view, SizeBytes: tc.Bytes, SHA256: tc.SHA}) {
			t.Fatal(d, w)
		}
	}
}

func TestCompiledPlatformMD053AnalysisFixtures(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, tc := range md053NativeLiterals(t) {
		if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
			t.Fatal("fixture source identity")
		}
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				if tc.Options.Enabled {
					setting := "MD053: {enabled: true}"
					if tc.Options.IgnoredLabels != nil {
						labels, e := json.Marshal(tc.Options.IgnoredLabels)
						if e != nil {
							t.Fatal(e)
						}
						setting = "MD053: {enabled: true, ignored_labels: " + string(labels) + "}"
					}
					conf = strings.Replace(conf, "MD053: {enabled: false}", setting, 1)
				} else {
					tc.Findings = nil
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
					md053NativeAssert(t, out, tc, view)
					acceptance.ReadinessAssertEqual(t, "MD053 analysis/report-only preview", before, acceptance.ReadinessState(t, root))
				}
			})
		}
	}
}

func TestCompiledPlatformMD053DefaultOnAndInvalidAdmission(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, value := range []string{"omitted", "{}", "{enabled: false}", "{allow_directives: false}", "{ignored_labels: []}", "{ignored_labels: [other]}", "{unknown: 1}", "{enabled: \"yes\"}", "{enabled: false, unknown: true}", "{ignored_labels: foo}", "{ignored_labels: [1]}"} {
			t.Run(flavor+"/"+value, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				if value == "omitted" {
					conf = strings.Replace(conf, "  MD053: {enabled: false}\n", "", 1)
				} else {
					conf = strings.Replace(conf, "MD053: {enabled: false}", "MD053: "+value, 1)
				}
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", "[unused]: guide.md\n")
				before := acceptance.ReadinessState(t, root)
				got := acceptance.MD014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
				out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "lint")
				want, count := 10, 1
				if value == "{enabled: false}" {
					want, count = 0, 0
				}
				invalid := strings.Contains(value, "unknown") || strings.Contains(value, "yes") || strings.Contains(value, "foo") || strings.Contains(value, "[1]")
				if invalid {
					want, count = 12, 0
				}
				if got.Status != want || len(out.Diagnostics) != count || len(out.Changes)+len(out.Documents) != 0 {
					t.Fatal(got, out)
				}
				if count == 1 && (out.Diagnostics[0].Rule != "MD053" || out.Diagnostics[0].Range != (result.Range{Start: result.Position{Line: 1, Column: 1}, End: result.Position{Line: 1, Column: 10}})) {
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
				acceptance.ReadinessAssertEqual(t, "MD053 default/settings", before, acceptance.ReadinessState(t, root))
			})
		}
	}
}
