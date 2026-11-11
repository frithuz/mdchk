package rules

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestRuleLayout keeps rule code in per-rule packages: no mdNNN Go file at
// the top level, and one mdNNN/ folder for every registered rule.
func TestRuleLayout(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	flat := regexp.MustCompile(`^md\d{3}.*\.go$`)
	dirs := make(map[string]bool)
	for _, entry := range entries {
		switch {
		case entry.IsDir():
			dirs[entry.Name()] = true
		case entry.Type().IsRegular() && flat.MatchString(entry.Name()):
			t.Errorf("%s is a flat rule file; move it into its rule package", entry.Name())
		}
	}
	for _, registration := range Registrations() {
		if !dirs[strings.ToLower(registration.ID)] {
			t.Errorf("rule %s has no %s/ package folder", registration.ID, strings.ToLower(registration.ID))
		}
	}
}
