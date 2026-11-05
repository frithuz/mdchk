package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

type md054NativeLiteral struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Options      struct {
		Enabled       bool
		AllowedStyles []string `json:"allowed_styles"`
	}
	Findings []struct {
		Bytes, Start, End [2]int
		Expected, Message string
	}
}

func md054NativeLiterals(t *testing.T) []md054NativeLiteral {
	t.Helper()
	b, e := os.ReadFile("../../internal/rules/testdata/md054.json")
	if e != nil {
		t.Fatal(e)
	}
	var v struct{ Cases []md054NativeLiteral }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) == 0 {
		t.Fatal(e)
	}
	return v.Cases
}

func md054NativeAssert(t *testing.T, out result.Result, tc md054NativeLiteral, view string) {
	t.Helper()
	if !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.DeferredFixes)+len(out.Documents)+len(out.SuppressedDiagnostics) != 0 || len(out.Diagnostics) != len(tc.Findings) {
		t.Fatal(out, tc.Findings)
	}
	for i, d := range out.Diagnostics {
		w := tc.Findings[i]
		if d.Rule != "MD054" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range != (result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}) || d.Snapshot != (result.Snapshot{View: view, SizeBytes: tc.Bytes, SHA256: tc.SHA}) {
			t.Fatal(d, w)
		}
	}
}

func md054NativeSetting(tc md054NativeLiteral) string {
	if !tc.Options.Enabled {
		return "MD054: {enabled: false}"
	}
	if tc.Options.AllowedStyles == nil {
		return "MD054: {enabled: true}"
	}
	return "MD054: {enabled: true, allowed_styles: [" + strings.Join(tc.Options.AllowedStyles, ", ") + "]}"
}

func TestCompiledPlatformMD054AnalysisFixtures(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range md054NativeLiterals(t) {
		if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
			t.Fatal("fixture source identity")
		}
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				conf = strings.Replace(conf, "MD054: {enabled: false}", md054NativeSetting(tc), 1)
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
					md054NativeAssert(t, out, tc, view)
					readinessAssertEqual(t, "MD054 analysis/report-only preview", before, readinessState(t, root))
				}
			})
		}
	}
}

func TestCompiledPlatformMD054SpecExampleAndInvalidAdmission(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, value := range []string{"omitted", "{}", "{enabled: true}", "{enabled: true, allowed_styles: [full_reference]}", "{enabled: true, allowed_styles: [inline]}", "{enabled: false, allowed_styles: [full_reference]}", "{allowed_styles: []}", "{enabled: true, allowed_styles: []}", "{allowed_styles: [reference]}", "{allowed_styles: inline}", "{style: inline}", "{enabled: \"yes\"}"} {
			t.Run(flavor+"/"+value, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				if value == "omitted" {
					conf = strings.Replace(conf, "  MD054: {enabled: false}\n", "", 1)
				} else {
					conf = strings.Replace(conf, "MD054: {enabled: false}", "MD054: "+value, 1)
				}
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				putConfigurationFile(t, root, "doc.md", "[Guide](guide.md)\n")
				before := readinessState(t, root)
				got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
				out := md014NativeJSON(t, []byte(got.Stdout), "lint")
				want, count := 0, 0
				if value == "{enabled: true, allowed_styles: [full_reference]}" {
					want, count = 10, 1
				}
				invalid := strings.Contains(value, "[]") || strings.Contains(value, "reference]") && !strings.Contains(value, "full_reference") || strings.Contains(value, "styles: inline") || strings.Contains(value, "style:") || strings.Contains(value, "yes")
				if invalid {
					want, count = 12, 0
				}
				if got.Status != want || len(out.Diagnostics) != count || len(out.Changes)+len(out.Documents) != 0 {
					t.Fatal(got, out)
				}
				if count == 1 && (out.Diagnostics[0].Rule != "MD054" || out.Diagnostics[0].Message != `Link representation style "inline" is not allowed.` || out.Diagnostics[0].Range != (result.Range{Start: result.Position{Line: 1, Column: 1}, End: result.Position{Line: 1, Column: 18}})) {
					t.Fatal(out.Diagnostics)
				}
				if invalid {
					if len(out.Files) != 0 {
						t.Fatal("configuration failure acquired targets", out)
					}
					validated := md014NativeRun(t, binary, root, "config", "validate", ".", "--format=json")
					if validated.Status != 12 {
						t.Fatal(validated)
					}
				}
				readinessAssertEqual(t, "MD054 default/settings", before, readinessState(t, root))
			})
		}
	}
}
