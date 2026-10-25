package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func md047Configuration(enabled, permission bool) string {
	config := strings.ReplaceAll(cleanConfiguration(), "directives: {enabled: false}", "directives: {enabled: true}")
	value := "false"
	if enabled {
		value = "true"
	}
	allowed := "false"
	if permission {
		allowed = "true"
	}
	return strings.Replace(config, "MD047: {enabled: false}", "MD047: {enabled: "+value+", allow_directives: "+allowed+"}", 1)
}
func TestCompiledMD047ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	config := md047Configuration(true, true)
	putConfigurationFile(t, root, ".mdchk.yaml", config)
	for _, tc := range []struct {
		name, source                     string
		start, end, line, column, status int
	}{
		{"empty", "", 0, 0, 0, 0, 0}, {"one-crlf", "π\r\n", 0, 0, 0, 0, 0},
		{"missing", "\tπé", 6, 6, 1, 5, 10}, {"surplus", "π\r\n\r\n", 4, 6, 2, 1, 10},
		{"protected", "```\ncode", 8, 8, 2, 5, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := configurationBytes(t, root)
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
			if got.Status != tc.status || !out.Complete || len(out.Errors) != 0 || len(out.Coverage) != 0 {
				t.Fatal(got)
			}
			want := 0
			if tc.status == 10 {
				want = 1
			}
			if len(out.Diagnostics) != want {
				t.Fatal(out)
			}
			if want > 0 {
				d := out.Diagnostics[0]
				if d.Rule != "MD047" || d.ByteRange != (result.ByteRange{Start: tc.start, End: tc.end}) || d.Range.Start != (result.Position{Line: tc.line, Column: tc.column}) || d.FixAvailable != (tc.name == "surplus") {
					t.Fatal(d)
				}
			}
			seen, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
			if seen.Status != 0 || seen.Result.Status != "inspected" || len(seen.Result.Diagnostics) != want || p.File.SizeBytes != int64(len(tc.source)) {
				t.Fatal(seen)
			}
			found := false
			for _, rule := range *p.Rules.Data {
				if rule.Rule == "MD047" {
					found = true
					if rule.Implementation != "available" || rule.ActiveEvaluation != "complete" || rule.SourceCoverage.Active.Availability != "complete" {
						t.Fatal(rule)
					}
				}
				if (rule.Rule == "MD001" || rule.Rule == "MD003" || rule.Rule == "MD018" || rule.Rule == "MD019") && rule.Implementation != "available" {
					t.Fatal(rule)
				}
			}
			if !found {
				t.Fatal("MD047 missing")
			}
			if len(out.Changes) != 0 || len(out.Documents) != 0 || out.Files[0].WriteStatus != "not-requested" {
				t.Fatal(out)
			}
			if after := configurationBytes(t, root); len(before) != len(after) {
				t.Fatal("lint changed inventory")
			} else {
				for path, sum := range before {
					if after[path] != sum {
						t.Fatal("lint changed bytes", path)
					}
				}
			}
		})
	}
	// Production configuration admission now accepts MD047, while unknown options
	// and malformed shared values are rejected even on disabled rules.
	for _, setting := range []string{"{}", "{enabled: true}", "{enabled: false}", "{enabled: false, unknown: true}", "{enabled: false, allow_directives: 1}", "{enabled: null}"} {
		text := strings.Replace(config, "MD047: {enabled: true, allow_directives: true}", "MD047: "+setting, 1)
		putConfigurationFile(t, root, ".mdchk.yaml", text)
		got := runConfigurationProcessEnv(t, binary, root, validationEnvironment(t), "config", "validate", "--format=json")
		out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "config validate")
		invalid := strings.Contains(setting, "unknown") || strings.Contains(setting, ": 1") || strings.Contains(setting, "null")
		if invalid {
			if got.Status != 12 || len(out.Errors) == 0 {
				t.Fatal(got)
			}
		} else if got.Status != 0 || len(out.Errors) != 0 {
			t.Fatal(got)
		}
	}
	// Local and inherited permissions are authority; repeated transitions retain
	// their exact last owner for both EOF insertions and excess-suffix findings.
	for _, tc := range []struct {
		name, source                 string
		permission, global, ancestor bool
		hidden                       int
	}{
		{"missing", "<!-- mdchk:disable MD047 --><!-- mdchk:disable MD047 -->tail", true, true, false, 1},
		{"surplus", "<!-- mdchk:disable MD047 --><!-- mdchk:disable MD047 -->\r\n\r\n", true, true, false, 1},
		{"per-rule blocked", "<!-- mdchk:disable MD047 -->tail", false, true, false, 0},
		{"global blocked", "<!-- mdchk:disable MD047 -->tail", true, false, false, 0},
		{"ancestor blocked", "<!-- mdchk:disable MD047 -->tail", true, true, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := "nested"
			config := md047Configuration(true, tc.permission)
			if !tc.global {
				config = strings.Replace(config, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
			if tc.ancestor {
				putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, false))
			}
			putConfigurationFile(t, root, child+"/.mdchk.yaml", config)
			putConfigurationFile(t, root, child+"/doc.md", tc.source)
			got, p := runInspection(t, binary, filepath.Join(root, child), root, "inspect", "doc.md", "--format=json")
			if got.Status != 0 || len(got.Result.SuppressedDiagnostics) != tc.hidden || len(got.Result.Diagnostics) != 1-tc.hidden {
				t.Fatal(got)
			}
			if tc.hidden > 0 {
				var suppressed result.SuppressedDiagnostic
				if err := json.Unmarshal(got.Result.SuppressedDiagnostics[0], &suppressed); err != nil {
					t.Fatal(err)
				}
				want := len(tc.source)
				if strings.HasSuffix(tc.source, "\r\n\r\n") {
					want -= 2
				}
				if suppressed.Diagnostic.ByteRange.Start != want || suppressed.SuppressedBy.DirectiveID != "directive-2" {
					t.Fatal(suppressed)
				}
				if p.File.SHA256 != suppressed.Diagnostic.Snapshot.SHA256 {
					t.Fatal("suppressed snapshot mismatch")
				}
			}
		})
	}
	// Missing ordinary defaults still make lint incomplete; inspection reports
	// their absence as observations without changing the implemented rule status.
	putConfigurationFile(t, root, ".mdchk.yaml", availableConfiguration)
	putConfigurationFile(t, root, "doc.md", "missing")
	got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
	out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
	if got.Status != 15 || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD047" || out.Complete {
		t.Fatal(got)
	}
	for _, c := range out.Coverage {
		if c.Rule == "MD047" {
			t.Fatal("implemented MD047 reported unavailable", c)
		}
	}
}
