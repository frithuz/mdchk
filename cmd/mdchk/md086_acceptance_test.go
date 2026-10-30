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

func md086Configuration(flavor string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD086: {enabled: false}", "MD086: {enabled: true}", 1)
}

func TestCompiledPlatformMD086ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	data, err := os.ReadFile("testdata/md086-cli.json")
	if err != nil {
		t.Fatal(err)
	}
	var plan struct {
		Calls []struct {
			ID, Source, Flavor, Mode string
			Lines                    []int `json:"inspect_lines"`
			Complete                 bool  `json:"expected_complete"`
			Findings                 []struct {
				Bytes, Start, End [2]int
				Message           string
			} `json:"expected_findings"`
			Case struct {
				Source               string
				Enabled, Frontmatter *bool
				Allow                *bool `json:"allow_directives"`
				Directives           *bool `json:"directives_enabled"`
			} `json:"oracle_case"`
		} `json:"literal_calls"`
	}
	if err = json.Unmarshal(data, &plan); err != nil || len(plan.Calls) != 20 {
		t.Fatal(err, len(plan.Calls))
	}
	for _, tc := range plan.Calls {
		t.Run(tc.ID, func(t *testing.T) {
			root := configurationProject(t)
			source := tc.Source
			if tc.Mode != "inspect" {
				source = tc.Case.Source
			}
			conf := md086Configuration(tc.Flavor)
			if tc.ID == "literal-17" {
				conf = strings.Replace(conf, "  MD086: {enabled: true}\n", "", 1)
			}
			if tc.Case.Enabled != nil && !*tc.Case.Enabled {
				conf = strings.Replace(conf, "MD086: {enabled: true}", "MD086: {enabled: false}", 1)
			}
			if tc.Case.Allow != nil && !*tc.Case.Allow {
				conf = strings.Replace(conf, "MD086: {enabled: true}", "MD086: {enabled: true, allow_directives: false}", 1)
			}
			if tc.Case.Directives == nil || *tc.Case.Directives {
				conf = strings.Replace(conf, "directives: {enabled: false}", "directives: {enabled: true}", 1)
			}
			if tc.Case.Frontmatter != nil {
				conf += fmt.Sprintf("frontmatter: {enabled: %v}\n", *tc.Case.Frontmatter)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", source)
			before := readinessState(t, root)
			args := []string{tc.Mode, "doc.md", "--format=json"}
			if len(tc.Lines) != 0 {
				args = append(args, fmt.Sprintf("--lines=%d-%d", tc.Lines[0], tc.Lines[1]))
			}
			got := runConfigurationProcess(t, binary, root, args...)
			after := readinessState(t, root)
			readinessAssertEqual(t, "literal analysis", before, after)
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), tc.Mode)
			status := 0
			if !tc.Complete {
				status = 15
			} else if len(tc.Findings) != 0 {
				status = 10
			}
			if tc.Mode == "inspect" {
				status = 0
			}
			if got.Status != status || out.Complete != tc.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(tc.Findings) || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
				t.Fatal(got, out)
			}
			for i, d := range out.Diagnostics {
				w := tc.Findings[i]
				if d.Rule != "MD086" || d.Message != w.Message || d.FixAvailable || [2]int{d.ByteRange.Start, d.ByteRange.End} != w.Bytes || d.Range.Start != (result.Position{Line: w.Start[0], Column: w.Start[1]}) || d.Range.End != (result.Position{Line: w.End[0], Column: w.End[1]}) || d.Snapshot.View != "original" || d.Snapshot.SizeBytes != len(source) || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(source))) {
					t.Fatal(d, w)
				}
			}
		})
	}
}

func TestCompiledPlatformMD086TypedOptionsAndDefaults(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		setting          string
		status, findings int
	}{{"OMITTED", 10, 1}, {"{}", 10, 1}, {"{enabled: true}", 10, 1}, {"{enabled: false}", 0, 0}, {"{allow_directives: false}", 10, 1}} {
		t.Run(tc.setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := md086Configuration("gfm")
			if tc.setting == "OMITTED" {
				conf = strings.Replace(conf, "  MD086: {enabled: true}\n", "", 1)
			} else {
				conf = strings.Replace(conf, "MD086: {enabled: true}", "MD086: "+tc.setting, 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", "<!-- note")
			before := readinessState(t, root)
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
			after := readinessState(t, root)
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
			if got.Status != tc.status || !out.Complete || len(out.Diagnostics) != tc.findings || len(out.Errors) != 0 {
				t.Fatal(got, out)
			}
			readinessAssertEqual(t, "defaults", before, after)
		})
	}
	for _, setting := range []string{"null", "[]", "true", "{enabled: null}", "{enabled: 'false'}", "{allow_directives: null}", "{enabled: false, allowed_tags: []}", "{enabled: false, options: {}}", "{enabled: false, unknown: 1}"} {
		t.Run(setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md086Configuration("gfm"), "MD086: {enabled: true}", "MD086: "+setting, 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			before := readinessState(t, root)
			got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
			after := readinessState(t, root)
			if got.Status != 12 {
				t.Fatal(got)
			}
			readinessAssertEqual(t, "invalid settings", before, after)
		})
	}
}

