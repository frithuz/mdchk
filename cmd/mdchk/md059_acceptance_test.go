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

type md059NativeLiteral struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Frontmatter  bool
	Options      struct {
		Enabled bool
		Phrases *[]string
	}
	Findings []struct {
		Bytes, Start, End [2]int
		Message           string
	}
}

func md059NativeLiterals(t *testing.T) []md059NativeLiteral {
	t.Helper()
	b, e := os.ReadFile("../../internal/rules/testdata/md059.json")
	if e != nil {
		t.Fatal(e)
	}
	var v struct{ Cases []md059NativeLiteral }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) == 0 {
		t.Fatal(e)
	}
	return v.Cases
}

func md059NativeAssert(t *testing.T, out result.Result, tc md059NativeLiteral, view string) {
	t.Helper()
	if !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.DeferredFixes)+len(out.Documents)+len(out.SuppressedDiagnostics) != 0 || len(out.Diagnostics) != len(tc.Findings) {
		t.Fatal(out, tc.Findings)
	}
	for i, d := range out.Diagnostics {
		w := tc.Findings[i]
		if d.Rule != "MD059" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range != (result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}) || d.Snapshot != (result.Snapshot{View: view, SizeBytes: tc.Bytes, SHA256: tc.SHA}) {
			t.Fatal(d, w)
		}
	}
}

func md059NativeSetting(t *testing.T, tc md059NativeLiteral) string {
	t.Helper()
	if !tc.Options.Enabled {
		return "MD059: {enabled: false}"
	}
	if tc.Options.Phrases == nil {
		return "MD059: {enabled: true}"
	}
	// JSON flow sequences are YAML and keep edge whitespace and empty phrases.
	phrases, e := json.Marshal(*tc.Options.Phrases)
	if e != nil {
		t.Fatal(e)
	}
	return "MD059: {enabled: true, phrases: " + string(phrases) + "}"
}

func TestCompiledPlatformMD059AnalysisFixtures(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, tc := range md059NativeLiterals(t) {
		if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
			t.Fatal("fixture source identity")
		}
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				conf = strings.Replace(conf, "MD059: {enabled: false}", md059NativeSetting(t, tc), 1)
				if tc.Frontmatter {
					conf += "frontmatter: {enabled: true}\n"
				}
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
					md059NativeAssert(t, out, tc, view)
					readinessAssertEqual(t, "MD059 analysis/report-only preview", before, readinessState(t, root))
				}
			})
		}
	}
}

func TestCompiledPlatformMD059SpecExampleWithDefaults(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	root := configurationProject(t)
	putConfigurationFile(t, root, "a.md", "[Click here](guide.md)\n")
	before := readinessState(t, root)
	// Other default rules remain unavailable, so the status reports incompleteness.
	got := md014NativeRun(t, binary, root, "lint", "a.md")
	if got.Status == 0 || !strings.Contains(got.Stdout, "a.md:1:2 MD059: Link label is uninformative. [manual]\n") || strings.Contains(got.Stdout, "MD059 rule-unavailable") {
		t.Fatal(got)
	}
	readinessAssertEqual(t, "MD059 default lint", before, readinessState(t, root))
	fixed := md014NativeRun(t, binary, root, "fix", "a.md")
	if !strings.Contains(fixed.Stdout, "a.md:1:2 MD059: Link label is uninformative. [manual]") {
		t.Fatal(fixed)
	}
	//nolint:gosec // Read back only the test-owned fixture.
	if b, e := os.ReadFile(filepath.Join(root, "a.md")); e != nil || string(b) != "[Click here](guide.md)\n" {
		t.Fatal("fix changed the report-only spec example", string(b), e)
	}
}

func TestCompiledPlatformMD059SettingsAndInvalidAdmission(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, value := range []string{"omitted", "{}", "{enabled: true}", "{enabled: false}", "{phrases: [link]}", "{phrases: []}", "{phrases: [\"  Click   HERE \"]}", "{phrases: [here], allow_directives: false}", "{phrase: [here]}", "{phrases: here}", "{phrases: [1]}", "{enabled: \"yes\"}"} {
			t.Run(flavor+"/"+value, func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				if value == "omitted" {
					conf = strings.Replace(conf, "  MD059: {enabled: false}\n", "", 1)
				} else {
					conf = strings.Replace(conf, "MD059: {enabled: false}", "MD059: "+value, 1)
				}
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				putConfigurationFile(t, root, "doc.md", "[Click here](guide.md)\n")
				before := readinessState(t, root)
				got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
				out := md014NativeJSON(t, []byte(got.Stdout), "lint")
				want, count := 10, 1
				if value == "{enabled: false}" || value == "{phrases: [link]}" || value == "{phrases: []}" || value == "{phrases: [here], allow_directives: false}" {
					want, count = 0, 0
				}
				invalid := strings.Contains(value, "phrase:") || strings.Contains(value, "phrases: here") || strings.Contains(value, "[1]") || strings.Contains(value, "yes")
				if invalid {
					want, count = 12, 0
				}
				if got.Status != want || len(out.Diagnostics) != count || len(out.Coverage)+len(out.Changes)+len(out.Documents) != 0 {
					t.Fatal(got, out)
				}
				if count == 1 && (out.Diagnostics[0].Rule != "MD059" || out.Diagnostics[0].Message != "Link label is uninformative." || out.Diagnostics[0].FixAvailable || out.Diagnostics[0].Range != (result.Range{Start: result.Position{Line: 1, Column: 2}, End: result.Position{Line: 1, Column: 12}})) {
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
				readinessAssertEqual(t, "MD059 default/settings", before, readinessState(t, root))
			})
		}
	}
}

func TestCompiledPlatformMD059DirectiveSuppression(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow-%v", allow), func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(cleanConfiguration(), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			conf = strings.Replace(conf, "MD059: {enabled: false}", fmt.Sprintf("MD059: {enabled: true, allow_directives: %v}", allow), 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "<!-- mdchk:disable MD059 -->\n[Click here](guide.md)\n")
			got := md014NativeRun(t, binary, root, "lint", "doc.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "lint")
			visible, want := 0, 1
			if allow {
				want = 0
			}
			for _, d := range out.Diagnostics {
				if d.Rule == "MD059" {
					visible++
				}
			}
			if visible != want || len(out.Coverage) != 0 {
				t.Fatal(got, out)
			}
		})
	}
}
