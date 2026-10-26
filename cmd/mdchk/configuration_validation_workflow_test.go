package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

// These assertions run the production executable, not the dispatcher or a
// test-only validator. Git is used only to construct a tracked-file fixture.
func TestConfigurationValidationWorkflow(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	env := validationEnvironment(t)
	run := func(t *testing.T, root, snapshotRoot string, args ...string) configurationRun {
		t.Helper()
		before := configurationBytes(t, snapshotRoot)
		defer func() {
			if !reflect.DeepEqual(before, configurationBytes(t, snapshotRoot)) {
				t.Error("validation changed fixture bytes or files")
			}
		}()
		got := runConfigurationProcessEnv(t, binary, root, env, args...)
		if strings.Contains(strings.Join(args, " "), "--format=json") {
			got.Result = validateConfigurationOperationJSON(t, []byte(got.Stdout), "config validate")
			checkConfigurationLinks(t, root, got)
			assertValidationOnly(t, got.Result)
		}
		return got
	}
	t.Run("independent strict definitions and located evidence", func(t *testing.T) {
		root := configurationProject(t)
		cases := []struct {
			name, yaml, message string
			positions           [][2]int
		}{
			{"good", availableConfiguration, "", nil},
			{"good-disabled", availableConfiguration + "rules: {MD013: {enabled: false, length: 80}}\n", "", nil},
			{"duplicate", availableConfiguration + "version: 1\n", "duplicate key", [][2]int{{1, 1}, {4, 1}}},
			{"unknown-rule", availableConfiguration + "rules: {MD999: {}}\n", "unknown or reserved rule", [][2]int{{4, 16}}},
			{"unknown-option", availableConfiguration + "rules: {MD013: {enabled: false, lenght: 80}}\n", "unknown option", [][2]int{{4, 41}}},
			{"disabled-domain", availableConfiguration + "rules: {MD013: {enabled: false, length: -1}}\n", "value outside integer option domain", [][2]int{{4, 41}}},
			{"explicit-unavailable", availableConfiguration + "rules: {MD009: {}}\n", "explicitly enabled rule is unavailable", [][2]int{{4, 16}}},
		}
		for _, tc := range cases {
			putConfigurationFile(t, root, tc.name+"/.mdchk.yaml", tc.yaml)
		}
		// Invalid UTF-8 proves validation does not accidentally begin Markdown work.
		putConfigurationFile(t, root, "bad.md", "bad\xff")
		got := run(t, root, root, "config", "validate", "--format=json")
		assertValidationSummary(t, got, 12, true, 7, 2, 5, 0)
		if len(got.Result.Errors) != 6 || len(got.Result.CheckedFiles) != 7 || len(got.Result.Files) != 7 {
			t.Fatalf("independent decisions lost: %+v", got.Result)
		}
		for _, f := range got.Result.Files {
			if f.Analysis != "complete" || f.InputSnapshot == nil {
				t.Fatalf("readable invalid decision incomplete: %+v", f)
			}
		}
		for _, tc := range cases {
			for _, pos := range tc.positions {
				path := tc.name + "/.mdchk.yaml"
				requireDiscoveryErrorSource(t, got, root, path, pos[0], pos[1])
				found := false
				for _, e := range got.Result.Errors {
					if e.Path == path && strings.Contains(e.Message, tc.message) && e.Category == result.Configuration {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing %s rejection: %+v", tc.name, got.Result.Errors)
				}
			}
		}
		text := run(t, root, root, "config", "validate", "--format=text")
		if text.Status != 12 || !strings.Contains(text.Stdout, "Checked 7 definitions: 2 valid, 5 invalid") {
			t.Fatalf("text counts/status: %+v", text)
		}
	})
	t.Run("atomic invalid policies and inherited lock", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", availableConfiguration+"allow_nested_excludes: false\n")
		putConfigurationFile(t, root, "middle/.mdchk.yaml", availableConfiguration+"allow_nested_excludes: true\n")
		putConfigurationFile(t, root, "middle/child/.mdchk.yaml", availableConfiguration+"exclude: ['hidden/']\n")
		putConfigurationFile(t, root, "middle/child/hidden/.mdchk.yaml", availableConfiguration)
		got := run(t, root, root, "config", "validate", "--format=json")
		assertValidationSummary(t, got, 12, true, 4, 3, 1, 0)
		requireDiscoveryErrorSource(t, got, root, "middle/child/.mdchk.yaml", 4, 10)
		requireDiscoveryErrorSource(t, got, root, ".mdchk.yaml", 4, 24)
		if len(got.Result.Errors) != 2 {
			t.Fatalf("policy origins: %+v", got.Result.Errors)
		}
	})
	t.Run("ancestor execution settings are not merged", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", availableConfiguration)
		putConfigurationFile(t, root, "child/.mdchk.yaml", "version: 1\nflavor: unknown\n")
		got := run(t, root, root, "config", "validate", "--format=json")
		assertValidationSummary(t, got, 12, true, 2, 1, 1, 0)
		requireDiscoveryErrorSource(t, got, root, "child/.mdchk.yaml", 2, 9)
		if len(got.Result.Errors) != 1 || !strings.Contains(got.Result.Errors[0].Message, "expected commonmark or gfm") {
			t.Fatalf("ancestor flavor leaked into child: %+v", got.Result)
		}
	})
	t.Run("invalid definition activates neither exclusions nor restrictions", func(t *testing.T) {
		for _, invalid := range []string{
			"version: 1\nflavor: commonmark\nexclude: ['**']\nallow_nested_excludes: false\nunknown: true\n",
			"version: 1\nflavor: unknown\nexclude: ['**']\nallow_nested_excludes: false\n",
			"version: 1\nexclude: [\n",
		} {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", invalid)
			putConfigurationFile(t, root, "child/.mdchk.yaml", availableConfiguration+"exclude: ['hidden/']\n")
			putConfigurationFile(t, root, "child/hidden/.mdchk.yaml", "invalid: [\n")
			got := run(t, root, root, "config", "validate", "--format=json")
			assertValidationSummary(t, got, 12, true, 2, 1, 1, 0)
			requireValidationSkip(t, got, "child/hidden", "configuration-exclusion")
		}
	})
	t.Run("relative accumulated exclusions and self validation", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", availableConfiguration+"exclude: ['.mdchk.yaml', 'blocked/']\n")
		putConfigurationFile(t, root, "blocked/.mdchk.yaml", "invalid: [\n")
		putConfigurationFile(t, root, "branch/.mdchk.yaml", availableConfiguration+"exclude: ['hidden/']\n")
		putConfigurationFile(t, root, "branch/hidden/.mdchk.yaml", "invalid: [\n")
		putConfigurationFile(t, root, "sibling/hidden/.mdchk.yaml", availableConfiguration)
		got := run(t, root, root, "config", "validate", "--format=json")
		assertValidationSummary(t, got, 0, true, 3, 3, 0, 0)
		requireValidationSkip(t, got, "blocked", "configuration-exclusion")
		requireValidationSkip(t, got, "branch/hidden", "configuration-exclusion")
		if len(got.Result.SkippedFiles) != 3 {
			t.Fatalf("skip roots: %+v", got.Result.SkippedFiles)
		}
	})
	t.Run("selected subroot ignores hostile outer policy", func(t *testing.T) {
		outer := configurationProject(t)
		putConfigurationFile(t, outer, ".mdchk.yaml", "invalid: [\nallow_nested_excludes: false\n")
		putConfigurationFile(t, outer, "project/.mdchk.yaml", availableConfiguration+"exclude: ['hidden/']\n")
		putConfigurationFile(t, outer, "project/hidden/.mdchk.yaml", "invalid: [\n")
		got := run(t, outer, outer, "config", "validate", "project", "--format=json")
		assertValidationSummary(t, got, 0, true, 1, 1, 0, 0)
		if !reflect.DeepEqual(got.Result.CheckedFiles, []string{"project/.mdchk.yaml"}) {
			t.Fatalf("root boundary: %+v", got.Result)
		}
		requireValidationSkip(t, got, "project/hidden", "configuration-exclusion")
	})
	t.Run("tracked ignored definitions and reachable negation", func(t *testing.T) {
		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		git, err := exec.LookPath("git")
		if err != nil {
			t.Skip("Git unavailable for tracked fixture setup")
		}
		for _, args := range [][]string{{"init", "-q", root}, {"-C", root, "config", "core.excludesFile", filepath.ToSlash(filepath.Join(root, "absent"))}} {
			//nolint:gosec // Located Git constructs this test-owned repository only.
			cmd := exec.Command(git, args...)
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git setup: %v: %s", err, out)
			}
		}
		for _, name := range []string{"tracked/.mdchk.yaml", "visible/keep/.mdchk.yaml", "visible/drop/.mdchk.yaml", "pruned/keep/.mdchk.yaml"} {
			putConfigurationFile(t, root, name, availableConfiguration)
		}
		//nolint:gosec // Fixed setup command records only the test-owned definition.
		cmd := exec.Command(git, "-C", root, "add", "tracked/.mdchk.yaml")
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git add: %v: %s", err, out)
		}
		putConfigurationFile(t, root, ".gitignore", "tracked/\nvisible/*\n!visible/keep/\npruned/\n!pruned/keep/\n")
		got := run(t, root, root, "config", "validate", "--format=json")
		assertValidationSummary(t, got, 0, true, 1, 1, 0, 0)
		if !reflect.DeepEqual(got.Result.CheckedFiles, []string{"visible/keep/.mdchk.yaml"}) {
			t.Fatalf("ignore negation/tracking: %+v", got.Result)
		}
		for _, path := range []string{"tracked", "visible/drop", "pruned"} {
			requireValidationSkip(t, got, path, "gitignore")
		}
	})
	t.Run("unsupported Git escaping remains a source failure", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", availableConfiguration)
		putConfigurationFile(t, root, ".git/config", "[core]\nexcludesFile = C:\\\\Users\\\\runner\\\\absent\n")
		got := run(t, root, root, "config", "validate", "--format=json")
		assertValidationSummary(t, got, 13, false, 0, 0, 0, 0)
		if got.Result.Complete || len(got.Result.Errors) != 1 || got.Result.Errors[0].Category != result.IO || got.Result.Errors[0].Code != "config-ignore" || !strings.Contains(got.Result.Errors[0].Message, "unsupported Git config escaping") || len(got.Result.CheckedFiles)+len(got.Result.Files) != 0 {
			t.Fatalf("unsupported policy was disregarded: %+v", got)
		}
	})

	t.Run("symlinks and empty scans", func(t *testing.T) {
		root := configurationProject(t)
		outside := configurationProject(t)
		putConfigurationFile(t, outside, ".mdchk.yaml", "invalid: [\n")
		if err := os.Symlink(filepath.Join(outside, ".mdchk.yaml"), filepath.Join(root, ".mdchk.yaml")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
			t.Fatal(err)
		}
		got := run(t, root, root, "config", "validate", "--format=json")
		assertValidationSummary(t, got, 0, true, 0, 0, 0, 0)
		requireValidationSkip(t, got, ".mdchk.yaml", "symlink")
		requireValidationSkip(t, got, "linked", "symlink")
		rejected := run(t, root, root, "config", "validate", "linked", "--format=json")
		if rejected.Status != 11 || len(rejected.Result.Errors) != 1 || rejected.Result.Errors[0].Category != result.Usage || len(rejected.Result.CheckedFiles)+len(rejected.Result.Files) != 0 {
			t.Fatalf("symlink root must fail before acquisition: %+v", rejected)
		}
		empty := configurationProject(t)
		for _, format := range []string{"json", "text"} {
			got = run(t, empty, empty, "config", "validate", "--format="+format)
			if got.Status != 0 {
				t.Fatalf("empty %s: %+v", format, got)
			}
			if format == "json" {
				assertValidationSummary(t, got, 0, true, 0, 0, 0, 0)
			} else if !strings.Contains(got.Stdout, "Checked 0 definitions: 0 valid, 0 invalid") {
				t.Fatalf("empty text: %s", got.Stdout)
			}
		}
	})
	t.Run("individually ignored definitions are counted without acquisition", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".gitignore", "child/.mdchk.yaml\n")
		putConfigurationFile(t, root, "child/.mdchk.yaml", "invalid: [\n")
		got := run(t, root, root, "config", "validate", "--format=json")
		assertValidationSummary(t, got, 0, true, 0, 0, 0, 1)
		requireValidationSkip(t, got, "child/.mdchk.yaml", "gitignore")
		if len(got.Result.CheckedFiles)+len(got.Result.Files) != 0 {
			t.Fatalf("ignored source was acquired: %+v", got.Result)
		}
	})
	t.Run("runtime quotas preserve actual attempt stages", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", availableConfiguration)
		got := run(t, root, root, "config", "validate", "--format=json", "--limit=work-units=1")
		assertValidationSummary(t, got, 15, false, 0, 0, 0, 0)
		if len(got.Result.Interruptions) != 1 || len(got.Result.CheckedFiles) != 0 || len(got.Result.Files) != 0 {
			t.Fatalf("quota failed before acquisition: %+v", got.Result)
		}
		got = run(t, root, root, "config", "validate", "--format=json", "--limit=structured-input-depth=1")
		assertValidationSummary(t, got, 15, false, 0, 0, 0, 0)
		if len(got.Result.Interruptions) != 1 || !reflect.DeepEqual(got.Result.CheckedFiles, []string{".mdchk.yaml"}) || len(got.Result.Files) != 1 || got.Result.Files[0].Analysis != "partial" || got.Result.Files[0].InputSnapshot == nil {
			t.Fatalf("acquired interrupted attempt: %+v", got.Result)
		}
	})
	t.Run("tiny reporting deadline preserves actual counts", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", availableConfiguration)
		for _, format := range []string{"json", "text"} {
			for _, duration := range []string{"1ns", "1us"} {
				got := run(t, root, root, "config", "validate", "--format="+format, "--limit=reporting-time="+duration)
				if (got.Status != 0 && got.Status != 15) || got.Stdout == "" {
					t.Fatalf("invalid reporting outcome: %+v", got)
				}
				if format == "json" {
					assertValidationSummary(t, got, got.Status, true, 1, 1, 0, 0)
					if len(got.Result.Errors) != 0 {
						t.Fatalf("unexpected error: %+v", got.Result.Errors)
					}
					if got.Status == 0 {
						if !got.Result.Complete || got.Result.Status != "clean" || len(got.Result.Interruptions)+len(got.Result.Omissions) != 0 || !reflect.DeepEqual(got.Result.CheckedFiles, []string{".mdchk.yaml"}) || len(got.Result.Files) != 1 || got.Result.Files[0].Analysis != "complete" || got.Result.Files[0].InputSnapshot == nil || got.Result.Files[0].ResultSnapshot == nil {
							t.Fatalf("incoherent completed report: %+v", got.Result)
						}
					} else if got.Result.Complete || got.Result.Status != "incomplete" || len(got.Result.Omissions) == 0 || len(got.Result.Interruptions) != 1 || got.Result.Interruptions[0].Phase != "reporting" || got.Result.Interruptions[0].Reason != "deadline-exceeded" {
						t.Fatalf("incoherent interrupted report: %+v", got.Result)
					}
				} else {
					complete := got.Status == 0
					status := "incomplete"
					if complete {
						status = "clean"
					}
					want := fmt.Sprintf("Checked 1 definitions: 1 valid, 0 invalid, 0 known definitions skipped. 1 skipped paths, 0 errors; %s (scan complete: true, complete: %t).\n", status, complete)
					reserveWant := strings.Replace(want, "1 skipped paths", "0 skipped paths", 1)
					if (!strings.Contains(got.Stdout, want) && (complete || !strings.Contains(got.Stdout, reserveWant))) || strings.Contains(got.Stdout, "reporting-time exhausted") == complete {
						t.Fatalf("incoherent text report: %+v", got)
					}
				}
			}
		}
	})
	t.Run("preflight and metadata precede project access", func(t *testing.T) {
		root := configurationProject(t)
		for _, args := range [][]string{{"config", "validate", "--help", "missing"}, {"config", "validate", "--version", "missing"}} {
			got := run(t, root, root, args...)
			if got.Status != 0 {
				t.Fatalf("metadata: %+v", got)
			}
		}
		for _, tc := range []struct{ extra, message string }{
			{"--config=missing", "does not accept --config"},
			{"--limit=document-bytes=1", `limit "document-bytes" does not apply`},
		} {
			got := runConfigurationProcessEnv(t, binary, root, env, "config", "validate", tc.extra, "missing")
			if got.Status != 11 || !strings.Contains(got.Stdout, tc.message) {
				t.Fatalf("preflight %s: %+v", tc.extra, got)
			}
		}
	})
	t.Run("permission failures preserve independent outcomes", func(t *testing.T) {
		for _, kind := range []string{"definition", "directory", "ignore"} {
			t.Run(kind, func(t *testing.T) {
				root := configurationProject(t)
				putConfigurationFile(t, root, "a/.mdchk.yaml", availableConfiguration)
				name := "denied/.mdchk.yaml"
				code := 12
				if kind == "directory" {
					name = "denied"
					code = 13
				}
				if kind == "ignore" {
					name = "denied/.gitignore"
					code = 13
				}
				putConfigurationFile(t, root, "denied/.mdchk.yaml", availableConfiguration)
				if kind == "ignore" {
					putConfigurationFile(t, root, name, "unused\n")
				}
				before := configurationBytes(t, root)
				path := filepath.Join(root, filepath.FromSlash(name))
				mode := os.FileMode(0600)
				if kind == "directory" {
					mode = 0700
				}
				if err := os.Chmod(path, 0); err != nil {
					t.Skipf("permission denial unavailable: %v", err)
				}
				defer func() {
					if err := os.Chmod(path, mode); err != nil {
						t.Error(err)
						return
					}
					if !reflect.DeepEqual(before, configurationBytes(t, root)) {
						t.Error("validation wrote fixture")
					}
				}()
				var probe error
				if kind == "directory" {
					_, probe = os.ReadDir(path)
				} else {
					//nolint:gosec // Test-owned permission-denial probe.
					_, probe = os.ReadFile(path)
				}
				if probe == nil {
					t.Skip("OS/user permits mode-000 access; denial cannot be tested")
				}
				if !errors.Is(probe, os.ErrPermission) {
					t.Fatal(probe)
				}
				for _, format := range []string{"json", "text"} {
					got := runConfigurationProcessEnv(t, binary, root, env, "config", "validate", "--format="+format)
					if got.Status != code {
						t.Fatalf("permission %s: %+v", kind, got)
					}
					if format == "json" {
						got.Result = validateConfigurationOperationJSON(t, []byte(got.Stdout), "config validate")
						assertValidationOnly(t, got.Result)
						assertValidationSummary(t, got, code, false, 1, 1, 0, 0)
						if !reflect.DeepEqual(got.Result.CheckedFiles, []string{"a/.mdchk.yaml"}) || len(got.Result.Errors) != 1 {
							t.Fatalf("partial outcomes: %+v", got.Result)
						}
						wantCategory := result.IO
						if kind == "definition" {
							wantCategory = result.Configuration
						}
						if got.Result.Errors[0].Category != wantCategory || got.Result.Errors[0].Snapshot != nil {
							t.Fatalf("unreadable evidence fabricated: %+v", got.Result.Errors)
						}
						if kind == "definition" {
							if len(got.Result.Files) != 2 {
								t.Fatalf("missing unreadable outcome: %+v", got.Result.Files)
							}
							for _, f := range got.Result.Files {
								if f.Path == name && (f.Analysis != "failed" || f.InputSnapshot != nil || len(f.ErrorIDs) != 1) {
									t.Fatalf("unreadable file: %+v", f)
								}
							}
						}
					} else if !strings.Contains(got.Stdout, "Checked 1 definitions: 1 valid, 0 invalid") {
						t.Fatalf("partial text: %s", got.Stdout)
					}
				}
			})
		}
	})
}

