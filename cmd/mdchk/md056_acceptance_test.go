package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

type md056NativeLiteral struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Options      struct{ Enabled bool }
	Findings     []struct {
		Bytes, Start, End [2]int
		Message           string
	}
}

func md056NativeLiterals(t *testing.T) []md056NativeLiteral {
	t.Helper()
	b, e := os.ReadFile("../../internal/rules/testdata/md056.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "99f108a6a78b9078e645d3c73b2697dec3c812958b3c124680b3b8dc4ff55dae" {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md056NativeLiteral }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) == 0 {
		t.Fatal(e)
	}
	return v.Cases
}

func md056NativeAssert(t *testing.T, out result.Result, tc md056NativeLiteral, view string) {
	t.Helper()
	if !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.DeferredFixes)+len(out.Documents)+len(out.SuppressedDiagnostics) != 0 || len(out.Diagnostics) != len(tc.Findings) {
		t.Fatal(out, tc.Findings)
	}
	for i, d := range out.Diagnostics {
		w := tc.Findings[i]
		if d.Rule != "MD056" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range != (result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}) || d.Snapshot != (result.Snapshot{View: view, SizeBytes: tc.Bytes, SHA256: tc.SHA}) {
			t.Fatal(d, w)
		}
	}
}

func TestCompiledPlatformMD056AnalysisFixtures(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, tc := range md056NativeLiterals(t) {
		if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
			t.Fatal("fixture source identity")
		}
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				conf = strings.Replace(conf, "MD056: {enabled: false}", fmt.Sprintf("MD056: {enabled: %v}", tc.Options.Enabled), 1)
				if !tc.Options.Enabled {
					tc.Findings = nil
				}
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				putConfigurationFile(t, root, "doc.md", tc.Source)
				before := readinessState(t, root)
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
					got := md014NativeRun(t, binary, root, args...)
					want := 0
					if command != "inspect" && len(tc.Findings) > 0 {
						want = 10
					}
					out := md014NativeJSON(t, []byte(got.Stdout), operation)
					if command == "apply" && got.Status == 13 {
						if !md038NativeRefusal(t, got, out) {
							t.Fatal(got, out)
						}
						readinessAssertEqual(t, "unsupported manual apply", before, readinessState(t, root))
						continue
					}
					if got.Status != want {
						t.Fatal(got)
					}
					md056NativeAssert(t, out, tc, view)
					readinessAssertEqual(t, "MD056 analysis/report-only preview", before, readinessState(t, root))
				}
			})
		}
	}
}

func TestCompiledPlatformMD056SpecExamplesWithDefaults(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	threeCells := result.Diagnostic{Message: "Row has 3 cells; table has 2 columns.", ByteRange: result.ByteRange{Start: 16, End: 25}, Range: result.Range{Start: result.Position{Line: 3, Column: 1}, End: result.Position{Line: 3, Column: 10}}}
	for _, tc := range []struct {
		flavor, source string
		want           []result.Diagnostic
	}{
		{"gfm", "A | B\n--- | ---\n1 | 2 | 3\n", []result.Diagnostic{threeCells}},
		{"gfm", "A | B\n--- | ---\n1 | 2\n", nil},
		{"gfm", "| A | B |\n| --- | --- |\n| `a|b` | c |\n", []result.Diagnostic{
			{Message: "Row has 3 cells; table has 2 columns.", ByteRange: result.ByteRange{Start: 24, End: 37}, Range: result.Range{Start: result.Position{Line: 3, Column: 1}, End: result.Position{Line: 3, Column: 14}}},
		}},
		{"gfm", "| A | B |\n| --- | --- |\n| `a\\|b` | c |\n", nil},
		{"gfm", "A | B\n--- | ---\n1\n", []result.Diagnostic{
			{Message: "Row has 1 cell; table has 2 columns.", ByteRange: result.ByteRange{Start: 16, End: 17}, Range: result.Range{Start: result.Position{Line: 3, Column: 1}, End: result.Position{Line: 3, Column: 2}}},
		}},
		{"gfm", "| A | B | C |\n| --- | --- |\n| 1 | 2 |\n", nil},
		{"commonmark", "A | B\n--- | ---\n1 | 2 | 3\n", nil},
	} {
		t.Run(fmt.Sprintf("%s/%q", tc.flavor, tc.source), func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\nflavor: "+tc.flavor+"\n")
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := readinessState(t, root)
			// Other default rules remain unavailable, so the status reports incompleteness.
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
				got := md014NativeRun(t, binary, root, args...)
				out := md014NativeJSON(t, []byte(got.Stdout), args[0])
				var found []result.Diagnostic
				for _, d := range out.Diagnostics {
					if d.Rule == "MD056" {
						found = append(found, d)
					}
				}
				for _, c := range out.Coverage {
					if c.Rule == "MD056" {
						t.Fatal("MD056 coverage", c)
					}
				}
				if len(found) != len(tc.want) || len(out.Changes)+len(out.DeferredFixes)+len(out.Documents) != 0 {
					t.Fatal(got, out)
				}
				for i, d := range found {
					w := tc.want[i]
					if d.Message != w.Message || d.ByteRange != w.ByteRange || d.Range != w.Range || d.FixAvailable {
						t.Fatal(d, w)
					}
				}
				readinessAssertEqual(t, "MD056 default "+args[0], before, readinessState(t, root))
			}
			fixed := md014NativeRun(t, binary, root, "fix", "doc.md")
			if len(tc.want) > 0 && !strings.Contains(fixed.Stdout, "doc.md:3:1 MD056: "+tc.want[0].Message+" [manual]") {
				t.Fatal(fixed)
			}
			//nolint:gosec // Read back only the test-owned fixture.
			if b, e := os.ReadFile(filepath.Join(root, "doc.md")); e != nil || string(b) != tc.source {
				t.Fatal("fix changed the report-only spec example", string(b), e)
			}
		})
	}
}

