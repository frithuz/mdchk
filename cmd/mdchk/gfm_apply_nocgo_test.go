package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCompiledGFMNoCGOApply(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", strings.ReplaceAll(md047Configuration(true, true), "flavor: commonmark\n", ""))
	putConfigurationFile(t, root, "doc.md", gfmConsumerSource)
	verify := unchangedFixFixture(t, root)
	preview := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
	if preview.Status != 0 || len(preview.Result.Changes) != 1 {
		t.Fatal(preview)
	}
	verify()
	got := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
	got.Result = validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
	if got.Status != 0 || len(got.Result.Changes) != 1 || got.Result.Changes[0].Disposition != "written" || strings.Contains(got.Stdout, "unsupported") {
		t.Fatal(got)
	}
	if saved := readFixture(t, filepath.Join(root, "doc.md")); saved != gfmConsumerSource+"\n" {
		t.Fatalf("saved %q", saved)
	}
}
