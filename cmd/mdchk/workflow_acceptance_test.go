package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func TestLintWorkflowAcceptance(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	t.Run("metadata bypasses project access", func(t *testing.T) {
		for _, invalid := range []bool{false, true} {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if invalid {
				putConfigurationFile(t, root, ".mdchk.yaml", "invalid: [\n")
			}
			before := configurationBytes(t, root)
			defer func() {
				if !reflect.DeepEqual(before, configurationBytes(t, root)) {
					t.Error("metadata changed fixture bytes")
				}
			}()
			var version string
			for _, args := range [][]string{
				{"--help"}, {"--version"},
				{"lint", "--help"}, {"lint", "--version"},
				{"lint", "--help", "--config=missing.yaml", "missing.md"},
				{"lint", "--version", "--config=missing.yaml", "missing.md"},
			} {
				got := runConfigurationProcess(t, binary, root, args...)
				if got.Status != 0 || strings.HasPrefix(got.Stdout, "{") {
					t.Fatalf("metadata %v: %+v", args, got)
				}
				if strings.Contains(strings.Join(args, " "), "--help") {
					want := "Usage: mdchk [global options] <command>"
					if args[0] == "lint" {
						want = "Usage: mdchk lint"
					}
					if !strings.Contains(got.Stdout, want) {
						t.Fatalf("help %v: %q", args, got.Stdout)
					}
				} else {
					if !strings.HasPrefix(got.Stdout, "mdchk ") || len(strings.Fields(got.Stdout)) != 2 || !strings.HasSuffix(got.Stdout, "\n") {
						t.Fatalf("version: %q", got.Stdout)
					}
					if version != "" && version != got.Stdout {
						t.Fatalf("version depends on command: %q / %q", version, got.Stdout)
					}
					version = got.Stdout
				}
			}
		}
	})
	t.Run("successful selection and argument forms", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", cleanConfiguration())
		for _, name := range []string{"a.md", "docs/b.MARKDOWN", "--draft.md", "explicit.txt"} {
			putConfigurationFile(t, root, name, "# 雪\r\n")
		}
		for _, tc := range []struct {
			name        string
			args, paths []string
		}{
			{"directory", []string{"lint", "--format=json", "docs"}, []string{"docs/b.MARKDOWN"}},
			{"default", []string{"lint", "--format=json"}, []string{"--draft.md", "a.md", "docs/b.MARKDOWN"}},
			{"overlapping dedup and explicit nonmarkdown", []string{"lint", "--format=json", "docs/b.MARKDOWN", "docs", "explicit.txt"}, []string{"docs/b.MARKDOWN", "explicit.txt"}},
			{"literal leading dash", []string{"lint", "--format=json", "--", "--draft.md"}, []string{"--draft.md"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				got := runConfiguration(t, binary, root, root, tc.args...)
				if got.Status != 0 || !reflect.DeepEqual(got.Result.CheckedFiles, tc.paths) || len(got.Result.Files) != len(tc.paths) || len(got.Result.Errors) != 0 || len(got.Result.Coverage) != 0 {
					t.Fatalf("selection: %+v", got)
				}
			})
		}
		for _, format := range []string{"text", "json"} {
			before := runConfiguration(t, binary, root, root, "lint", "--format="+format, "--config=.mdchk.yaml", "a.md")
			after := runConfiguration(t, binary, root, root, "lint", "a.md", "--config", ".mdchk.yaml", "--format", format)
			if before.Status != 0 || after.Status != 0 || before.Stdout != after.Stdout {
				t.Fatalf("option placement changed result: %+v / %+v", before, after)
			}
		}
	})
}