func validationEnvironment(t *testing.T) []string {
	t.Helper()
	var env []string
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		if !strings.HasPrefix(key, "GIT_") && key != "HOME" && key != "XDG_CONFIG_HOME" && key != "PATH" {
			env = append(env, v)
		}
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return append(env, "PATH="+filepath.Join(home, "no-runtime-programs"), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, "xdg"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(home, "gitconfig"), "GIT_TERMINAL_PROMPT=0")
}

func assertValidationOnly(t *testing.T, r result.Result) {
	t.Helper()
	if len(r.Diagnostics)+len(r.Coverage)+len(r.Changes)+len(r.DeferredFixes)+len(r.Documents)+len(r.SuppressedDiagnostics)+len(r.Notes) != 0 {
		t.Fatalf("validation performed Markdown/fix/return work: %+v", r)
	}
	for _, f := range r.Files {
		if !reflect.DeepEqual(f.InputSnapshot, f.ResultSnapshot) {
			t.Fatalf("read-only snapshots differ: %+v", f)
		}
		if f.WriteStatus != "not-requested" || f.ReturnStatus != "not-requested" || len(f.ChangeIDs)+len(f.CoverageIDs) != 0 {
			t.Fatalf("nonvalidation outcome: %+v", f)
		}
	}
}
func assertValidationSummary(t *testing.T, got configurationRun, status int, complete bool, checked, valid, invalid, skipped int) {
	t.Helper()
	want := result.ConfigValidation{ScanComplete: complete, DefinitionsChecked: checked, ValidDefinitions: valid, InvalidDefinitions: invalid, SkippedDefinitions: skipped}
	if got.Status != status || got.Result.ConfigValidation == nil || *got.Result.ConfigValidation != want {
		t.Fatalf("status/counts: got %d %+v; want %d %+v", got.Status, got.Result.ConfigValidation, status, want)
	}
	if len(got.Result.Interruptions) == 0 && got.Result.Complete != complete {
		t.Fatalf("scan/envelope completeness: %+v", got.Result)
	}
}
func requireValidationSkip(t *testing.T, got configurationRun, path, reason string) {
	t.Helper()
	for _, s := range got.Result.SkippedFiles {
		if s.Path == path && s.Reason == reason {
			return
		}
	}
	t.Fatalf("missing skip %s (%s): %+v", path, reason, got.Result.SkippedFiles)
}
