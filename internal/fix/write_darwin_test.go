//go:build darwin && arm64 && cgo

package fix_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/files"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules"
)

func TestRetainedProductionApprovalNativePublication(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(root, "a.md"), filepath.Join(root, "b.md")}
	for _, p := range paths {
		if err := os.WriteFile(p, []byte("hello"), 0640); err != nil { //nolint:gosec // Synthetic fixture deliberately exercises preservation of group-readable mode.
			t.Fatal(err)
		}
	}
	selected, err := files.Select(files.Options{Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	native, acquired, _, err := files.AcquireWrite(context.Background(), selected, files.WriteOptions{AcquireOptions: files.AcquireOptions{Budget: budget}})
	if err != nil || len(acquired.Errors) > 0 {
		t.Fatalf("acquire %v %+v", err, acquired)
	}
	defer func() { _ = native.Close() }()
	registry, err := engine.NewCatalogRegistry(rules.Registrations())
	if err != nil {
		t.Fatal(err)
	}
	settings := engine.Settings{}
	for _, m := range engine.Catalog() {
		enabled := m.ID == "MD047"
		settings[m.ID] = engine.RuleSetting{Enabled: &enabled}
	}
	selection, err := registry.SelectRules(settings, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selection.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := fix.NewValidator(registry, rules.CorrectionCheckers())
	if err != nil {
		t.Fatal(err)
	}
	targets := make([]fix.Target, len(acquired.Documents))
	for i, d := range acquired.Documents {
		targets[i] = fix.Target{Path: d.Path, Source: d.Source, Plan: plan, Produce: rules.Corrections}
	}
	targets[1].Dependencies = []string{paths[0]}
	approved, err := validator.Validate(context.Background(), targets, fix.ValidationOptions{Budget: budget})
	if err != nil || !approved.Complete() {
		t.Fatalf("validate %v", err)
	}
	detached, _ := approved.File(paths[0])
	detached.Candidate = parser.NewSource([]byte("forged"))
	detached.Path = paths[1]
	ambiguousTarget := targets[0]
	ambiguousTarget.ResourceDependencies = []string{paths[0]}
	ambiguous, err := validator.Validate(context.Background(), []fix.Target{ambiguousTarget}, fix.ValidationOptions{Budget: budget, Resources: []fix.ResourceObservation{{Key: paths[0], State: fix.ResourceContent, Source: parser.NewSource([]byte("foreign resource"))}}})
	if err != nil || !ambiguous.Complete() {
		t.Fatalf("ambiguous semantic fixture %v", err)
	}
	refused := ambiguous.PublishPrototype(context.Background(), native, paths[0])
	if refused.Err == nil || refused.Published || refused.Saved != (parser.Source{}) {
		t.Fatalf("distinct resource Source overwritten by target binding: %+v", refused)
	}
	first := approved.PublishPrototype(context.Background(), native, paths[0])
	if first.Err != nil || !first.Published {
		t.Fatalf("first %+v", first)
	}
	second := approved.PublishPrototype(context.Background(), native, paths[1])
	if second.Err != nil || !second.Published {
		t.Fatalf("own saved dependency rejected %+v", second)
	}
	for _, p := range paths {
		raw, e := os.ReadFile(p) //nolint:gosec // The path names an independently created synthetic test fixture.
		if e != nil || string(raw) != "hello\n" {
			t.Fatalf("retained bytes %q %v", raw, e)
		}
	}
}

func TestUnchangedProductionResultNeverClaimsSaved(t *testing.T) {
	for _, text := range []string{"", "hello\n"} {
		for _, scenario := range []string{"clean", "external-bytes", "identical-replacement", "closed", "foreign-acquisition", "origin-canceled", "caller-canceled", "exhausted"} {
			t.Run(fmt.Sprintf("bytes%d/%s", len(text), scenario), func(t *testing.T) {
				root, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(root, "doc.md")
				if err := os.WriteFile(path, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
				selected, err := files.Select(files.Options{Paths: []string{path}})
				if err != nil {
					t.Fatal(err)
				}
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				origin, cancelOrigin := context.WithCancel(context.Background())
				defer cancelOrigin()
				native, a, _, err := files.AcquireWrite(origin, selected, files.WriteOptions{AcquireOptions: files.AcquireOptions{Budget: budget}})
				if err != nil || len(a.Documents) != 1 {
					t.Fatalf("acquire %v %+v", err, a)
				}
				originalNative := native
				defer func() { _ = originalNative.Close() }()
				registry, err := engine.NewCatalogRegistry(rules.Registrations())
				if err != nil {
					t.Fatal(err)
				}
				settings := engine.Settings{}
				for _, m := range engine.Catalog() {
					enabled := m.ID == "MD047"
					settings[m.ID] = engine.RuleSetting{Enabled: &enabled}
				}
				selection, err := registry.SelectRules(settings, engine.ValidationProviders{})
				if err != nil {
					t.Fatal(err)
				}
				plan, err := selection.Plan(engine.DocumentContext{Flavor: "commonmark"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				validator, err := fix.NewValidator(registry, rules.CorrectionCheckers())
				if err != nil {
					t.Fatal(err)
				}
				approved, err := validator.Validate(origin, []fix.Target{{Path: path, Source: a.Documents[0].Source, Plan: plan, Produce: rules.Corrections}}, fix.ValidationOptions{Budget: budget})
				if err != nil || !approved.Complete() {
					t.Fatalf("validation %v", err)
				}
				caller := context.Background()
				switch scenario {
				case "external-bytes":
					if err := os.WriteFile(path, []byte("externally changed"), 0600); err != nil {
						t.Fatal(err)
					}
				case "identical-replacement":
					replacement := path + ".replacement"
					if err := os.WriteFile(replacement, []byte(text), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(replacement, path); err != nil {
						t.Fatal(err)
					}
				case "closed":
					if err := native.Close(); err != nil {
						t.Fatal(err)
					}
				case "foreign-acquisition":
					var fresh *files.Acquisition
					native, fresh, _, err = files.AcquireWrite(origin, selected, files.WriteOptions{AcquireOptions: files.AcquireOptions{Budget: budget}})
					if err != nil || len(fresh.Documents) != 1 {
						t.Fatalf("foreign acquire %v", err)
					}
					defer func() { _ = native.Close() }()
				case "origin-canceled":
					cancelOrigin()
				case "caller-canceled":
					ctx, cancel := context.WithCancel(context.Background())
					cancel()
					caller = ctx
				case "exhausted":
					_ = budget.ChargeWork(budget.Limits().WorkUnits + 1)
				}
				beforeBytes, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				before, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				out := approved.PublishPrototype(caller, native, path)
				if out.Published || out.Saved != (parser.Source{}) {
					t.Fatalf("unconfirmed unchanged source reported saved: %+v", out)
				}
				afterBytes, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(beforeBytes, afterBytes) {
					t.Fatal("no-change modified bytes", err)
				}
				after, err := os.Stat(path)
				if err != nil || !os.SameFile(before, after) {
					t.Fatal("no-change replaced inode", err)
				}
				entries, err := os.ReadDir(root)
				if err != nil || len(entries) != 1 {
					t.Fatalf("no-change created artifacts: %v %v", entries, err)
				}
			})
		}
	}
}