func TestLintRegisteredRuleWorkflow(t *testing.T) {
	binary := buildConfigurationFixtureExecutable(t)
	t.Run("validated options and enablement reach callback", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, "doc.md", "# 雪\r\nfixture style\r\n")
		for _, tc := range []struct {
			name, setting string
			status        int
		}{
			{"catalog option default", "{enabled: true}", 0},
			{"matching option override", "{enabled: true, style: setext}", 10},
			{"different option override", "{enabled: true, style: atx}", 0},
			{"disabled with matching option", "{enabled: false, style: setext}", 0},
		} {
			t.Run(tc.name, func(t *testing.T) {
				config := strings.Replace(fixtureConfiguration(false, false, false), "MD003: {enabled: false}", "MD003: "+tc.setting, 1)
				putConfigurationFile(t, root, ".mdchk.yaml", config)
				for _, format := range []string{"text", "json"} {
					got := runConfiguration(t, binary, root, root, "lint", "doc.md", "--format="+format)
					if got.Status != tc.status {
						t.Fatalf("option/enablement outcome: %+v", got)
					}
					if format == "text" {
						if strings.Contains(got.Stdout, "doc.md:2:1 MD003: fixture style setext") != (tc.status == 10) {
							t.Fatalf("option text: %s", got.Stdout)
						}
						continue
					}
					if len(got.Result.Coverage) != 0 || len(got.Result.Errors) != 0 || len(got.Result.CheckedFiles) != 1 {
						t.Fatalf("option result: %+v", got.Result)
					}
					if tc.status == 10 {
						if len(got.Result.Diagnostics) != 1 || got.Result.Diagnostics[0].Rule != "MD003" || got.Result.Diagnostics[0].Message != "fixture style setext" || got.Result.Diagnostics[0].Range.Start.Line != 2 || got.Result.Diagnostics[0].Range.Start.Column != 1 {
							t.Fatalf("option finding: %+v", got.Result.Diagnostics)
						}
					} else if len(got.Result.Diagnostics) != 0 {
						t.Fatalf("unexpected finding: %+v", got.Result.Diagnostics)
					}
				}
			})
		}
	})
	t.Run("unreadable input retains readable findings", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", fixtureConfiguration(false, true, false))
		putConfigurationFile(t, root, "a.md", "fixture violation\n")
		putConfigurationFile(t, root, "denied.md", "unchanged denied source\r\n")
		before := configurationBytes(t, root)
		denied := filepath.Join(root, "denied.md")
		if err := os.Chmod(denied, 0); err != nil {
			t.Skipf("permission denial unavailable: %v", err)
		}
		defer func() {
			if err := os.Chmod(denied, 0600); err != nil {
				t.Error(err)
				return
			}
			if !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Error("lint changed fixture bytes or files")
			}
		}()
		// Probe actual OS behavior: root and some platforms can still read mode 000.
		permissionRoot, err := os.OpenRoot(root)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := permissionRoot.Close(); err != nil {
				t.Error(err)
			}
		}()
		_, err = permissionRoot.ReadFile("denied.md")
		if err == nil {
			t.Skip("OS/user can read mode-000 file; permission-denied integration unavailable")
		}
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("permission probe: %v", err)
		}
		for _, format := range []string{"json", "text"} {
			t.Run(format, func(t *testing.T) {
				if err := os.Chmod(denied, 0); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := os.Chmod(denied, 0600); err != nil {
						t.Error(err)
						return
					}
					if !reflect.DeepEqual(before, configurationBytes(t, root)) {
						t.Error("lint changed fixture bytes or files")
					}
				}()
				got := runConfigurationProcess(t, binary, root, "lint", "--format="+format, "a.md", "denied.md")
				if got.Status != 13 {
					t.Fatalf("unreadable status: %+v", got)
				}
				if format == "text" {
					if !strings.Contains(got.Stdout, "a.md:1:1 MD001: fixture violation") || !strings.Contains(got.Stdout, "denied.md") || !strings.Contains(got.Stdout, "(io)") {
						t.Fatalf("partial text: %s", got.Stdout)
					}
					return
				}
				got.Result = validateConfigurationJSON(t, []byte(got.Stdout))
				checkConfigurationLinks(t, root, got)
				r := got.Result
				if len(r.Diagnostics) != 1 || r.Diagnostics[0].Path != "a.md" || len(r.Errors) != 1 || r.Errors[0].Path != "denied.md" || r.Errors[0].Category != result.IO || len(r.Files) != 2 || !reflect.DeepEqual(r.CheckedFiles, []string{"a.md"}) {
					t.Fatalf("partial unreadable report: %+v", r)
				}
				for _, f := range r.Files {
					if f.Path == "denied.md" && (len(f.ErrorIDs) != 1 || f.ErrorIDs[0] != r.Errors[0].ID || f.InputSnapshot != nil || f.Analysis != "failed") {
						t.Fatalf("denied outcome: %+v", f)
					}
				}
			})
		}
	})
}
