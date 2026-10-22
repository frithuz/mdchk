package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The host's actual filesystem and compiled command are the oracle. Logical
// paths and source byte hashes are checked independently of native arguments.
func TestCompiledPlatformPathsAndStreams(t *testing.T) {
	t.Logf("runtime GOOS=%s GOARCH=%s", runtime.GOOS, runtime.GOARCH)
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
	name := filepath.Join("space ü", "nested", "δ document.md")
	data := "first\r\n\t😀é"
	putConfigurationFile(t, root, name, data)
	putConfigurationFile(t, root, "--draft.md", "clean\n")
	verify := unchangedFixFixture(t, root)
	defer verify()
	logical := filepath.ToSlash(name)
	var reference string
	for _, paths := range [][]string{{filepath.Dir(name), filepath.Join(root, name), "--draft.md"}, {"--draft.md", name, filepath.Dir(name)}} {
		args := append([]string{"lint", "--format=json", "--"}, paths...)
		got := runConfigurationProcess(t, binary, root, args...)
		r := validateConfigurationJSON(t, []byte(got.Stdout))
		if got.Status != 10 || !reflect.DeepEqual(r.CheckedFiles, []string{"--draft.md", logical}) || len(r.Diagnostics) != 1 {
			t.Fatal(got)
		}
		d := r.Diagnostics[0]
		if d.Path != logical || d.Rule != "MD047" || d.Range.Start.Line != 2 || d.Range.Start.Column != 4 || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(data))) || d.Snapshot.SizeBytes != len(data) {
			t.Fatal(d)
		}
		if reference != "" && reference != got.Stdout {
			t.Fatal("target aliases/order changed canonical report")
		}
		reference = got.Stdout
	}
	for _, format := range []string{"text", "json", "vscode", "github"} {
		t.Run(format, func(t *testing.T) {
			got := runConfigurationProcess(t, binary, root, "lint", "--format="+format, "--", name)
			if got.Status != 10 {
				t.Fatal(got)
			}
			if format == "vscode" && !strings.Contains(got.Stdout, logical+":2:4: error: MD047:") {
				t.Fatal(got)
			}
			if format == "github" && (!strings.Contains(got.Stdout, "file="+logical+",line=2,title=MD047") || strings.Count(got.Stdout, "::error ") != 1) {
				t.Fatal(got)
			}
			clean := runConfigurationProcess(t, binary, root, "lint", "--format="+format, "--", "--draft.md")
			if clean.Status != 0 || ((format == "vscode" || format == "github") && clean.Stdout != "") {
				t.Fatal(clean)
			}
		})
	}
	preview := runConfigurationProcess(t, binary, root, "fix", "--dry-run", "--format=json", "--", name)
	r := validateConfigurationOperationJSON(t, []byte(preview.Stdout), "fix")
	if preview.Status != 0 || len(r.Changes) != 1 || r.Changes[0].Candidate.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(data+"\r\n"))) {
		t.Fatal(preview)
	}
	if runtime.GOOS != "windows" {
		t.Run("literal backslash", func(t *testing.T) {
			specialRoot := configurationProject(t)
			putConfigurationFile(t, specialRoot, ".mdchk.yaml", md047Configuration(true, true))
			putConfigurationFile(t, specialRoot, `one\component.md`, "x")
			check := unchangedFixFixture(t, specialRoot)
			defer check()
			got := runConfigurationProcess(t, binary, specialRoot, "lint", "--format=json", `one\component.md`)
			r := validateConfigurationJSON(t, []byte(got.Stdout))
			if got.Status != 10 || !reflect.DeepEqual(r.CheckedFiles, []string{`one\component.md`}) {
				t.Fatal(got)
			}
		})
	}
}

func platformProcess(t *testing.T, binary, root string, args ...string) (int, string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	//nolint:gosec // Execute the test-owned compiled binary and fixture arguments.
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = root
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatal(ctx.Err())
	}
	status := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatal(err)
		}
		status = exit.ExitCode()
	}
	return status, out.String(), errOut.String()
}

