package files

import (
	"strings"
	"testing"
)

// Hosts outside linux, darwin and windows refuse apply with an explicit,
// testable reason rather than a missing adapter. The message is a function of
// GOOS, so it is exercised on every host.
func TestUnsupportedPlatformRefusalNamesHostAndSupportedSet(t *testing.T) {
	err := unsupportedPlatformError("plan9")
	for _, want := range []string{"unsupported on plan9", "linux", "darwin", "windows"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q lacks %q", err, want)
		}
	}
}
