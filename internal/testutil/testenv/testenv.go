// Package testenv keeps tests independent of Git configuration injected into
// the process environment, such as GIT_CONFIG_COUNT from a sandbox or CI.
//
// mdchk refuses to scan while these overrides are present, so a test that
// does not exercise that refusal must not inherit them. It is for tests only.
package testenv

import (
	"os"
	"strings"
	"testing"
)

// IsGitOverride reports whether key is a Git environment variable that
// redirects or extends Git configuration, repository or work tree discovery.
func IsGitOverride(key string) bool {
	key = strings.ToUpper(key)
	switch key {
	case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_CONFIG", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS":
		return true
	}
	return strings.HasPrefix(key, "GIT_CONFIG_KEY_") || strings.HasPrefix(key, "GIT_CONFIG_VALUE_")
}

// Environ returns the current process environment without Git overrides.
func Environ() []string {
	return Without(os.Environ())
}

// Without returns the KEY=VALUE entries of env that are not Git overrides.
func Without(env []string) []string {
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if !IsGitOverride(key) {
			kept = append(kept, entry)
		}
	}
	return kept
}

// Clear unsets every Git override in the process environment for the rest of
// the test and restores the original values when it finishes. Call it before
// setting any override the test means to check.
func Clear(t testing.TB) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !IsGitOverride(key) {
			continue
		}
		// Setenv records the original value so cleanup restores it; the
		// Unsetenv then makes the variable absent, which Git treats
		// differently from empty.
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}
