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
}
