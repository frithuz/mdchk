package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

// The overlay changes only provider construction. The actual main, dispatcher,
// config discovery, parser, result finalization and reporters remain production
// code. Only two synthetic callbacks are registered against the full catalog.
func TestConfigurationFixtureAcceptance(t *testing.T) {
	binary := buildConfigurationFixtureExecutable(t)
	for _, tc := range []struct {
		name       string
		missing    bool
		finding    bool
		failure    bool
		targetIO   bool
		status     int
		wantStatus string
	}{
		{"clean-disabled-callbacks", false, false, false, false, 0, "clean"},
		{"complete-findings", false, true, false, false, 10, "violations"},
		{"missing-rule-retains-findings", true, true, false, false, 15, "incomplete"},
		{"io-over-incomplete-and-findings", true, true, false, true, 13, "error"},
		{"internal-over-io-incomplete-and-findings", true, true, true, true, 14, "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", fixtureConfiguration(tc.missing, tc.finding, tc.failure))
			const original = "# 雪\r\nπ fixture violation\r\n"
			putConfigurationFile(t, root, "a.md", original)
			putConfigurationFile(t, root, "b.md", original)
			putConfigurationFile(t, root, "z-failure.md", "fixture failure\n")
			targets := []string{"b.md", "a.md", "z-failure.md"}
			if tc.targetIO {
				targets = append(targets, "missing.md")
			}
			for _, format := range []string{"json", "text"} {
				t.Run(format, func(t *testing.T) {
					args := append([]string{"lint", "--format=" + format}, targets...)
					got := runConfiguration(t, binary, root, root, args...)
					if got.Status != tc.status {
						t.Fatalf("status %d, want %d: %s", got.Status, tc.status, got.Stdout)
					}
					reversed := append([]string{}, targets...)
					for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
						reversed[i], reversed[j] = reversed[j], reversed[i]
					}
					again := runConfiguration(t, binary, root, root, append([]string{"lint", "--format=" + format}, reversed...)...)
					if again.Status != got.Status || again.Stdout != got.Stdout {
						t.Fatalf("target argument order changed %s result\nfirst %s\nsecond %s", format, got.Stdout, again.Stdout)
					}
					if format == "text" {
						if !strings.Contains(got.Stdout, tc.wantStatus+" (complete:") || strings.Contains(got.Stdout, root) {
							t.Fatalf("text summary/path contract: %s", got.Stdout)
						}
						if tc.finding && !strings.Contains(got.Stdout, "a.md:2:3 MD001: fixture violation") {
							t.Fatalf("text lost original finding: %s", got.Stdout)
						}
						assertFixtureText(t, got.Stdout, tc.missing, tc.targetIO, tc.failure)
						return
					}
					r := got.Result
					if r.Status != tc.wantStatus || r.Complete != (tc.status == 0 || tc.status == 10) || len(r.CheckedFiles) != 3 {
						t.Fatalf("outcome: %+v", r)
					}
					if tc.finding {
						assertFixtureFindings(t, r, original)
					} else if len(r.Diagnostics) != 0 {
						t.Fatal("disabled fixture callback produced findings")
					}
					missingRules, selectionFailures, callbackFailures := 0, 0, 0
					for _, c := range r.Coverage {
						switch {
						case c.Rule == "MD004" && c.Reason == "rule-unavailable":
							missingRules++
						case c.Path == "missing.md" && c.Rule == "" && c.Reason == "selection-failed":
							selectionFailures++
						case c.Path == "z-failure.md" && c.Rule == "MD003" && c.Reason == "context-unavailable":
							callbackFailures++
						default:
							t.Fatalf("unexpected missing coverage: %+v", c)
						}
					}
					wantMissing, wantSelection, wantCallback := 0, 0, 0
					if tc.missing {
						wantMissing = 3
					}
					if tc.targetIO {
						wantSelection = 1
					}
					if tc.failure {
						wantCallback = 1
					}
					if missingRules != wantMissing || selectionFailures != wantSelection || callbackFailures != wantCallback {
						t.Fatalf("lost coverage: %+v", r.Coverage)
					}
					wantCategories := []result.ErrorCategory{}
					if tc.failure {
						wantCategories = append(wantCategories, result.Internal)
					}
					if tc.targetIO {
						wantCategories = append(wantCategories, result.IO)
					}
					gotCategories := map[result.ErrorCategory]int{}
					for _, e := range r.Errors {
						gotCategories[e.Category]++
					}
					if len(r.Errors) != len(wantCategories) {
						t.Fatalf("errors: %+v", r.Errors)
					}
					for _, category := range wantCategories {
						if gotCategories[category] != 1 {
							t.Fatalf("missing retained category %s: %+v", category, r.Errors)
						}
					}
				})
			}
		})
	}

	t.Run("preflight-before-fixture-callbacks-and-target-io", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, "doc.md", "fixture violation\nfixture failure\n")
		putConfigurationFile(t, root, ".mdchk.yaml", configurationSyntaxContext+"version: '1'\n")
		for _, tc := range []struct {
			args []string
			code int
			kind result.ErrorCategory
		}{
			{[]string{"lint", "--format=json", "doc.md", "missing.md"}, 12, result.Configuration},
			{[]string{"lint", "--format=json", "--unsupported", "missing.md"}, 11, result.Usage},
		} {
			got := runConfiguration(t, binary, root, root, tc.args...)
			if got.Status != tc.code || len(got.Result.Errors) != 1 || got.Result.Errors[0].Category != tc.kind || len(got.Result.Diagnostics) != 0 || len(got.Result.CheckedFiles) != 0 || len(got.Result.Files) != 0 || len(got.Result.Coverage) != 0 {
				t.Fatalf("preflight started target work: status=%d result=%+v", got.Status, got.Result)
			}

			if tc.kind == result.Configuration {
				assertConfigurationNegative(t, got, configurationNegative{field: "version", message: "expected integer, got string", line: 3, column: 10})
			} else if !strings.Contains(got.Result.Errors[0].Message, "--unsupported") {
				t.Fatalf("unrelated usage error masked unsupported flag: %+v", got.Result.Errors)
			}
		}
	})
}

