//go:build !darwin || !arm64 || !cgo

package main

import (
	"strings"
	"testing"
)

func TestCompiledGFMUnsupportedApply(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", strings.ReplaceAll(md047Configuration(true, true), "flavor: commonmark\n", ""))
	putConfigurationFile(t, root, "doc.md", gfmConsumerSource)
	verify := unchangedFixFixture(t, root)
	got := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
	got.Result = validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
	if got.Status != 13 || len(got.Result.Changes) != 0 || !strings.Contains(got.Stdout, "unsupported") {
		t.Fatal(got)
	}
	verify()
	preview := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
	if preview.Status != 0 || len(preview.Result.Changes) != 1 {
		t.Fatal(preview)
	}
	verify()
}
