//go:build unix

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExecutableClosedOutputPipe(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "mdchk")
	//nolint:gosec // Fixed Go tool and test-owned temporary build destination.
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	for _, tc := range []struct {
		args   []string
		stderr bool
	}{
		{[]string{"--help"}, false}, {[]string{"lint", "--bad", "--format=text"}, false}, {[]string{"lint", "--bad", "--format=json"}, false}, {[]string{"lint", "--bad", "--format=vscode"}, true},
	} {
		args := tc.args
		t.Run(args[len(args)-1], func(t *testing.T) {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := writer.Close(); err != nil {
					t.Error(err)
				}
			}()
			// Closing the reader before process start makes EPIPE deterministic and
			// tests the inherited real stdout descriptor, not an injected io.Writer.
			//nolint:gosec // Execute only the test-owned binary with fixed arguments.
			cmd := exec.Command(binary, args...)
			var stderr bytes.Buffer
			if tc.stderr {
				cmd.Stderr = writer
				cmd.Stdout = &stderr
			} else {
				cmd.Stdout = writer
				cmd.Stderr = &stderr
			}
			err = cmd.Run()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 13 {
				t.Fatalf("closed output pipe: err=%v, stderr=%q; want exit13", err, &stderr)
			}
			if stderr.Len() != 0 {
				t.Fatalf("unexpected report on other stream: %q", &stderr)
			}
		})
	}
}
