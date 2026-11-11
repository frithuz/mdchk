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

type md060NativeLiteral struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Options      struct {
		Enabled bool
		Style   *string
	}
	Findings []struct {
		Bytes, Start, End [2]int
		Message           string
	}
}

func md060NativeLiterals(t *testing.T) []md060NativeLiteral {
	t.Helper()
	b, e := os.ReadFile("../../internal/rules/md060/testdata/md060.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "4dc4a37c8333402b1a9c5fe63b32e0b8caed5217b56909aa29ff433d08bd5837" {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md060NativeLiteral }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) == 0 {
		t.Fatal(e)
	}
	return v.Cases
}

func md060NativeAssert(t *testing.T, out result.Result, tc md060NativeLiteral, view string) {
	t.Helper()
	if !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.DeferredFixes)+len(out.Documents)+len(out.SuppressedDiagnostics) != 0 || len(out.Diagnostics) != len(tc.Findings) {
		t.Fatal(out, tc.Findings)
	}
	for i, d := range out.Diagnostics {
		w := tc.Findings[i]
		if d.Rule != "MD060" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range != (result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}) || d.Snapshot != (result.Snapshot{View: view, SizeBytes: tc.Bytes, SHA256: tc.SHA}) {
			t.Fatal(d, w)
		}
	}
}

func md060NativeSetting(tc md060NativeLiteral) string {
	if !tc.Options.Enabled {
		return "MD060: {enabled: false}"
	}
	if tc.Options.Style == nil {
		return "MD060: {enabled: true}"
	}
	return "MD060: {enabled: true, style: " + *tc.Options.Style + "}"
}

func TestCompiledPlatformMD060AnalysisFixtures(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, tc := range md060NativeLiterals(t) {
		if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
			t.Fatal("fixture source identity")
		}
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				conf = strings.Replace(conf, "MD060: {enabled: false}", md060NativeSetting(tc), 1)
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
					md060NativeAssert(t, out, tc, view)
					readinessAssertEqual(t, "MD060 analysis/report-only preview", before, readinessState(t, root))
				}
			})
		}
	}
}

func md060NativeRange(b0, b1, line, c0, c1 int) result.Diagnostic {
	return result.Diagnostic{Message: "Cell padding does not match the aligned style; column width is 3.", ByteRange: result.ByteRange{Start: b0, End: b1}, Range: result.Range{Start: result.Position{Line: line, Column: c0}, End: result.Position{Line: line, Column: c1}}}
}

