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

type md055NativeLiteral struct {
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

func md055NativeLiterals(t *testing.T) []md055NativeLiteral {
	t.Helper()
	b, e := os.ReadFile("../../internal/rules/testdata/md055.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "5439144ca04395f31749913a620651b8c6c82ca67d33b4f729533df89b1dbefa" {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md055NativeLiteral }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) == 0 {
		t.Fatal(e)
	}
	return v.Cases
}

func md055NativeAssert(t *testing.T, out result.Result, tc md055NativeLiteral, view string) {
	t.Helper()
	if !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.DeferredFixes)+len(out.Documents)+len(out.SuppressedDiagnostics) != 0 || len(out.Diagnostics) != len(tc.Findings) {
		t.Fatal(out, tc.Findings)
	}
	for i, d := range out.Diagnostics {
		w := tc.Findings[i]
		if d.Rule != "MD055" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range != (result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}) || d.Snapshot != (result.Snapshot{View: view, SizeBytes: tc.Bytes, SHA256: tc.SHA}) {
			t.Fatal(d, w)
		}
	}
}

func md055NativeSetting(tc md055NativeLiteral) string {
	if !tc.Options.Enabled {
		return "MD055: {enabled: false}"
	}
	if tc.Options.Style == nil {
		return "MD055: {enabled: true}"
	}
	return "MD055: {enabled: true, style: " + *tc.Options.Style + "}"
}

func TestCompiledPlatformMD055AnalysisFixtures(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range md055NativeLiterals(t) {
		if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
			t.Fatal("fixture source identity")
		}
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				conf = strings.Replace(conf, "MD055: {enabled: false}", md055NativeSetting(tc), 1)
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
					md055NativeAssert(t, out, tc, view)
					readinessAssertEqual(t, "MD055 analysis/report-only preview", before, readinessState(t, root))
				}
			})
		}
	}
}

func TestCompiledPlatformMD055SpecExamplesWithDefaults(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		flavor, source string
		want           []result.Diagnostic
	}{
		{"gfm", "A | B\n--- | ---\n| 1 | 2 |\n", []result.Diagnostic{
			{Message: "Unexpected leading pipe; expected no outer pipes.", ByteRange: result.ByteRange{Start: 16, End: 17}, Range: result.Range{Start: result.Position{Line: 3, Column: 1}, End: result.Position{Line: 3, Column: 2}}},
			{Message: "Unexpected trailing pipe; expected no outer pipes.", ByteRange: result.ByteRange{Start: 24, End: 25}, Range: result.Range{Start: result.Position{Line: 3, Column: 9}, End: result.Position{Line: 3, Column: 10}}},
		}},
		{"gfm", "A | B\n--- | ---\n1 | 2\n", nil},
		{"commonmark", "A | B\n--- | ---\n| 1 | 2 |\n", nil},
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
					if d.Rule == "MD055" {
						found = append(found, d)
					}
				}
				for _, c := range out.Coverage {
					if c.Rule == "MD055" {
						t.Fatal("MD055 coverage", c)
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
				readinessAssertEqual(t, "MD055 default "+args[0], before, readinessState(t, root))
			}
			fixed := md014NativeRun(t, binary, root, "fix", "doc.md")
			if len(tc.want) > 0 && !strings.Contains(fixed.Stdout, "doc.md:3:1 MD055: Unexpected leading pipe; expected no outer pipes. [manual]") {
				t.Fatal(fixed)
			}
			//nolint:gosec // Read back only the test-owned fixture.
			if b, e := os.ReadFile(filepath.Join(root, "doc.md")); e != nil || string(b) != tc.source {
				t.Fatal("fix changed the report-only spec example", string(b), e)
			}
		})
	}
}

func TestCompiledPlatformMD055SettingsAndInvalidAdmission(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, value := range []string{"omitted", "{}", "{enabled: true}", "{enabled: false}", "{style: consistent}", "{style: none}", "{style: leading_and_trailing}", "{style: none, allow_directives: false}", "{style: both}", "{style: 1}", "{styles: none}", "{enabled: \"yes\"}", "{allow_directives: \"false\"}"} {
		t.Run(value, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: gfm", 1)
			if value == "omitted" {
				conf = strings.Replace(conf, "  MD055: {enabled: false}\n", "", 1)
			} else {
				conf = strings.Replace(conf, "MD055: {enabled: false}", "MD055: "+value, 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "A | B\n--- | ---\n| 1 | 2 |\n")
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "lint")
			want, count := 10, 2
			switch value {
			case "{enabled: false}":
				want, count = 0, 0
			case "{style: leading_and_trailing}":
				count = 4
			case "{style: both}", "{style: 1}", "{styles: none}", "{enabled: \"yes\"}", "{allow_directives: \"false\"}":
				want, count = 12, 0
			}
			if got.Status != want || len(out.Diagnostics) != count || len(out.Coverage)+len(out.Changes)+len(out.Documents) != 0 {
				t.Fatal(got, out)
			}
			for _, d := range out.Diagnostics {
				if d.Rule != "MD055" || d.FixAvailable {
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
			readinessAssertEqual(t, "MD055 default/settings", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD055DirectiveSuppression(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow-%v", allow), func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(cleanConfiguration(), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			conf = strings.Replace(conf, "flavor: commonmark", "flavor: gfm", 1)
			conf = strings.Replace(conf, "MD055: {enabled: false}", fmt.Sprintf("MD055: {enabled: true, allow_directives: %v}", allow), 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "<!-- mdchk:disable MD055 -->\nA | B\n--- | ---\n| 1 | 2 |\n")
			got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "lint")
			visible, want, hidden := 0, 2, 0
			if allow {
				want, hidden = 0, 2
			}
			for _, d := range out.Diagnostics {
				if d.Rule == "MD055" {
					visible++
				}
			}
			if visible != want || len(out.SuppressedDiagnostics) != hidden || len(out.Coverage) != 0 {
				t.Fatal(got, out)
			}
		})
	}
}
