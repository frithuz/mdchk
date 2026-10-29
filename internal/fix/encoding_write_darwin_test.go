//go:build darwin && arm64 && cgo

package fix_test

import (
	"context"
	"github.com/frithuz/mdchk/internal/files"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules"
	"os"
	"path/filepath"
	"testing"
)

func TestRetainedOriginalEncodingBlocksNativeDependencyProof(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(root, "bad.md"), filepath.Join(root, "dependent.md"), filepath.Join(root, "independent.md")}
	for i, name := range paths {
		data := []byte("A * � *")
		if i == 0 {
			data = []byte{'x', 0xff}
		}
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	plan, v, b := encodingProduction(t)
	selected, err := files.Select(files.Options{Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	native, acquired, _, err := files.AcquireWrite(context.Background(), selected, files.WriteOptions{AcquireOptions: files.AcquireOptions{Budget: b}})
	if err != nil || len(acquired.Errors) != 0 {
		t.Fatal(acquired, err)
	}
	defer func() { _ = native.Close() }()
	var targets []fix.Target
	var bad parser.Source
	for _, doc := range acquired.Documents {
		targets = append(targets, fix.Target{Path: doc.Path, Source: doc.Source, Plan: plan, Produce: rules.Corrections})
		if doc.Path == paths[0] {
			bad = doc.Source
		}
	}
	for i := range targets {
		if targets[i].Path == paths[1] {
			targets[i].ResourceDependencies = []string{paths[0]}
		}
	}
	out, err := v.Validate(context.Background(), targets, fix.ValidationOptions{Budget: b, Resources: []fix.ResourceObservation{{Key: paths[0], State: fix.ResourceContent, Source: bad}}})
	if err != nil || out.Complete() {
		t.Fatal(err)
	}
	// Detached mutations grant no authority to an encoding-blocked component.
	detached, _ := out.File(paths[1])
	detached.Reasons = nil
	detached.Candidate = parser.NewSource([]byte("forged\n"))
	for _, name := range paths[:2] {
		publication := out.Publish(context.Background(), native, name)
		if publication.Err == nil || publication.Published || publication.NoChange || publication.Saved != (parser.Source{}) {
			t.Fatal("private publication proof escaped", publication)
		}
	}
	independent := out.Publish(context.Background(), native, paths[2])
	if independent.Err != nil || !independent.Published || independent.Saved.Len() != 10 {
		t.Fatal(independent)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil || string(data) != string([]byte{'x', 0xff}) {
		t.Fatal(data, err)
	}
}
