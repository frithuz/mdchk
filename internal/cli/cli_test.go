package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDispatch(t *testing.T) {
	validProject(t)
	for _, tc := range []struct {
		args   []string
		status int
		stdout string
	}{
		{nil, 0, "Usage: mdchk"}, {[]string{"--help"}, 0, "Commands:"}, {[]string{"lint", "--help"}, 0, "Usage: mdchk lint"}, {[]string{"--help", "lint"}, 0, "Usage: mdchk lint"}, {[]string{"--version"}, 0, "mdchk test-version"},
		{[]string{"lint", "--version"}, 0, "mdchk test-version"}, {[]string{"lint"}, 11, ""},
		{[]string{"fix"}, 11, ""}, {[]string{"inspect"}, 11, ""}, {[]string{"config"}, 11, ""}, {[]string{"mcp"}, 11, ""}, {[]string{"wat"}, 11, ""}, {[]string{"--wat"}, 11, ""},
		{[]string{"lint", "a.md", "--wat"}, 11, ""}, {[]string{"lint", "--format"}, 11, ""}, {[]string{"lint", "--format", "--help"}, 11, ""},
		{[]string{"lint", "--config="}, 11, ""}, {[]string{"lint", "--limit="}, 11, ""}, {[]string{"lint", "--format", "yaml"}, 11, ""},
		{[]string{"lint", "--", "--help", "--version"}, 13, ""},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out, errout bytes.Buffer
			got := Run(tc.args, &out, &errout, "test-version")
			if got != tc.status {
				t.Fatalf("status=%d; want %d; stderr=%s", got, tc.status, &errout)
			}
			if tc.stdout != "" && !strings.Contains(out.String(), tc.stdout) {
				t.Fatalf("stdout=%q; want %q", &out, tc.stdout)
			}
			if tc.status != 0 && (out.Len() == 0 || errout.Len() != 0) {
				t.Fatalf("failure did not write sole stdout report: %q", &out)
			}
			if tc.status == 15 && !strings.Contains(out.String(), "coverage") {
				t.Fatalf("missing incomplete explanation: %q", &errout)
			}
		})
	}
}

func TestLintOptions(t *testing.T) {
	root := validProject(t)
	put(t, filepath.Join(root, "target.md"), "ok")
	got, err := parseLint([]string{"--config", "missing.yaml", "one.md", "--format=json", "two.md", "--limit", "operation-time=120s", "--limit=target-files=20", "--", "--draft.md", "--help"})
	if err != nil {
		t.Fatal(err)
	}
	if got.config != "missing.yaml" || got.format != "json" || strings.Join(got.paths, ",") != "one.md,two.md,--draft.md,--help" || got.help || got.limits["target-files"] != 20 {
		t.Fatalf("wrong options: %+v", got)
	}
	for _, format := range []string{"text", "json", "vscode", "github"} {
		var out, errout bytes.Buffer
		want := 15
		if format == "vscode" || format == "github" {
			want = 11
		}
		if got := Run([]string{"lint", "--format", format}, &out, &errout, "dev"); got != want || out.Len() == 0 || errout.Len() != 0 {
			t.Fatalf("format %s: status=%d stdout=%q", format, got, &out)
		}
	}
}

func TestMetadataDoesNotAccessProject(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, ".mdchk.yaml")
	if err := os.WriteFile(config, []byte("invalid: ["), 0o000); err != nil {
		t.Fatal(err)
	}
	// A directory config and missing target also fail if any future dispatcher
	// accidentally starts project loading before servicing metadata.
	for _, path := range []string{config, dir, filepath.Join(dir, "missing.yaml")} {
		for _, flag := range []string{"--help", "--version"} {
			var out, errout bytes.Buffer
			status := Run([]string{"lint", "--config", path, filepath.Join(dir, "missing.md"), flag}, &out, &errout, "dev")
			if status != 0 || out.Len() == 0 || errout.Len() != 0 {
				t.Fatalf("metadata accessed project: status=%d out=%q err=%q", status, &out, &errout)
			}
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("output disconnected") }
func TestOutputErrors(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--version"}, {"lint", "--help"}, {"lint", "--version"}, {"lint"}, {"unknown"}} {
		if got := Run(args, failingWriter{}, failingWriter{}, "dev"); got != 13 {
			t.Fatalf("%v: got %d", args, got)
		}
	}
	// A failing unused stream must not change successful metadata output.
	if got := Run([]string{"--help"}, io.Discard, failingWriter{}, "dev"); got != 0 {
		t.Fatal(got)
	}
}

func TestLimitValidation(t *testing.T) {
	valid := [][]string{{"document-bytes=2MiB", "total-content-bytes=16MiB"}, {"operation-time=120s", "queue-time=1m"}, {"target-files=20", "target-files=020"}, {"path-bytes=4096B"}, {"fix-rounds=5"}}
	for _, limits := range valid {
		args := []string{}
		for _, limit := range limits {
			args = append(args, "--limit", limit)
		}
		if _, err := parseLint(args); err != nil {
			t.Fatalf("%v: %v", limits, err)
		}
	}
	for _, limits := range [][]string{{"unknown=1"}, {"target-files=0"}, {"target-files=-1"}, {"target-files=+1"}, {"target-files=1.1"}, {"operation-time=2"}, {"operation-time=0s"}, {"operation-time=999999999999h"}, {"document-bytes=2MB"}, {"document-bytes=999999999999GiB"}, {"target-files=1", "target-files=2"}, {"document-bytes=9MiB"}, {"config-bytes=9MiB"}, {"operation-time=1s"}, {"response-bytes=1KiB"}, {"report-reserve-bytes=1"}, {"fix-rounds=11"}, {"target-files="}, {"target-files"}} {
		args := []string{}
		for _, limit := range limits {
			args = append(args, "--limit", limit)
		}
		if _, err := parseLint(args); err == nil {
			t.Fatalf("accepted invalid limits %v", limits)
		}
	}
}
