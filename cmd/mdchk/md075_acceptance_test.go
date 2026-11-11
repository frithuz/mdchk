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

type md075NativeLiteral struct {
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
		Message           string
	}
}

func md075NativeLiterals(t *testing.T) []md075NativeLiteral {
	t.Helper()
	b, e := os.ReadFile("../../internal/rules/md075/testdata/md075.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "ab1f0cdd6c5e51fea5e3d7a9ea02bc4417c9d1ca264b2a5e26cbed3afa12b497" {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md075NativeLiteral }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) == 0 {
		t.Fatal(e)
	}
	return v.Cases
}

func md075NativeAssert(t *testing.T, out result.Result, tc md075NativeLiteral, view string) {
	t.Helper()
	if !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.DeferredFixes)+len(out.Documents)+len(out.SuppressedDiagnostics) != 0 || len(out.Diagnostics) != len(tc.Findings) {
		t.Fatal(out, tc.Findings)
	}
	for i, d := range out.Diagnostics {
		w := tc.Findings[i]
		if d.Rule != "MD075" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range != (result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}) || d.Snapshot != (result.Snapshot{View: view, SizeBytes: tc.Bytes, SHA256: tc.SHA}) {
			t.Fatal(d, w)
		}
	}
}

func TestCompiledPlatformMD075AnalysisFixtures(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, tc := range md075NativeLiterals(t) {
		if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
			t.Fatal("fixture source identity")
		}
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				conf = strings.Replace(conf, "MD075: {enabled: false}", fmt.Sprintf("MD075: {enabled: %v}", tc.Options.Enabled), 1)
				if !tc.Options.Enabled {
					tc.Findings = nil
				}
				if tc.Options.Frontmatter != "" {
					conf += "frontmatter: {enabled: true, formats: [" + tc.Options.Frontmatter + "]}\n"
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
					md075NativeAssert(t, out, tc, view)
					readinessAssertEqual(t, "MD075 analysis/report-only preview", before, readinessState(t, root))
				}
			})
		}
	}
}

func TestCompiledPlatformMD075SpecExamplesWithDefaults(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	finding := func(start, end int, from, to [2]int) []result.Diagnostic {
		return []result.Diagnostic{{Message: "Pipe rows are not part of a table.", ByteRange: result.ByteRange{Start: start, End: end}, Range: result.Range{Start: result.Position{Line: from[0], Column: from[1]}, End: result.Position{Line: to[0], Column: to[1]}}}}
	}
	for _, tc := range []struct {
		flavor, source string
		want           []result.Diagnostic
	}{
		{"gfm", "A | B\n1 | 2\n", finding(0, 11, [2]int{1, 1}, [2]int{2, 6})},
		{"gfm", "A | B\r\n1 | 2\r\n", finding(0, 12, [2]int{1, 1}, [2]int{2, 6})},
		{"gfm", "| A | B |\n| --- |\n| 1 |\n", finding(0, 23, [2]int{1, 1}, [2]int{3, 6})},
		{"gfm", "A | B\n--- | ---\n1 | 2\n", nil},
		{"gfm", "| a | b |\n| - | - |\n| 1 |\n| 1 | 2 | 3 |\n", nil},
		{"gfm", "Choose a | b here.\nNext line.\n", nil},
		{"gfm", "a \\| b\nc \\| d\n", nil},
		{"gfm", "```\na | b\nc | d\n```\n", nil},
		{"gfm", "a | b\n\nc | d\n", nil},
		{"gfm", "> A | B\n> 1 | 2\n", finding(2, 15, [2]int{1, 3}, [2]int{2, 8})},
		{"gfm", "- A | B\n  1 | 2\n", finding(2, 15, [2]int{1, 3}, [2]int{2, 8})},
		{"commonmark", "A | B\n1 | 2\n", nil},
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
					if d.Rule == "MD075" {
						found = append(found, d)
					}
					// MD075 and MD056 never report the same rows.
					if d.Rule == "MD056" && len(tc.want) > 0 {
						t.Fatal("MD056 finding beside MD075", d)
					}
				}
				for _, c := range out.Coverage {
					if c.Rule == "MD075" {
						t.Fatal("MD075 coverage", c)
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
				readinessAssertEqual(t, "MD075 default "+args[0], before, readinessState(t, root))
			}
			for _, args := range [][]string{{"lint", "doc.md"}, {"fix", "--dry-run", "doc.md"}, {"fix", "doc.md"}} {
				got := md014NativeRun(t, binary, root, args...)
				if len(tc.want) > 0 && !strings.Contains(got.Stdout, fmt.Sprintf("doc.md:%d:%d MD075: %s [manual]", tc.want[0].Range.Start.Line, tc.want[0].Range.Start.Column, tc.want[0].Message)) || strings.Contains(got.Stdout, "MD075 rule-unavailable") {
					t.Fatal(args, got)
				}
				if args[0] == "fix" && (strings.Contains(got.Stdout, "(candidate)") || !strings.Contains(got.Stdout, "outcome doc.md: not-needed")) {
					t.Fatal("report-only dry run previewed a change", got)
				}
			}
			//nolint:gosec // Read back only the test-owned fixture.
			if b, e := os.ReadFile(filepath.Join(root, "doc.md")); e != nil || string(b) != tc.source {
				t.Fatal("fix changed the report-only spec example", string(b), e)
			}
			readinessAssertEqual(t, "MD075 default fix", before, readinessState(t, root))
		})
	}
}

