package testenv

import (
	"os"
	"slices"
	"testing"
)

func TestWithoutDropsOnlyGitOverrides(t *testing.T) {
	in := []string{
		"PATH=/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/x", "GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=a", "GIT_CONFIG_VALUE_0=b", "GIT_CONFIG_KEY_17=a", "GIT_CONFIG_VALUE_17=b",
		"GIT_CONFIG_PARAMETERS='a'='b'", "GIT_CONFIG=/y", "GIT_DIR=/d", "GIT_WORK_TREE=/w", "GIT_COMMON_DIR=/c", "HOME=/h",
	}
	want := []string{"PATH=/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/x", "GIT_TERMINAL_PROMPT=0", "HOME=/h"}
	if got := Without(in); !slices.Equal(got, want) {
		t.Fatalf("Without() = %q, want %q", got, want)
	}
}

func TestClearUnsetsAndRestores(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_3", "k")
	t.Run("inner", func(t *testing.T) {
		Clear(t)
		for _, key := range []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_3"} {
			if _, ok := os.LookupEnv(key); ok {
				t.Fatalf("%s still set", key)
			}
		}
		t.Setenv("GIT_CONFIG_COUNT", "5")
	})
	if got := os.Getenv("GIT_CONFIG_COUNT"); got != "1" {
		t.Fatalf("GIT_CONFIG_COUNT = %q after cleanup, want 1", got)
	}
	if got := os.Getenv("GIT_CONFIG_KEY_3"); got != "k" {
		t.Fatalf("GIT_CONFIG_KEY_3 = %q after cleanup, want k", got)
	}
}