// buildConfigurationFixtureExecutable isolates test registration at compile time.
// Reuse it for compiled CLI checks without adding a production runtime switch.
func buildConfigurationFixtureExecutable(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	overlayBytes, err := json.Marshal(struct{ Replace map[string]string }{map[string]string{
		filepath.Join(cwd, "..", "..", "internal", "cli", "providers.go"): filepath.Join(cwd, "testdata", "configuration_providers.go"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(t.TempDir(), "overlay.json")
	if err := os.WriteFile(overlay, overlayBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return buildConfigurationExecutable(t, overlay)
}

func fixtureConfiguration(missing, finding, failure bool) string {
	var b strings.Builder
	b.WriteString("version: 1\nflavor: commonmark\ndirectives: {enabled: false}\nrules:\n")
	for _, m := range engine.Catalog() {
		// Leave MD001 unmentioned when exercising findings: its enabled
		// catalog default must execute. MD013 stays unmentioned in every
		// case: its disabled default must never create missing coverage.
		if m.ID == "MD094" || m.ID == "MD013" || (finding && m.ID == "MD001") || (missing && m.ID == "MD004") {
			continue
		}
		enabled := m.ID == "MD003" && failure
		fmt.Fprintf(&b, "  %s: {enabled: %t}\n", m.ID, enabled)
	}
	return b.String()
}

func assertFixtureText(t *testing.T, text string, missing, targetIO, failure bool) {
	t.Helper()
	for _, tc := range []struct {
		want bool
		text string
	}{{missing, "MD004 rule-unavailable"}, {targetIO, "(io)"}, {failure, "(internal)"}} {
		if strings.Contains(text, tc.text) != tc.want {
			t.Fatalf("text presence %q = %t: %s", tc.text, tc.want, text)
		}
	}
}

func assertFixtureFindings(t *testing.T, r result.Result, original string) {
	t.Helper()
	if len(r.Diagnostics) != 2 || !reflect.DeepEqual([]string{r.Diagnostics[0].Path, r.Diagnostics[1].Path}, []string{"a.md", "b.md"}) {
		t.Fatalf("available findings lost or unordered: %+v", r.Diagnostics)
	}
	for _, d := range r.Diagnostics {
		start := strings.Index(original, "fixture violation")
		if d.Rule != "MD001" || d.Message != "fixture violation" || d.Range.Start.Line != 2 || d.Range.Start.Column != 3 || d.Range.End.Line != 2 || d.Range.End.Column != 20 || d.ByteRange.Start != start || d.ByteRange.End != start+len("fixture violation") || d.Snapshot.View != "original" || d.Snapshot.SizeBytes != len(original) || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(original))) || d.FixAvailable {
			t.Fatalf("finding lost immutable original-byte identity: %+v", d)
		}
	}
}
