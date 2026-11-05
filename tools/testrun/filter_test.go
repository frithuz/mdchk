package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The testdata streams are recorded go test -json runs of a scratch module.
// Each package also ran a TestNoise test that logs "noise" and passes.
func TestFilter(t *testing.T) {
	for _, tc := range []struct {
		name, stream string
		code         int
		want, absent []string
		summary      string
	}{
		{
			name: "all-pass", stream: "pass.jsonl",
			absent:  []string{"noise", "inner noise", "no test files", "ok  ", "PASS"},
			summary: "ok: 3 tests passed, 0 skipped in 2 packages",
		},
		{
			name: "skip", stream: "skip.jsonl",
			absent:  []string{"noise", "skipped reason", "SKIP", "no test files"},
			summary: "ok: 1 test passed, 1 skipped in 2 packages",
		},
		{
			name: "failing-test-and-subtest", stream: "fail.jsonl", code: 1,
			want: []string{
				"--- FAIL: TestBoom", "f_test.go:4: boom",
				"--- FAIL: TestParent/bad", "f_test.go:7: sub boom", "--- FAIL: TestParent ",
				"FAIL\texample.com/m/fail",
			},
			absent:  []string{"noise", "TestParent/good", "no test files"},
			summary: "FAIL: 3 tests failed, 2 passed, 0 skipped; 1 of 2 packages failed",
		},
		{
			name: "build-failure", stream: "build.jsonl", code: 1,
			want: []string{
				"# example.com/m/build [example.com/m/build.test]",
				`cannot use "s" (untyped string constant) as int value`,
				"FAIL\texample.com/m/build [build failed]",
			},
			absent:  []string{"no test files"},
			summary: "FAIL: 0 tests failed, 0 passed, 0 skipped; 1 of 2 packages failed",
		},
		{
			name: "panic", stream: "panic.jsonl", code: 1,
			want:    []string{"--- FAIL: TestPanic", "panic: kaboom", "p_test.go:4", "FAIL\texample.com/m/panic"},
			absent:  []string{"noise", "no test files"},
			summary: "FAIL: 1 test failed, 1 passed, 0 skipped; 1 of 2 packages failed",
		},
		{
			name: "timeout", stream: "timeout.jsonl", code: 1,
			want:    []string{"=== RUN   TestSleep", "panic: test timed out after 1s", "TestSleep (1s)", "FAIL\texample.com/m/timeout"},
			absent:  []string{"noise"},
			summary: "FAIL: 0 tests failed, 1 passed, 0 skipped; 1 of 1 package failed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input, err := os.Open(filepath.Join("testdata", tc.stream))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = input.Close() }()
			var output strings.Builder
			result, err := filter(input, &output)
			if err != nil {
				t.Fatal(err)
			}
			text := output.String()
			if len(tc.want) == 0 && text != "" {
				t.Errorf("passing stream wrote output:\n%s", text)
			}
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("missing %q in:\n%s", want, text)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(text, absent) {
					t.Errorf("unexpected %q in:\n%s", absent, text)
				}
			}
			if got := result.summary(tc.code); got != tc.summary {
				t.Errorf("summary = %q, want %q", got, tc.summary)
			}
		})
	}
}

func TestFilterPassesNonJSONLines(t *testing.T) {
	input := "go: downloading example.com/dependency v1.0.0\n" +
		`{"Action":"start","Package":"example.com/m"}` + "\n" +
		`{"Action":"skip","Package":"example.com/m"}` + "\n" +
		"no trailing newline"
	var output strings.Builder
	if _, err := filter(strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	want := "go: downloading example.com/dependency v1.0.0\nno trailing newline\n"
	if output.String() != want {
		t.Errorf("output = %q, want %q", output.String(), want)
	}
}

func TestSummaryReportsExitStatus(t *testing.T) {
	// go test can fail before any package reports, as for a bad pattern.
	if got := (report{}).summary(1); !strings.HasPrefix(got, "FAIL:") {
		t.Errorf("summary = %q, want a failure", got)
	}
}

func TestDebug(t *testing.T) {
	for value, want := range map[string]bool{"": false, "0": false, "1": true, "true": true, "false": true} {
		if got := debug(value); got != want {
			t.Errorf("debug(%q) = %v, want %v", value, got, want)
		}
	}
}
