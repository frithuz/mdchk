package main

import (
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

// md051ViewsProject has a final-newline defect, so fix proposes a candidate that
// differs from the original while the cross-file MD051 findings stay put.
func md051ViewsProject(t *testing.T, crossFile string) string {
	root := configurationProject(t)
	conf := strings.Replace(cleanConfiguration(), "MD051: {enabled: false}", "MD051: {cross_file: "+crossFile+"}", 1)
	conf = strings.Replace(conf, "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	putConfigurationFile(t, root, ".mdchk.yaml", conf)
	putConfigurationFile(t, root, "docs/other.md", "# Intro\n")
	putConfigurationFile(t, root, "docs/broken.md", "# Intro \xff\n")
	putConfigurationFile(t, root, "docs/self.md", "# Self\n\n[a](other.md#nope) [b](other.md#intro) [c](./self.md#nope) [d](broken.md#x)")
	return root
}

func md051Facts(out result.Result) (findings, coverage []string) {
	for _, d := range out.Diagnostics {
		if d.Rule == "MD051" {
			findings = append(findings, d.Snapshot.View+":"+d.Message)
		}
	}
	for _, c := range out.Coverage {
		if c.Rule == "MD051" && c.Range != nil {
			coverage = append(coverage, c.Snapshot.View)
		}
	}
	return findings, coverage
}

// lint, inspect and fix --dry-run agree on MD051 facts; only the snapshot view
// label differs (original, original, candidate). Lookups never change files.
func TestCompiledPlatformMD051ResourceFragmentsInspectAndFixDryRun(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, crossFile := range []string{"true", "false"} {
		t.Run("cross_file="+crossFile, func(t *testing.T) {
			root := md051ViewsProject(t, crossFile)
			before := readinessState(t, root)
			lint := md014NativeJSON(t, []byte(md014NativeRun(t, binary, root, "lint", "docs/self.md", "--format=json").Stdout), "lint")
			lintFindings, lintCoverage := md051Facts(lint)
			wantFindings, wantCoverage := 2, 1
			if crossFile == "false" {
				// Only the self-path spelling is resolved without cross-file access.
				wantFindings, wantCoverage = 1, 0
			}
			if len(lintFindings) != wantFindings || len(lintCoverage) != wantCoverage {
				t.Fatal(lintFindings, lintCoverage)
			}
			seen, _ := runInspection(t, binary, root, root, "inspect", "docs/self.md", "--format=json")
			inspectFindings, inspectCoverage := md051Facts(seen.Result)
			if len(inspectFindings) != wantFindings || len(inspectCoverage) != wantCoverage {
				t.Fatal(inspectFindings, inspectCoverage)
			}
			for _, f := range inspectFindings {
				if !strings.HasPrefix(f, "original:") {
					t.Fatal(f)
				}
			}
			dry := md014NativeRun(t, binary, root, "fix", "--dry-run", "docs/self.md", "--format=json")
			fixed := md014NativeJSON(t, []byte(dry.Stdout), "fix")
			fixFindings, fixCoverage := md051Facts(fixed)
			if len(fixFindings) != wantFindings || len(fixCoverage) != wantCoverage || len(fixed.Changes) != 1 {
				t.Fatal(fixFindings, fixCoverage, fixed.Changes, dry.Stdout)
			}
			for _, f := range fixFindings {
				if !strings.HasPrefix(f, "candidate:") {
					t.Fatal(f)
				}
			}
			for _, c := range fixed.Coverage {
				if c.Rule == "MD051" && c.Snapshot.View != "candidate" {
					t.Fatal(c)
				}
			}
			readinessAssertEqual(t, "inspect and fix --dry-run are read-only", before, readinessState(t, root))
		})
	}
}
