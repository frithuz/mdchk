package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCompiledApplyNoCGOClearlyRefuses(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "mdchk")
	//nolint:gosec // Fixed Go tool builds this package into a synthetic destination.
	command := exec.Command("go", "build", "-o", binary, ".")
	command.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, e := command.CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, output)
	}
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
	putConfigurationFile(t, root, "doc.md", "hello")
	before := configurationBytes(t, root)
	got := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
	parsed := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
	if got.Status != 13 || !strings.Contains(got.Stdout, "unsupported") || len(parsed.Changes) > 0 || !reflect.DeepEqual(before, configurationBytes(t, root)) {
		t.Fatal(got)
	}
	// The same unsupported build must finish terminal discovery without asking
	// for native write authority. Exercise actual serialized command results.
	for _, tc := range []struct {
		name, config, path string
		status             int
		code               string
		skip               bool
	}{
		{"empty", md047Configuration(true, true), ".", 11, "no-files", false},
		{"excluded", md047Configuration(true, true) + "exclude: ['doc.md']\n", "doc.md", 11, "no-files", true},
		{"missing", md047Configuration(true, true), "missing.md", 13, "selection-failed", false},
		{"invalid-config", "version: 2\n", ".", 12, "config-definition", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", tc.config)
			if tc.skip {
				putConfigurationFile(t, root, "doc.md", "hello")
			}
			before := configurationBytes(t, root)
			prior := map[string]os.FileInfo{}
			for path := range before {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				prior[path] = info
			}
			for _, dry := range []bool{false, true} {
				for _, format := range []string{"json", "text"} {
					args := []string{"fix", tc.path, "--format=" + format}
					if dry {
						args = append(args, "--dry-run")
					}
					got := runConfigurationProcess(t, binary, root, args...)
					if got.Status != tc.status || strings.Contains(got.Stdout, "write-acquisition") {
						t.Fatal(got)
					}
					if format == "json" {
						r := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
						excluded := false
						for _, skip := range r.SkippedFiles {
							if skip.Path == "doc.md" && skip.Reason == "excluded" {
								excluded = true
							}
						}
						if excluded != tc.skip {
							t.Fatal(r.SkippedFiles)
						}
						if len(r.Errors) != 1 || r.Errors[0].Code != tc.code || len(r.CheckedFiles) != 0 || len(r.Changes) != 0 || len(r.Interruptions) != 0 {
							t.Fatal(r)
						}
					}
					for path, info := range prior {
						after, err := os.Stat(path)
						if err != nil {
							t.Fatal(err)
						}
						if !os.SameFile(info, after) || info.Mode() != after.Mode() || info.ModTime() != after.ModTime() {
							t.Fatal("terminal discovery changed metadata", path)
						}
					}
					if !reflect.DeepEqual(before, configurationBytes(t, root)) {
						t.Fatal("terminal discovery changed fixture inventory or bytes")
					}
				}
			}
		})
	}

}
