package rules

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

// md051NativeResources is a minimal native provider over a temporary root, so
// rule-level tests exercise the real shared resolver rather than a stand-in.
type md051NativeResources struct {
	resolver *resource.FragmentResolver
	view     resource.View
	service  *resource.Service
	root     *resource.FilesystemRoot
}

func (r *md051NativeResources) Fragments(context.Context) (*resource.FragmentResolver, resource.View, error) {
	return r.resolver, r.view, nil
}

func md051Native(t *testing.T, files map[string]string, flavor string, targetID string, source parser.Source) *md051NativeResources {
	t.Helper()
	files = maps.Clone(files)
	files["docs/self.md"] = string(source.Bytes())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	ctx := context.Background()
	root, err := resource.OpenFilesystemRoot(ctx, budget, dir, resource.DefaultFilesystemLimits())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := root.BindTarget(filepath.Join(dir, "docs", "self.md"), targetID, source)
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := resource.NewSettings(parser.ParseOptions{GFM: flavor == "gfm"}, "index.md", "github", "test-1")
	operation, err := resource.NewOperation(ctx, budget, root.Root(), settings, []resource.Target{receipt.Target()})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := root.Reader(operation, receipt)
	if err != nil {
		t.Fatal(err)
	}
	service, err := resource.NewFilesystemService(reader, resource.DefaultServiceLimits())
	if err != nil {
		t.Fatal(err)
	}
	view, err := operation.NewView(resource.Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	normalizer, err := resource.NewNormalizer(view, resource.DefaultPathLimits(), resource.NativeNamePolicy)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := resource.NewFragmentResolver(service, normalizer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close(); _ = root.Close() })
	return &md051NativeResources{resolver, view, service, root}
}

func TestMD051ResolveFragmentOutcomes(t *testing.T) {
	files := map[string]string{"docs/other.md": "# Intro\n", "docs/bad.md": "# Intro \xff\n", "docs/sub/index.md": "# Top\n"}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, row := range []struct {
			destination string
			outcome     resource.FragmentOutcome
		}{
			{"other.md#intro", resource.FragmentFound},
			{"other.md#nope", resource.FragmentAbsentTarget},
			{"other.md#%ZZ", resource.FragmentMalformed},
			{"other.md#", resource.DocumentReference},
			{"gone.md#intro", resource.FragmentInapplicable},
			{"bad.md#intro", resource.FragmentContextUnavailable},
			{"sub/#top", resource.FragmentFound},
			{"sub/#nope", resource.FragmentAbsentTarget},
			{"./self.md#self", resource.FragmentFound},
			{"https://example.test/#a", resource.FragmentSkipped},
		} {
			t.Run(flavor+"/"+row.destination, func(t *testing.T) {
				source := "# Self\n\n[x](" + row.destination + ")\n"
				called := false
				r := registry(t, []engine.Registration{{ID: "MD051", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, err := c.FragmentLinks(ctx)
					if err != nil || len(items.Links) != 1 {
						t.Fatal(items, err)
					}
					resolved, err := c.ResolveFragment(ctx, items.Links[0].Destination, items.Links[0].Span)
					if err != nil || !resolved.Available || resolved.Resolution.Outcome() != row.outcome {
						t.Fatal(resolved.Resolution.Outcome(), err)
					}
					called = true
					return engine.RuleOutcome{}
				}}})
				s := md051Settings()
				on := true
				s["MD051"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"cross_file": true}}
				in, err := engine.NewInput("docs/self.md", []byte(source), s, engine.DocumentContext{Flavor: flavor})
				if err != nil {
					t.Fatal(err)
				}
				provider := md051Native(t, files, flavor, "docs/self.md", in.Source())
				if _, err := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Fragments: provider}); err != nil || !called {
					t.Fatal(err, called)
				}
			})
		}
	}
}

func TestMD051ReferenceOriginsReportWholeLinkThroughResolver(t *testing.T) {
	files := map[string]string{"docs/other.md": "# Intro\n"}
	for _, flavor := range []string{"commonmark", "gfm"} {
		source := "# Self\n\n[z][r] [c][] [s]\n\n[r]: other.md#nope\n[c]: other.md#intro\n[s]: other.md#nope\n"
		s := md051Settings()
		on := true
		s["MD051"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"cross_file": true}}
		in, err := engine.NewInput("docs/self.md", []byte(source), s, engine.DocumentContext{Flavor: flavor})
		if err != nil {
			t.Fatal(err)
		}
		provider := md051Native(t, files, flavor, "docs/self.md", in.Source())
		out, err := registry(t, Registrations()).Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Fragments: provider})
		if err != nil || len(out.Errors)+len(out.Coverage) != 0 {
			t.Fatal(out, err)
		}
		var spans []string
		for _, d := range out.Diagnostics {
			if d.Rule == "MD051" {
				spans = append(spans, source[d.ByteRange.Start:d.ByteRange.End])
			}
		}
		if len(spans) != 2 || spans[0] != "[z][r]" || spans[1] != "[s]" {
			t.Fatal(spans)
		}
	}
}
