package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExecutableStatuses(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "mdchk")
	//nolint:gosec // Fixed Go tool and test-owned temporary build destination.
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	project, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".mdchk.yaml"), []byte("version: 1\nflavor: commonmark\ndirectives: {enabled: false}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args   []string
		status int
	}{{[]string{"--help"}, 0}, {[]string{"--version"}, 0}, {[]string{"lint", "--help"}, 0}, {[]string{"lint"}, 11}, {[]string{"lint", ".mdchk.yaml"}, 15}, {[]string{"unknown"}, 11}, {[]string{"lint", "--format", "--help"}, 11}, {[]string{"lint", "--", "--help"}, 13}} {
		//nolint:gosec // Execute only the binary just built in this test with fixed test arguments.
		command := exec.Command(binary, tc.args...)
		command.Dir = project
		var stdout, stderr bytes.Buffer
		command.Stdout = &stdout
		command.Stderr = &stderr
		err := command.Run()
		status := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal(err)
			}
			status = exit.ExitCode()
		}
		if status != tc.status {
			t.Fatalf("%v: got %d, want %d; stderr=%q", tc.args, status, tc.status, &stderr)
		}
	}
	// Inherit a read-only descriptor as stdout to exercise actual delivery failure
	// without relying on platform-specific devices or SIGPIPE behavior.
	outputPath := filepath.Join(t.TempDir(), "output")
	if err := os.WriteFile(outputPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	//nolint:gosec // Open the test-owned temporary file created above.
	readOnly, err := os.Open(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := readOnly.Close(); err != nil {
			t.Error(err)
		}
	}()
	//nolint:gosec // Execute only the binary just built in this test.
	command := exec.Command(binary, "--help")
	command.Stdout = readOnly
	err = command.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 13 {
		t.Fatalf("output failure: %v", err)
	}

}
