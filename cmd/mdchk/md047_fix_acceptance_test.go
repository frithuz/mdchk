package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/frithuz/mdchk/internal/result"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// This workflow compiles the ordinary production command with no provider
// overlay. The retained byte fixtures are shared acceptance inputs for #183.
func TestCompiledProductionMD047FixWorkflow(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	data, err := os.ReadFile("testdata/md047_corrections.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Source, Candidate, Config string
		Status                          int
		Eligible, Deferred              bool
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			root := configurationProject(t)
			config := md047Configuration(tc.Config != "off", tc.Config != "blocked")
			putConfigurationFile(t, root, ".mdchk.yaml", config)
			putConfigurationFile(t, root, "doc.md", tc.Source)
			before := configurationBytes(t, root)
			stat, err := os.Stat(filepath.Join(root, "doc.md"))
			if err != nil {
				t.Fatal(err)
			}
			// Lint and inspect eligibility are independently observed against these
			// explicitly specified context expectations; neither constructs edits.
			lint := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
			lintResult := validateConfigurationOperationJSON(t, []byte(lint.Stdout), "lint")
			for _, d := range lintResult.Diagnostics {
				if d.Rule == "MD047" && d.FixAvailable != tc.Eligible {
					t.Fatal("lint eligibility", d, tc.Eligible)
				}
			}
			seen, _ := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
			for _, d := range seen.Result.Diagnostics {
				if d.Rule == "MD047" && d.FixAvailable != tc.Eligible {
					t.Fatal("inspection eligibility", d)
				}
			}
			for _, format := range []string{"json", "text"} {
				got := runFixConfiguration(t, binary, root, "fix", "--config=.mdchk.yaml", "--dry-run", "doc.md", "--format="+format)
				if got.Status != tc.Status {
					t.Fatalf("status %d want %d: %s", got.Status, tc.Status, got.Stdout)
				}
				changed := tc.Source != tc.Candidate
				if format == "text" {
					if strings.Contains(got.Stdout, "proposed doc.md") != changed {
						t.Fatal(got.Stdout)
					}
					continue
				}
				r := got.Result
				if !r.Complete || len(r.Documents) != 0 || len(r.Files) != 1 || (len(r.Changes) > 0) != changed || (len(r.DeferredFixes) > 0) != tc.Deferred {
					t.Fatalf("unexpected result: %+v", r)
				}
				file := r.Files[0]
				if file.InputSnapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) || file.ResultSnapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Candidate))) || file.ResultSnapshot.SizeBytes != len(tc.Candidate) {
					t.Fatal(file)
				}
				for _, d := range r.Diagnostics {
					if d.Snapshot != *file.ResultSnapshot {
						t.Fatal("remaining coordinates not candidate", d)
					}
					if d.Rule == "MD047" && d.FixAvailable {
						t.Fatal("deferred correction advertised", d)
					}
				}
				if changed {
					change := r.Changes[0]
					if change.EditCount != 1 || !reflect.DeepEqual(change.Rules, []string{"MD047"}) || change.Disposition != "proposed" || change.Diff == nil || change.Diff.ContextLines != 3 || change.Before != *file.InputSnapshot || change.Candidate != *file.ResultSnapshot {
						t.Fatal(change)
					}
					if tc.Name == "missing-LF" && !reflect.DeepEqual(change.Diff.Hunks[0].Lines, []result.DiffLine{{Kind: "remove", Text: "text", LineEnding: "none"}, {Kind: "add", Text: "text", LineEnding: "lf"}}) {
						t.Fatal(change.Diff)
					}
					if tc.Name == "surplus-mixed" && !reflect.DeepEqual(change.Diff.Hunks[0].Lines, []result.DiffLine{{Kind: "context", Text: "text", LineEnding: "crlf"}, {Kind: "remove", Text: "", LineEnding: "lf"}, {Kind: "remove", Text: "", LineEnding: "cr"}}) {
						t.Fatal(change.Diff)
					}
				} else if file.WriteStatus != "not-needed" {
					t.Fatal(file)
				}
				for _, d := range r.DeferredFixes {
					if d.Rule != "MD047" || !strings.HasPrefix(d.GroupID, "round-1:MD047-terminal-") || d.Snapshot.SHA256 != file.InputSnapshot.SHA256 {
						t.Fatal(d)
					}
				}
			}
			after, err := os.Stat(filepath.Join(root, "doc.md"))
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(stat, after) || stat.Mode() != after.Mode() || !stat.ModTime().Equal(after.ModTime()) || !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("dry run changed input metadata, bytes or inventory")
			}
			// Idempotence uses an isolated, test-owned candidate fixture, never the
			// operation's input target. It invokes the same real production provider.
			secondRoot := configurationProject(t)
			putConfigurationFile(t, secondRoot, ".mdchk.yaml", config)
			putConfigurationFile(t, secondRoot, "doc.md", tc.Candidate)
			second := runFixConfiguration(t, binary, secondRoot, "fix", "--dry-run", "doc.md", "--format=json")
			if second.Status != tc.Status || len(second.Result.Changes) != 0 || second.Result.Files[0].ResultSnapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Candidate))) {
				t.Fatal("not idempotent", second)
			}
		})
	}
}

