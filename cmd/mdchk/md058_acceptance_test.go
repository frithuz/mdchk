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

type md058NativeLiteral struct {
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

func md058NativeLiterals(t *testing.T) []md058NativeLiteral {
	t.Helper()
	b, e := os.ReadFile("../../internal/rules/testdata/md058.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "0329e5fe41d8e42d369e911602fe2085b3e9b002e7cd2beba0edb6ab45a4e095" {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md058NativeLiteral }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) == 0 {
		t.Fatal(e)
	}
	return v.Cases
}

func md058NativeAssert(t *testing.T, out result.Result, tc md058NativeLiteral, view string) {
	t.Helper()
	if !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.DeferredFixes)+len(out.Documents)+len(out.SuppressedDiagnostics) != 0 || len(out.Diagnostics) != len(tc.Findings) {
		t.Fatal(out, tc.Findings)
	}
	for i, d := range out.Diagnostics {
		w := tc.Findings[i]
		if d.Rule != "MD058" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range != (result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}) || d.Snapshot != (result.Snapshot{View: view, SizeBytes: tc.Bytes, SHA256: tc.SHA}) {
			t.Fatal(d, w)
		}
	}
}

func TestCompiledPlatformMD058AnalysisFixtures(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, tc := range md058NativeLiterals(t) {
		if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
			t.Fatal("fixture source identity")
		}
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				conf = strings.Replace(conf, "MD058: {enabled: false}", fmt.Sprintf("MD058: {enabled: %v}", tc.Options.Enabled), 1)
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
					md058NativeAssert(t, out, tc, view)
					readinessAssertEqual(t, "MD058 analysis/report-only preview", before, readinessState(t, root))
				}
			})
		}
	}
}

func md058NativeFindings(t *testing.T, out result.Result) []result.Diagnostic {
	t.Helper()
	var found []result.Diagnostic
	for _, d := range out.Diagnostics {
		if d.Rule == "MD058" {
			found = append(found, d)
		}
	}
	for _, c := range out.Coverage {
		if c.Rule == "MD058" {
			t.Fatal("MD058 coverage", c)
		}
	}
	return found
}

// The spec acceptance inputs run with only version and flavor configured, so
// MD058 is enabled by its default and other default rules run alongside it.
func TestCompiledPlatformMD058SpecExamplesWithDefaults(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	before, after := "Table requires a blank body line before it.", "Table requires a blank body line after it."
	point := func(message string, at, line, column int) result.Diagnostic {
		p := result.Position{Line: line, Column: column}
		return result.Diagnostic{Message: message, ByteRange: result.ByteRange{Start: at, End: at}, Range: result.Range{Start: p, End: p}}
	}
	for _, tc := range []struct {
		flavor, source string
		want           []result.Diagnostic
	}{
		{"gfm", "# Title\n| a |\n| - |\n", []result.Diagnostic{point(before, 8, 2, 1)}},
		{"gfm", "| a |\n| - |\n# Next\n", []result.Diagnostic{point(after, 11, 2, 6)}},
		{"gfm", "# Title\r\n| a |\r\n| - |\r\n# Next\r\n", []result.Diagnostic{point(before, 9, 2, 1), point(after, 21, 3, 6)}},
		{"gfm", "> # T\n> | a |\n> | - |\n", []result.Diagnostic{point(before, 8, 2, 3)}},
		{"gfm", "> # T\n>\n> | a |\n> | - |\n", nil},
		{"gfm", "[r]: /u\n| a |\n| - |\n", []result.Diagnostic{point(before, 8, 2, 1)}},
		{"gfm", "| a |\n| - |\n", nil},
		{"gfm", "# T\n\n| a |\n| - |\n\n# N\n", nil},
		{"gfm", "> | a |\n> | - |\n", nil},
		{"gfm", "- | a |\n  | - |\n", nil},
		{"gfm", "```\n| a |\n| - |\n```\n", nil},
		{"gfm", "<div>\n| a |\n| - |\n", nil},
		{"gfm", "Text\nA | B\n--- | ---\n", nil},
		{"gfm", "<!-- mdchk:disable MD058 -->\n\n# Title\n| a |\n| - |\n", nil},
		{"commonmark", "# Title\n| a |\n| - |\n", nil},
	} {
		t.Run(fmt.Sprintf("%s/%q", tc.flavor, tc.source), func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\nflavor: "+tc.flavor+"\n")
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := readinessState(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
				got := md014NativeRun(t, binary, root, args...)
				out := md014NativeJSON(t, []byte(got.Stdout), args[0])
				found := md058NativeFindings(t, out)
				if len(found) != len(tc.want) {
					t.Fatal(got, out)
				}
				for _, c := range out.Changes {
					for _, rule := range c.Rules {
						if rule == "MD058" {
							t.Fatal("MD058 change", c)
						}
					}
				}
				for _, d := range out.DeferredFixes {
					if d.Rule == "MD058" {
						t.Fatal("MD058 deferred fix", d)
					}
				}
				for i, d := range found {
					w := tc.want[i]
					if d.Message != w.Message || d.ByteRange != w.ByteRange || d.Range != w.Range || d.FixAvailable {
						t.Fatal(d, w)
					}
				}
				readinessAssertEqual(t, "MD058 default "+args[0], before, readinessState(t, root))
			}
			fixed := md014NativeRun(t, binary, root, "fix", "doc.md")
			if tc.source == "# Title\n| a |\n| - |\n" && tc.flavor == "gfm" && !strings.Contains(fixed.Stdout, "doc.md:2:1 MD058: Table requires a blank body line before it. [manual]") {
				t.Fatal(fixed)
			}
			//nolint:gosec // Read back only the test-owned fixture.
			if b, e := os.ReadFile(filepath.Join(root, "doc.md")); e != nil || string(b) != tc.source {
				t.Fatal("fix changed the report-only example", string(b), e)
			}
		})
	}
}