func TestCompiledPlatformMD056SettingsAndInvalidAdmission(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, value := range []string{"omitted", "{}", "{enabled: true}", "{enabled: false}", "{enabled: true, allow_directives: false}", "{style: x}", "{unknown: 1}", "{enabled: \"yes\"}", "{allow_directives: \"false\"}"} {
		t.Run(value, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: gfm", 1)
			if value == "omitted" {
				conf = strings.Replace(conf, "  MD056: {enabled: false}\n", "", 1)
			} else {
				conf = strings.Replace(conf, "MD056: {enabled: false}", "MD056: "+value, 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "A | B\n--- | ---\n1 | 2 | 3\n")
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "lint")
			want, count := 10, 1
			switch value {
			case "{enabled: false}":
				want, count = 0, 0
			case "{style: x}", "{unknown: 1}", "{enabled: \"yes\"}", "{allow_directives: \"false\"}":
				want, count = 12, 0
			}
			if got.Status != want || len(out.Diagnostics) != count || len(out.Coverage)+len(out.Changes)+len(out.Documents) != 0 {
				t.Fatal(got, out)
			}
			for _, d := range out.Diagnostics {
				if d.Rule != "MD056" || d.FixAvailable {
					t.Fatal(d)
				}
			}
			if want == 12 {
				if len(out.Files) != 0 {
					t.Fatal("configuration failure acquired targets", out)
				}
				validated := md014NativeRun(t, binary, root, "config", "validate", ".", "--format=json")
				if validated.Status != 12 {
					t.Fatal(validated)
				}
			}
			readinessAssertEqual(t, "MD056 default/settings", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD056DirectiveSuppression(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow-%v", allow), func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(cleanConfiguration(), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			conf = strings.Replace(conf, "flavor: commonmark", "flavor: gfm", 1)
			conf = strings.Replace(conf, "MD056: {enabled: false}", fmt.Sprintf("MD056: {enabled: true, allow_directives: %v}", allow), 1)
			conf = strings.Replace(conf, "MD087: {enabled: false}", "MD087: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "<!-- mdchk:disable MD056 -->\nA | B\n--- | ---\n1 | 2 | 3\n")
			got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "lint")
			visible, want, hidden := 0, 1, 0
			if allow {
				want, hidden = 0, 1
			}
			for _, d := range out.Diagnostics {
				switch d.Rule {
				case "MD056":
					visible++
				default:
					// A used directive is never reported as unused by MD087.
					t.Fatal(d)
				}
			}
			if visible != want || len(out.SuppressedDiagnostics) != hidden || len(out.Coverage) != 0 {
				t.Fatal(got, out)
			}
		})
	}
}