func TestCompiledPlatformMD086DirectiveInheritanceAndSelection(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, denial := range []string{"allowed", "global", "rule"} {
		t.Run(denial, func(t *testing.T) {
			root := configurationProject(t)
			parent := strings.Replace(md086Configuration("gfm"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			if denial == "global" {
				parent = strings.Replace(parent, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			if denial == "rule" {
				parent = strings.Replace(parent, "MD086: {enabled: true}", "MD086: {enabled: true, allow_directives: false}", 1)
			}
			child := strings.Replace(md086Configuration("gfm"), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", parent)
			putConfigurationFile(t, root, "nested/.mdchk.yaml", child)
			putConfigurationFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD086 -->\n\n<!-- note\npayload")
			before := readinessState(t, root)
			got, p := runInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=3", "--format=json")
			active, hidden := 1, 0
			if denial == "allowed" {
				active, hidden = 0, 1
			}
			if got.Status != 0 || len(got.Result.Diagnostics) != active || len(got.Result.SuppressedDiagnostics) != hidden || p.File.SelectedLines.Start != 3 {
				t.Fatal(got, p)
			}
			got, _ = runInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=4", "--format=json")
			if got.Status != 0 || len(got.Result.Diagnostics)+len(got.Result.SuppressedDiagnostics) != 0 {
				t.Fatal(got)
			}
			readinessAssertEqual(t, "full-context inspection", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD086FourReporters(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md086Configuration("gfm"))
	putConfigurationFile(t, root, "doc.md", "<!-- note")
	before := readinessState(t, root)
	for _, format := range []string{"text", "json", "vscode", "github"} {
		t.Run(format, func(t *testing.T) {
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format="+format)
			if got.Status != 10 || !strings.Contains(got.Stdout, "MD086") || !strings.Contains(got.Stdout, "HTML comment is missing a closing delimiter.") {
				t.Fatal(got)
			}
			switch format {
			case "text":
				if !strings.Contains(got.Stdout, "doc.md:1:1 MD086:") {
					t.Fatal(got)
				}
			case "vscode":
				if !strings.Contains(got.Stdout, "doc.md:1:1: error: MD086:") {
					t.Fatal(got)
				}
			case "github":
				if strings.Count(got.Stdout, "::error file=doc.md,line=1,title=MD086::") != 1 {
					t.Fatal(got)
				}
			case "json":
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
				if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange.Start != 0 || out.Diagnostics[0].ByteRange.End != 4 {
					t.Fatal(out)
				}
			}
		})
	}
	readinessAssertEqual(t, "reporters", before, readinessState(t, root))
}

func TestCompiledPlatformMD086MD047Composition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, source := range []string{"> <!-- note\n\nend", "<!-- note"} {
		t.Run(source, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md086Configuration("gfm"), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", source)
			before := readinessState(t, root)
			protected := source == "<!-- note"
			for _, dry := range []bool{true, false} {
				args := []string{"fix", "doc.md", "--format=json"}
				if dry {
					args = []string{"fix", "--dry-run", "doc.md", "--format=json"}
				}
				got := runConfigurationProcess(t, binary, root, args...)
				after := readinessState(t, root)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
				if !dry && md038NativeRefusal(t, got, out) {
					readinessAssertEqual(t, "refusal", before, after)
					return
				}
				want, view, disposition := source, "saved", "written"
				changes, deferred, diagnostics, start := 1, 0, 1, 2
				if protected {
					changes, deferred, diagnostics, start = 0, 1, 2, 0
				} else {
					want += "\n"
				}
				if dry {
					view, disposition = "candidate", "proposed"
				}
				if got.Status != 10 || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != diagnostics || len(out.Changes) != changes || len(out.DeferredFixes) != deferred || len(out.Documents) != 0 {
					t.Fatal(got, out)
				}
				manual := 0
				for _, d := range out.Diagnostics {
					if d.Rule == "MD086" {
						manual++
						if d.FixAvailable || d.ByteRange.Start != start || d.ByteRange.End != start+4 || d.Snapshot.View != view || d.Snapshot.SizeBytes != len(want) || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(want))) {
							t.Fatal(d)
						}
					} else if d.Rule != "MD047" || d.FixAvailable {
						t.Fatal(d)
					}
				}
				if manual != 1 {
					t.Fatal(out)
				}
				if protected {
					if out.DeferredFixes[0].Rule != "MD047" {
						t.Fatal(out)
					}
				} else {
					ch := out.Changes[0]
					if len(ch.Rules) != 1 || ch.Rules[0] != "MD047" || ch.Disposition != disposition {
						t.Fatal(ch)
					}
				}
				//nolint:gosec // Isolated fixed test-owned document.
				actual, err := os.ReadFile(filepath.Join(root, "doc.md"))
				saved := source
				if !dry {
					saved = want
				}
				if err != nil || string(actual) != saved {
					t.Fatal(string(actual), err)
				}
				if dry || protected {
					readinessAssertEqual(t, "unchanged", before, after)
				}
			}
		})
	}
}

func TestCompiledPlatformMD086NoCGORefusal(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	conf := strings.Replace(md086Configuration("gfm"), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	putConfigurationFile(t, root, ".mdchk.yaml", conf)
	putConfigurationFile(t, root, "doc.md", "> <!-- note\n\nend")
	before := readinessState(t, root)
	got := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
	after := readinessState(t, root)
	out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
	if !md038NativeRefusal(t, got, out) || !out.Complete || len(out.Errors) != 1 || len(out.Diagnostics)+len(out.CheckedFiles)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
		t.Fatal(got, out)
	}
	readinessAssertEqual(t, "actual cgo0 early refusal", before, after)
}
