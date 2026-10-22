//go:build darwin && arm64 && cgo

package files

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestNativeFailedTargetsRetainIndependentRequiredAuthority(t *testing.T) {
	for _, mode := range []string{"publish", "config-changed", "absence-appeared", "unknown-owner", "unknown-owner-with-failure", "missing-resource", "resource-owner"} {
		t.Run(mode, func(t *testing.T) {
			root, a := nativeFixture(t)
			b, c, d := filepath.Join(root, "b.md"), filepath.Join(root, "c.md"), filepath.Join(root, "0-blocked.md")
			for path, content := range map[string]string{b: "blocked", c: "independent", d: "also blocked"} {
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, path := range []string{b, d} {
				if err := os.Chmod(path, 0444); err != nil { //nolint:gosec // Read-only synthetic targets exercise native refusal.
					t.Fatal(err)
				}
			}
			before, err := os.Stat(b)
			if err != nil {
				t.Fatal(err)
			}
			config, absent := filepath.Join(root, "config.yaml"), filepath.Join(root, "missing.yaml")
			if err := os.WriteFile(config, []byte("configuration"), 0600); err != nil {
				t.Fatal(err)
			}
			selected, err := Select(Options{Paths: []string{b, a, c, d}})
			if err != nil {
				t.Fatal(err)
			}
			resources, err := Select(Options{Paths: []string{config}})
			if err != nil {
				t.Fatal(err)
			}
			required := map[string][]string{a: {config, absent}, b: {config, absent}, c: {config, absent}, d: {config, absent}}
			switch mode {
			case "unknown-owner":
				required = map[string][]string{filepath.Join(root, "unknown.md"): nil}
			case "unknown-owner-with-failure":
				required[filepath.Join(root, "unknown.md")] = nil
			case "missing-resource":
				required[a] = []string{b}
			case "resource-owner":
				required[config] = nil
			}
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			set, acquired, _, err := AcquireWrite(context.Background(), selected, WriteOptions{AcquireOptions: AcquireOptions{Budget: budget}, Resources: resources, Absences: []string{absent}, Required: required})
			if strings.HasPrefix(mode, "unknown-owner") || mode == "resource-owner" || mode == "missing-resource" {
				want := "unknown dependency owner"
				if mode == "missing-resource" {
					want = "unobserved dependency"
				}
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("authority guard %s: %v", mode, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = set.Close() }()
			if len(acquired.Documents) != 2 || len(acquired.Errors) != 2 || len(acquired.Interruptions) != 0 {
				t.Fatalf("partition %+v", acquired)
			}
			failures := map[string]bool{}
			for _, failure := range acquired.Errors {
				if failure.Err == nil {
					t.Fatal("untyped failure", failure)
				}
				failures[failure.Path] = true
			}
			if !failures[b] || !failures[d] {
				t.Fatal("lost acquisition failures", acquired.Errors)
			}
			if out := set.Publish(context.Background(), b, parser.NewSource([]byte("blocked")), parser.NewSource([]byte("forged\n")), nil); out.Err == nil || out.Published {
				t.Fatal("failed target acquired foreign authority", out)
			}
			switch mode {
			case "config-changed":
				if err := os.WriteFile(config, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "absence-appeared":
				if err := os.WriteFile(absent, []byte("appeared"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, doc := range acquired.Documents {
				candidate := parser.NewSource(append(doc.Source.Bytes(), '\n'))
				out := set.Publish(context.Background(), doc.Path, doc.Source, candidate, nil)
				if mode == "publish" {
					if out.Err != nil || !out.Published || out.Saved != candidate {
						t.Fatal(out)
					}
					assertNativeBytes(t, doc.Path, candidate.Bytes())
				} else {
					if out.Err == nil || out.Published {
						t.Fatal("lost required config binding", out)
					}
					assertNativeBytes(t, doc.Path, doc.Source.Bytes())
				}
			}
			after, err := os.Stat(b)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("failed target changed")
			}
			assertNativeBytes(t, b, []byte("blocked"))
			assertNativeBytes(t, d, []byte("also blocked"))
			assertNoStage(t, root)
		})
	}
}