func TestCompiledPlatformMD060SpecExamples(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	const violating = "| A| B |\n|---|---|\n|1|2|\n"
	compact := md060NativeRange(1, 5, 1, 2, 6)
	compact.Message = "Cell padding does not match the compact style."
	for _, tc := range []struct {
		flavor, setting, source string
		want                    []result.Diagnostic
	}{
		{"gfm", "{enabled: true}", violating, []result.Diagnostic{md060NativeRange(1, 3, 1, 2, 4), md060NativeRange(4, 7, 1, 5, 8), md060NativeRange(10, 13, 2, 2, 5), md060NativeRange(14, 17, 2, 6, 9), md060NativeRange(20, 21, 3, 2, 3), md060NativeRange(22, 23, 3, 4, 5)}},
		{"gfm", "{enabled: true}", "| A   | B   |\n| --- | --- |\n| 1   | 2   |\n", nil},
		{"gfm", "{enabled: true, style: compact}", "| A | B |\n| --- | --- |\n| 1 | 2 |\n", nil},
		{"gfm", "{enabled: true, style: compact}", "| A  | B |\n| --- | --- |\n| 1 | 2 |\n", []result.Diagnostic{compact}},
		{"gfm", "{enabled: true, style: tight}", "|A|B|\n|---|---|\n|1|2|\n", nil},
		{"gfm", "{}", violating, nil},
		{"gfm", "omitted", violating, nil},
		{"commonmark", "{enabled: true}", violating, nil},
	} {
		t.Run(fmt.Sprintf("%s/%s/%q", tc.flavor, tc.setting, tc.source), func(t *testing.T) {
			root := configurationProject(t)
			conf := "version: 1\nflavor: " + tc.flavor + "\n"
			if tc.setting != "omitted" {
				conf += "rules:\n  MD060: " + tc.setting + "\n"
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := readinessState(t, root)
			// Other default rules remain unavailable, so the status reports incompleteness.
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
				got := md014NativeRun(t, binary, root, args...)
				out := md014NativeJSON(t, []byte(got.Stdout), args[0])
				var found []result.Diagnostic
				for _, d := range out.Diagnostics {
					if d.Rule == "MD060" {
						found = append(found, d)
					}
				}
				for _, c := range out.Coverage {
					if c.Rule == "MD060" {
						t.Fatal("MD060 coverage", c)
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
				readinessAssertEqual(t, "MD060 spec example "+args[0], before, readinessState(t, root))
			}
			fixed := md014NativeRun(t, binary, root, "fix", "doc.md")
			if len(tc.want) > 0 && !strings.Contains(fixed.Stdout, "doc.md:1:2 MD060: "+tc.want[0].Message+" [manual]") {
				t.Fatal(fixed)
			}
			//nolint:gosec // Read back only the test-owned fixture.
			if b, e := os.ReadFile(filepath.Join(root, "doc.md")); e != nil || string(b) != tc.source {
				t.Fatal("fix changed the report-only spec example", string(b), e)
			}
		})
	}
}

func TestCompiledPlatformMD060SettingsAndInvalidAdmission(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, value := range []string{"omitted", "{}", "{enabled: true}", "{enabled: false}", "{enabled: true, style: aligned}", "{enabled: true, style: compact}", "{enabled: true, style: tight}", "{enabled: true, style: tight, allow_directives: false}", "{style: compact}", "{style: wide}", "{style: 1}", "{styles: tight}", "{enabled: \"yes\"}", "{allow_directives: \"false\"}"} {
		t.Run(value, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: gfm", 1)
			if value == "omitted" {
				conf = strings.Replace(conf, "  MD060: {enabled: false}\n", "", 1)
			} else {
				conf = strings.Replace(conf, "MD060: {enabled: false}", "MD060: "+value, 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "| A| B |\n|---|---|\n|1|2|\n")
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "lint")
			want, count := 10, 6
			switch value {
			case "omitted", "{}", "{enabled: false}", "{style: compact}":
				want, count = 0, 0
			case "{enabled: true, style: compact}":
				count = 5
			case "{enabled: true, style: tight}", "{enabled: true, style: tight, allow_directives: false}":
				count = 2
			case "{style: wide}", "{style: 1}", "{styles: tight}", "{enabled: \"yes\"}", "{allow_directives: \"false\"}":
				want, count = 12, 0
			}
			if got.Status != want || len(out.Diagnostics) != count || len(out.Coverage)+len(out.Changes)+len(out.Documents) != 0 {
				t.Fatal(got, out)
			}
			for _, d := range out.Diagnostics {
				if d.Rule != "MD060" || d.FixAvailable {
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
			readinessAssertEqual(t, "MD060 default/settings", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD060DirectiveSuppression(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow-%v", allow), func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(cleanConfiguration(), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			conf = strings.Replace(conf, "flavor: commonmark", "flavor: gfm", 1)
			conf = strings.Replace(conf, "MD060: {enabled: false}", fmt.Sprintf("MD060: {enabled: true, allow_directives: %v}", allow), 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "<!-- mdchk:disable MD060 -->\n| A| B |\n|---|---|\n|1|2|\n")
			got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "lint")
			visible, want, hidden := 0, 6, 0
			if allow {
				want, hidden = 0, 6
			}
			for _, d := range out.Diagnostics {
				if d.Rule == "MD060" {
					visible++
				}
			}
			if visible != want || len(out.SuppressedDiagnostics) != hidden || len(out.Coverage) != 0 {
				t.Fatal(got, out)
			}
		})
	}
}