func TestCompiledPlatformFilesystemCapabilities(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	t.Run("independent path errors", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
		putConfigurationFile(t, root, "ok.md", "violation")
		check := unchangedFixFixture(t, root)
		defer check()
		for _, bad := range []string{"missing.md", filepath.Join("ok.md", "child.md")} {
			for _, format := range []string{"text", "json", "vscode", "github"} {
				status, out, errOut := platformProcess(t, binary, root, "lint", "--format="+format, "ok.md", bad)
				if status != 13 {
					t.Fatalf("%s %s status%d out%s err%s", bad, format, status, out, errOut)
				}
				if format == "json" {
					r := validateConfigurationJSON(t, []byte(out))
					if !reflect.DeepEqual(r.CheckedFiles, []string{"ok.md"}) || len(r.Errors) == 0 || len(r.Diagnostics) != 1 {
						t.Fatal(r)
					}
				}
				if format == "vscode" && (!strings.Contains(out, "ok.md:1:10:") || errOut == "") {
					t.Fatal(out, errOut)
				}
				if format == "github" && (!strings.Contains(out, "::error") || !strings.Contains(out, "MD047")) {
					t.Fatal(out, errOut)
				}
				if (format == "text" || format == "json") && errOut != "" {
					t.Fatal("ordinary report leaked stderr", errOut)
				}
			}
		}
	})
	t.Run("symlinks", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
		putConfigurationFile(t, root, "real/doc.md", "x")
		if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
			t.Skipf("actual symlink creation unavailable: %v", err)
		}
		if err := os.Symlink(filepath.Join(root, "absent"), filepath.Join(root, "dangling.md")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "real", "doc.md"), filepath.Join(root, "alias.md")); err != nil {
			t.Fatal(err)
		}
		check := unchangedFixFixture(t, root)
		defer check()
		// Deliberately concatenate: filepath.Join would erase the raw link/.. check.
		raw := "link" + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "real" + string(os.PathSeparator) + "doc.md"
		for _, name := range []string{"alias.md", "dangling.md", filepath.Join("link", "doc.md"), raw} {
			for _, op := range []string{"lint", "fix"} {
				args := []string{op, "--format=json"}
				if op == "fix" {
					args = append(args, "--dry-run")
				}
				args = append(args, name)
				got := runConfigurationProcess(t, binary, root, args...)
				r := validateConfigurationOperationJSON(t, []byte(got.Stdout), op)
				if got.Status != 11 || len(r.CheckedFiles) != 0 || len(r.SkippedFiles) == 0 {
					t.Fatal(name, got)
				}
			}
		}
		got := runConfigurationProcess(t, binary, root, "inspect", "--format=json", "alias.md")
		if got.Status != 0 || !strings.Contains(got.Stdout, fmt.Sprintf("%x", sha256.Sum256([]byte("x")))) {
			t.Fatal(got)
		}
	})
	t.Run("read denial capability", func(t *testing.T) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
		putConfigurationFile(t, root, "denied.md", "x")
		putConfigurationFile(t, root, "ok.md", "y")
		path := filepath.Join(root, "denied.md")
		if err := os.Chmod(path, 0); err != nil {
			t.Skipf("permission setup unavailable: %v", err)
		}
		defer func() {
			if err := os.Chmod(path, 0600); err != nil {
				t.Error(err)
			}
		}()
		//nolint:gosec // Open only the test-owned fixture path.
		f, err := os.Open(path)
		if err == nil {
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			t.Skipf("mode-000 read denial not enforced for this user/filesystem on %s", runtime.GOOS)
		}
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("probe did not establish permission denial: %v", err)
		}
		got := runConfigurationProcess(t, binary, root, "lint", "--format=json", "denied.md", "ok.md")
		r := validateConfigurationJSON(t, []byte(got.Stdout))
		if got.Status != 13 || len(r.Errors) == 0 || !reflect.DeepEqual(r.CheckedFiles, []string{"ok.md"}) || len(r.Diagnostics) != 1 {
			t.Fatal(got)
		}
	})
}

func TestCompiledPlatformOutputDescriptors(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
	putConfigurationFile(t, root, "doc.md", "x")
	for _, tc := range []struct {
		name   string
		args   []string
		stderr bool
	}{
		{"violation stdout", []string{"lint", "--format=json", "doc.md"}, false},
		{"incomplete stdout", []string{"lint", "--format=json", "--limit=work-units=1", "doc.md"}, false},
		{"usage stdout", []string{"lint", "--bad", "--format=json"}, false},
		{"configuration stdout", []string{"lint", "--config=missing.yaml", "--format=json", "doc.md"}, false},
		{"vscode stderr and stdout", []string{"lint", "--format=vscode", "missing.md", "doc.md"}, true},
		{"github human stderr", []string{"fix", "--dry-run", "--format=github", "missing.md", "doc.md"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "read-only")
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			//nolint:gosec // Open only the test-owned fixture path.
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := f.Close(); err != nil {
					t.Error(err)
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			//nolint:gosec // Execute the test-owned binary and fixed fixture arguments.
			cmd := exec.CommandContext(ctx, binary, tc.args...)
			cmd.Dir = root
			var other bytes.Buffer
			if tc.stderr {
				cmd.Stdout = &other
				cmd.Stderr = f
			} else {
				cmd.Stdout = f
				cmd.Stderr = &other
			}
			err = cmd.Run()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 13 {
				t.Fatalf("real descriptor failure: %v other=%s", err, &other)
			}
			if tc.stderr && other.Len() == 0 {
				t.Fatal("other output stream was not attempted")
			}
			if !tc.stderr && other.Len() != 0 {
				t.Fatal("failed ordinary report duplicated to stderr", &other)
			}
		})
	}
}