func TestCompiledProductionMD047FixBoundsAndRemainingCoverage(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\nflavor: commonmark\n")
	putConfigurationFile(t, root, "doc.md", "hello")
	got := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
	if got.Status != 15 || len(got.Result.Changes) != 1 || got.Result.Complete || got.Result.Changes[0].Candidate.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte("hello\n"))) {
		t.Fatal(got)
	}
	putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
	for _, limit := range []string{"reporting-time=1ns", "response-bytes=131072", "work-units=1"} {
		if limit == "response-bytes=131072" {
			putConfigurationFile(t, root, "doc.md", strings.Repeat("x", 150000))
		} else {
			putConfigurationFile(t, root, "doc.md", "hello")
		}
		got := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json", "--limit="+limit)
		if got.Status != 15 || got.Result.Complete {
			t.Fatal(limit, got)
		}
		if limit == "reporting-time=1ns" || limit == "response-bytes=131072" {
			if len(got.Result.Changes) != 1 || got.Result.Changes[0].Diff != nil {
				t.Fatal("verified proposal identity lost or unbounded diff", got)
			}
		}
	}
	putConfigurationFile(t, root, "doc.md", "hello")
	got = runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "missing.md", "--format=json")
	if got.Status != 13 || len(got.Result.Changes) != 1 {
		t.Fatal(got)
	}
}

func TestCompiledProductionMD047InheritedDirectivePermission(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, kind := range []string{"per-rule", "global", "ancestor"} {
		t.Run(kind, func(t *testing.T) {
			root := configurationProject(t)
			child := filepath.Join(root, "nested")
			parentConfig := md047Configuration(true, kind != "ancestor")
			config := md047Configuration(true, kind != "per-rule")
			if kind == "global" {
				config = strings.Replace(config, "directives: {enabled: true}", "directives: {enabled: false}", 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", parentConfig)
			putConfigurationFile(t, root, "nested/.mdchk.yaml", config)
			source := "<!-- mdchk:disable MD047 -->\ntext"
			putConfigurationFile(t, root, "nested/doc.md", source)
			before := configurationBytes(t, root)
			got := runFixConfiguration(t, binary, child, "fix", "--dry-run", "doc.md", "--format=json")
			if got.Status != 0 || len(got.Result.Changes) != 1 || len(got.Result.SuppressedDiagnostics) != 0 || got.Result.Changes[0].Candidate.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(source+"\n"))) {
				t.Fatal(got)
			}
			if !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("changed inherited configuration or targets")
			}
		})
	}
}