func TestCompiledPlatformMD058SettingsAndInvalidAdmission(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, value := range []string{"omitted", "{}", "{enabled: true}", "{enabled: false}", "{allow_directives: false}", "{unknown: true}", "{enabled: false, unknown: true}", "{enabled: \"yes\"}", "{allow_directives: \"false\"}"} {
		t.Run(value, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: gfm", 1)
			if value == "omitted" {
				conf = strings.Replace(conf, "  MD058: {enabled: false}\n", "", 1)
			} else {
				conf = strings.Replace(conf, "MD058: {enabled: false}", "MD058: "+value, 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "# Title\n| a |\n| - |\n")
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "lint")
			want, count := 10, 1
			switch value {
			case "{enabled: false}":
				want, count = 0, 0
			case "{unknown: true}", "{enabled: false, unknown: true}", "{enabled: \"yes\"}", "{allow_directives: \"false\"}":
				want, count = 12, 0
			}
			if got.Status != want || len(out.Diagnostics) != count || len(out.Coverage)+len(out.Changes)+len(out.Documents) != 0 {
				t.Fatal(got, out)
			}
			for _, d := range out.Diagnostics {
				if d.Rule != "MD058" || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: 8, End: 8}) {
					t.Fatal(d)
				}
			}
			validated := md014NativeRun(t, binary, root, "config", "validate", ".", "--format=json")
			if want == 12 {
				if len(out.Files) != 0 {
					t.Fatal("configuration failure acquired targets", out)
				}
				if validated.Status != 12 {
					t.Fatal(validated)
				}
			} else if validated.Status != 0 {
				t.Fatal(validated)
			}
			readinessAssertEqual(t, "MD058 default/settings", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD058DirectiveSuppression(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow-%v", allow), func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(cleanConfiguration(), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			conf = strings.Replace(conf, "flavor: commonmark", "flavor: gfm", 1)
			conf = strings.Replace(conf, "MD058: {enabled: false}", fmt.Sprintf("MD058: {enabled: true, allow_directives: %v}", allow), 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "<!-- mdchk:disable MD058 -->\n\n# Title\n| a |\n| - |\n# Next\n")
			got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "lint")
			visible, want, hidden := 0, 2, 0
			if allow {
				want, hidden = 0, 2
			}
			for _, d := range out.Diagnostics {
				if d.Rule == "MD058" {
					visible++
				}
			}
			if visible != want || len(out.SuppressedDiagnostics) != hidden || len(out.Coverage) != 0 {
				t.Fatal(got, out)
			}
		})
	}
}
