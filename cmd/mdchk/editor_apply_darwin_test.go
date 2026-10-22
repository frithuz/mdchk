//go:build darwin && arm64 && cgo

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompiledEditorPartialNativePublication(t *testing.T) {
	// Build-time control performs a genuine stale-source refusal and normal native
	// B publication. C's genuine publication is followed by loss of observation;
	// the explicit uncertainty/owned-name evidence exercises recovery presentation.
	body := `if path=="a.md" { if e:=os.WriteFile(names[path],[]byte("\n\n<!-- 😀x"),0600);e!=nil {panic(e)};return validated.PublishMapped(ctx,native,path,names) }; pub:=validated.PublishMapped(ctx,native,path,names); if path=="c.md" { if e:=os.Remove(names[path]);e!=nil {panic(e)};pub.Published=false;pub.Saved=parser.Source{};pub.Uncertain=true;pub.Err=errors.New("observation lost ##[error] ::group::");pub.Leftovers=[]string{".owned-##[group]::stop-commands::"} }; return pub`
	binary := buildApplyBoundaryFixture(t, body)
	for _, format := range []string{"json", "text", "vscode", "github"} {
		for _, deadline := range []string{"2s", "1ns"} {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
			for _, name := range []string{"a.md", "b.md", "c.md"} {
				putConfigurationFile(t, root, name, "same")
			}
			command := exec.Command(binary, "fix", "a.md", "b.md", "c.md", "--format="+format, "--limit=reporting-time="+deadline) //nolint:gosec // Test-owned compiled binary and fixtures.
			command.Dir = root
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			var failure *exec.ExitError
			if err := command.Run(); !errors.As(err, &failure) || failure.ExitCode() != 13 {
				t.Fatalf("%s %s %v %s %s", format, deadline, err, &stdout, &stderr)
			}
			//nolint:gosec // Read only a test-owned native publication fixture.
			saved, err := os.ReadFile(filepath.Join(root, "b.md"))
			if err != nil || string(saved) != "same\n" {
				t.Fatal("real B publication", string(saved), err)
			}
			//nolint:gosec // Read only a test-owned stale-source fixture.
			a, err := os.ReadFile(filepath.Join(root, "a.md"))
			if err != nil || string(a) != "\n\n<!-- 😀x" {
				t.Fatal("real stale A", string(a), err)
			}
			if _, err = os.Stat(filepath.Join(root, "c.md")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("C observation not removed", err)
			}
			if format == "vscode" || format == "github" {
				for _, evidence := range []string{"outcome a.md refused", "outcome b.md written", "outcome c.md unknown", ".owned-"} {
					if !strings.Contains(stderr.String(), evidence) {
						t.Fatalf("lost recovery %s %s %s %s", format, deadline, evidence, &stderr)
					}
				}
				if deadline == "2s" {
					if format == "vscode" && !strings.Contains(stdout.String(), "a.md:3:8: error: MD047:") {
						t.Fatal("lost saved coordinates", stdout.String())
					}
					if format == "github" && !strings.Contains(stdout.String(), "file=a.md,line=3,title=MD047") {
						t.Fatal("lost saved location", stdout.String())
					}
				}
				if strings.Contains(stdout.String(), "c.md:1:") || strings.Contains(stdout.String(), "file=c.md,line=") {
					t.Fatal("invented candidate for unknown saved view", stdout.String())
				}
				if format == "github" && (strings.Contains(stderr.String(), "##[") || strings.Contains(stderr.String(), "::")) {
					t.Fatal("workflow command in human recovery", stderr.String())
				}
			}
		}
	}
}