func md075AssertCompleteCoverage(t *testing.T, got configurationRun, want int) {
	t.Helper()
	if got.Status != want || !strings.Contains(got.Stdout, "complete: true") || strings.Contains(got.Stdout, "coverage t.md MD075") {
		t.Fatal(got)
	}
}

// An empty task item has no lines to check, so the default configuration
// reports complete coverage for it.
func TestCompiledPlatformMD075EmptyTaskDefaults(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, tc := range []struct {
		source string
		want   int
	}{
		{"- [x]\n", 10},
		{"- [ ]\n", 10},
		{"- [x] \n", 10},
		{"- [x] ", 10},
		{"- [x]\r\n", 10},
		{"> - [x]\n", 10},
		{"- a\n\n  - [x]\n", 10},
		{"# T\n\n- [x]\n", 0},
	} {
		t.Run(fmt.Sprintf("%q", tc.source), func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, "t.md", tc.source)
			md075AssertCompleteCoverage(t, md014NativeRun(t, binary, root, "lint", "t.md"), tc.want)
		})
	}
	for _, tc := range []struct{ source, finding string }{
		{"A | B\n1 | 2\n\n- [x]\n", "t.md:1:1 MD075: Pipe rows are not part of a table. [manual]"},
		{"- [x]\n\n  a | b\n  1 | 2\n", "t.md:3:3 MD075: Pipe rows are not part of a table. [manual]"},
	} {
		t.Run(fmt.Sprintf("%q", tc.source), func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, "t.md", tc.source)
			got := md014NativeRun(t, binary, root, "lint", "t.md")
			md075AssertCompleteCoverage(t, got, 10)
			if !strings.Contains(got.Stdout, tc.finding) {
				t.Fatal(got)
			}
		})
	}
}

// fix turns the trailing-space form into the form the next lint run checks.
func TestCompiledPlatformMD075EmptyTaskFixThenLint(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	root := configurationProject(t)
	putConfigurationFile(t, root, "t.md", "- [x] ")
	md075AssertCompleteCoverage(t, md014NativeRun(t, binary, root, "fix", "t.md"), 10)
	//nolint:gosec // Read back only the test-owned fixture.
	if b, e := os.ReadFile(filepath.Join(root, "t.md")); e != nil || string(b) != "- [x]\n" {
		t.Fatal(string(b), e)
	}
	md075AssertCompleteCoverage(t, md014NativeRun(t, binary, root, "lint", "t.md"), 10)
}

func TestCompiledPlatformMD075SettingsAndInvalidAdmission(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, value := range []string{"omitted", "{}", "{enabled: true}", "{enabled: false}", "{enabled: true, allow_directives: false}", "{style: x}", "{unknown: 1}", "{enabled: \"yes\"}", "{allow_directives: \"false\"}"} {
		t.Run(value, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: gfm", 1)
			if value == "omitted" {
				conf = strings.Replace(conf, "  MD075: {enabled: false}\n", "", 1)
			} else {
				conf = strings.Replace(conf, "MD075: {enabled: false}", "MD075: "+value, 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "A | B\n1 | 2\n")
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
				if d.Rule != "MD075" || d.FixAvailable {
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
			readinessAssertEqual(t, "MD075 default/settings", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD075DirectiveSuppression(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow-%v", allow), func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(cleanConfiguration(), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			conf = strings.Replace(conf, "flavor: commonmark", "flavor: gfm", 1)
			conf = strings.Replace(conf, "MD075: {enabled: false}", fmt.Sprintf("MD075: {enabled: true, allow_directives: %v}", allow), 1)
			conf = strings.Replace(conf, "MD087: {enabled: false}", "MD087: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "<!-- mdchk:disable MD075 -->\nA | B\n1 | 2\n")
			got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "lint")
			visible, want, hidden := 0, 1, 0
			if allow {
				want, hidden = 0, 1
			}
			for _, d := range out.Diagnostics {
				switch d.Rule {
				case "MD075":
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
