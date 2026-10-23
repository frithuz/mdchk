//go:build darwin && arm64 && cgo

package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompiledGFMNativeSavedBytes(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", strings.ReplaceAll(md047Configuration(true, true), "flavor: commonmark\n", ""))
	putConfigurationFile(t, root, "doc.md", gfmConsumerSource)
	preview := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
	if preview.Status != 0 || len(preview.Result.Changes) != 1 {
		t.Fatal(preview)
	}
	got := runAppliedFix(t, binary, root, "fix", "doc.md", "--format=json")
	if got.Status != 0 || !got.Result.Complete || len(got.Result.Changes) != 1 || got.Result.Files[0].WriteStatus != "written" {
		t.Fatal(got)
	}
	//nolint:gosec // Read back only the test-owned native publication target.
	saved, err := os.ReadFile(filepath.Join(root, "doc.md"))
	if err != nil || string(saved) != gfmConsumerSource+"\n" {
		t.Fatal("wrong saved bytes", err)
	}
	snap := got.Result.Files[0].ResultSnapshot
	if snap == nil || snap.View != "saved" || snap.SHA256 != fmt.Sprintf("%x", sha256.Sum256(saved)) || got.Result.Changes[0].Candidate != preview.Result.Changes[0].Candidate {
		t.Fatal("saved identity", got)
	}
	observed, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
	assertCompiledGFM(t, p, "gfm")
	if !observed.Result.Complete || len(observed.Result.Diagnostics) != 0 {
		t.Fatal(observed)
	}
	second := runAppliedFix(t, binary, root, "fix", "doc.md", "--format=json")
	if second.Status != 0 || len(second.Result.Changes) != 0 || second.Result.Files[0].ResultSnapshot.SHA256 != snap.SHA256 {
		t.Fatal("saved fix not idempotent", second)
	}
}
