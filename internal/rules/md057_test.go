package rules

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
)

type md057Located struct {
	Bytes, Start, End [2]int
	Expected, Message string
}

type md057Entry struct {
	Path, Kind, Content, Target string
}

type md057Literal struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Frontmatter  string
	Options      struct {
		CheckImages bool `json:"check_images"`
	}
	IndexFile string `json:"index_file"`
	Layout    []md057Entry
	Findings  []md057Located
	Coverage  []md057Located
}

func md057Literals(t *testing.T) []md057Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md057.json")
	if e != nil {
		t.Fatal(e)
	}
	var v struct{ Cases []md057Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) == 0 {
		t.Fatal(e)
	}
	return v.Cases
}

func md057Settings(checkImages bool) engine.Settings {
	s := settings()
	for id := range s {
		off := false
		s[id] = engine.RuleSetting{Enabled: &off}
	}
	on := true
	s["MD057"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"check_images": checkImages}}
	return s
}

// md057Resources is a provider over the real shared resolver that counts how
// often a rule asks for resource context.
type md057Resources struct {
	resolver *resource.FragmentResolver
	view     resource.View
	err      error
	calls    int
}

func (r *md057Resources) Fragments(context.Context) (*resource.FragmentResolver, resource.View, error) {
	r.calls++
	return r.resolver, r.view, r.err
}

// md057Native lays out a temporary native root with the selected document at
// docs/self.md. Directories, files and symbolic links come from the layout.
func md057Native(t testing.TB, layout []md057Entry, flavor, index string, source parser.Source) *md057Resources {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	layout = append([]md057Entry{{Path: "docs/self.md", Kind: "file", Content: string(source.Bytes())}}, layout...)
	for _, entry := range layout {
		path := filepath.Join(dir, filepath.FromSlash(entry.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		switch entry.Kind {
		case "file":
			err = os.WriteFile(path, []byte(entry.Content), 0o600)
		case "directory":
			err = os.Mkdir(path, 0o700)
		case "symlink":
			if runtime.GOOS == "windows" {
				t.Skip("symbolic link creation needs privileges")
			}
			err = os.Symlink(entry.Target, path)
		default:
			t.Fatal(entry)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if index == "" {
		index = "index.md"
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	ctx := context.Background()
	root, err := resource.OpenFilesystemRoot(ctx, budget, dir, resource.DefaultFilesystemLimits())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := root.BindTarget(filepath.Join(dir, "docs", "self.md"), "docs/self.md", source)
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := resource.NewSettings(parser.ParseOptions{GFM: flavor == "gfm"}, index, "github", "test-1")
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
	t.Cleanup(func() { _ = service.Close(); _ = root.Close() })
	return md057View(t, service, operation, nil)
}

func md057View(t testing.TB, service *resource.Service, operation *resource.Operation, represent resource.NamePolicy) *md057Resources {
	t.Helper()
	view, err := operation.NewView(resource.Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	if represent == nil && operation.Root().Kind() != resource.SuppliedRoot {
		represent = resource.NativeNamePolicy
	}
	normalizer, err := resource.NewNormalizer(view, resource.DefaultPathLimits(), represent)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := resource.NewFragmentResolver(service, normalizer)
	if err != nil {
		t.Fatal(err)
	}
	return &md057Resources{resolver: resolver, view: view}
}

// md057Supplied builds a supplied standalone inventory over the existing
// internal/resource seams, for partial-map and unknown-index coverage.
func md057Supplied(t *testing.T, raw string, source parser.Source) *md057Resources {
	t.Helper()
	root, err := resource.NewRoot(resource.SuppliedRoot)
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := resource.NewSettings(parser.ParseOptions{}, "index.md", "github", "test-1")
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	key, err := resource.NewKey("docs/self.md")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	operation, err := resource.NewOperation(ctx, budget, root, settings, []resource.Target{{ID: "docs/self.md", Source: source, ResourceKey: key}})
	if err != nil {
		t.Fatal(err)
	}
	input, err := resource.DecodeSupplied(ctx, budget, resource.DefaultInputLimits(), []byte(raw), []resource.SuppliedFile{{Key: key, Source: source}})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := resource.NewSuppliedReader(ctx, operation, input, resource.DefaultInputLimits())
	if err != nil {
		t.Fatal(err)
	}
	service, err := resource.NewSuppliedService(reader, resource.DefaultServiceLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	return md057View(t, service, operation, nil)
}

func md057Lint(t *testing.T, source string, s engine.Settings, dc engine.DocumentContext, provider func(parser.Source) *md057Resources) (result.Result, *md057Resources) {
	t.Helper()
	in, err := engine.NewInput("docs/self.md", []byte(source), s, dc)
	if err != nil {
		t.Fatal(err)
	}
	options := engine.RunOptions{}
	var resources *md057Resources
	if provider != nil {
		resources = provider(in.Source())
		options.Fragments = resources
	}
	out, err := registry(t, Registrations()).Lint(context.Background(), []engine.Input{in}, options)
	if err != nil || string(in.Source().Bytes()) != source || len(out.Changes)+len(out.Documents) != 0 {
		t.Fatal(out, err)
	}
	return out, resources
}

func TestMD057AnalysisFixtures(t *testing.T) {
	for _, tc := range md057Literals(t) {
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
					t.Fatal("source identity")
				}
				dc := engine.DocumentContext{Flavor: flavor, IndexFile: tc.IndexFile}
				if tc.Frontmatter != "" {
					var e error
					dc.Frontmatter, e = parser.NewFrontmatterOptions(true, []string{tc.Frontmatter})
					if e != nil {
						t.Fatal(e)
					}
				}
				selection, e := registry(t, Registrations()).SelectRules(md057Settings(tc.Options.CheckImages), engine.ValidationProviders{})
				if e != nil {
					t.Fatal(e)
				}
				plan, e := selection.Plan(dc, nil)
				if e != nil {
					t.Fatal(e)
				}
				original := []byte(tc.Source)
				input, e := plan.NewInput("docs/self.md", original)
				if e != nil {
					t.Fatal(e)
				}
				var previous []result.Diagnostic
				for pass := 0; pass < 2; pass++ {
					resources := md057Native(t, tc.Layout, flavor, tc.IndexFile, input.Source())
					out, trace, e := plan.Inspect(context.Background(), input, engine.RunOptions{Fragments: resources}, engine.InspectionOptions{})
					if e != nil || out.Complete != (len(tc.Coverage) == 0) || len(out.Errors)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes)+len(out.SuppressedDiagnostics) != 0 || len(out.Diagnostics) != len(tc.Findings) || len(out.Coverage) != len(tc.Coverage) || string(original) != tc.Source || string(input.Source().Bytes()) != tc.Source {
						t.Fatal(out, e, tc.Findings)
					}
					if len(out.Files) != 1 || out.Files[0].WriteStatus != "not-requested" {
						t.Fatal(out.Files)
					}
					if pass == 1 && !reflect.DeepEqual(previous, out.Diagnostics) {
						t.Fatal("non-deterministic findings")
					}
					previous = out.Diagnostics
					snapshot := result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}
					for i, d := range out.Diagnostics {
						w := tc.Findings[i]
						if d.Rule != "MD057" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range != (result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}) || d.Snapshot != snapshot || tc.Source[w.Bytes[0]:w.Bytes[1]] != w.Expected {
							t.Fatal(d, w)
						}
					}
					for i, c := range out.Coverage {
						w := tc.Coverage[i]
						if c.Rule != "MD057" || c.Path != "docs/self.md" || c.Reason != result.ContextUnavailable || c.Range == nil || *c.Range != (result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}) || c.Snapshot == nil || *c.Snapshot != snapshot || tc.Source[w.Bytes[0]:w.Bytes[1]] != w.Expected {
							t.Fatal(c, w)
						}
					}
					found := false
					for _, ev := range trace.Evaluations() {
						if ev.Rule == "MD057" {
							found = true
							// Located resource coverage is projected into the result, not the trace.
							if len(tc.Coverage) == 0 && ev.Active != "complete" {
								t.Fatal(ev)
							}
						}
					}
					if !found {
						t.Fatal("evaluation missing")
					}
				}
			})
		}
	}
}

func TestMD057AvailabilityDefaultsAndOptions(t *testing.T) {
	r := registry(t, Registrations())
	s, e := r.ResolveSettings(nil, engine.ValidationProviders{})
	if e != nil || !s["MD057"].Available || !s["MD057"].Enabled || len(s["MD057"].Options) != 1 || s["MD057"].Options["check_images"] != true {
		t.Fatal(s, e)
	}
	for _, meta := range r.Metadata() {
		if meta.ID == "MD057" && (meta.Fix != engine.NoFix || !meta.RequiresSyntax || len(meta.Options) != 1) {
			t.Fatal(meta)
		}
	}
	for _, option := range []engine.Options{{"index_file": "index.md"}, {"check_images": "yes"}} {
		settings := md057Settings(true)
		on := true
		settings["MD057"] = engine.RuleSetting{Enabled: &on, Options: option}
		if _, e = r.ResolveSettings(settings, engine.ValidationProviders{}); e == nil {
			t.Fatal("invalid option accepted", option)
		}
	}
}

func TestMD057NoLocalDestinationOpensNoRoot(t *testing.T) {
	for _, source := range []string{"", "# Title\n", "[a](https://example.test/a.md) [b](//host/a.md) [c](#top) [d]() [e](?q=1)\n", "`[x](missing.md)`\n"} {
		provider := &md057Resources{err: errors.New("resource root unavailable")}
		out, _ := md057Lint(t, source, md057Settings(true), engine.DocumentContext{Flavor: "gfm"}, func(parser.Source) *md057Resources { return provider })
		if provider.calls != 0 || !out.Complete || len(out.Diagnostics)+len(out.Coverage)+len(out.Errors) != 0 {
			t.Fatal(source, provider.calls, out)
		}
	}
	provider := &md057Resources{}
	out, _ := md057Lint(t, "![x](missing.png)\n", md057Settings(false), engine.DocumentContext{Flavor: "gfm"}, func(parser.Source) *md057Resources { return provider })
	if provider.calls != 0 || !out.Complete || len(out.Coverage) != 0 {
		t.Fatal("disabled image check consulted resources", provider.calls, out)
	}
}

func TestMD057ReducedCoverage(t *testing.T) {
	assertOnlyCoverage := func(t *testing.T, out result.Result, located bool, message string) {
		t.Helper()
		if out.Complete || len(out.Diagnostics)+len(out.Errors) != 0 || len(out.Coverage) != 1 || out.Coverage[0].Rule != "MD057" || out.Coverage[0].Reason != result.ContextUnavailable || (out.Coverage[0].Range != nil) != located || !strings.Contains(out.Coverage[0].Message, message) {
			t.Fatal(out)
		}
	}
	t.Run("no-provider", func(t *testing.T) {
		out, _ := md057Lint(t, "[x](missing.md)\n", md057Settings(true), engine.DocumentContext{Flavor: "gfm"}, nil)
		assertOnlyCoverage(t, out, false, "without resource context")
	})
	t.Run("provider-unavailable", func(t *testing.T) {
		out, provider := md057Lint(t, "[x](missing.md) [y](other.md)\n", md057Settings(true), engine.DocumentContext{Flavor: "gfm"}, func(parser.Source) *md057Resources {
			return &md057Resources{err: errors.New("resource root unavailable")}
		})
		assertOnlyCoverage(t, out, false, "without resource context")
		if provider.calls != 2 {
			t.Fatal(provider.calls)
		}
	})
	t.Run("over-long-path", func(t *testing.T) {
		out, _ := md057Lint(t, "[x]("+strings.Repeat("a", 5000)+".md)\n", md057Settings(true), engine.DocumentContext{Flavor: "gfm"}, func(source parser.Source) *md057Resources {
			return md057Native(t, nil, "gfm", "", source)
		})
		assertOnlyCoverage(t, out, false, "exceeds the path bounds")
	})
	t.Run("unknown-inventory", func(t *testing.T) {
		out, _ := md057Lint(t, "[x](missing.md)\n\n<div>\n<x title=\"\n\n[later](missing.md)", md057Settings(true), engine.DocumentContext{Flavor: "gfm"}, nil)
		assertOnlyCoverage(t, out, false, "inventory is withheld")
	})
	for _, row := range []struct{ name, source string }{
		{"blocked-root-escape", "[x](../../outside.md)\n"},
		{"blocked-absolute-path", "[x](/etc/passwd)\n"},
		{"denied-symlink", "[x](linked.md)\n"},
	} {
		t.Run(row.name, func(t *testing.T) {
			layout := []md057Entry{{Path: "docs/guide.md", Kind: "file", Content: "# Guide\n"}, {Path: "docs/linked.md", Kind: "symlink", Target: "guide.md"}}
			out, _ := md057Lint(t, row.source, md057Settings(true), engine.DocumentContext{Flavor: "gfm"}, func(source parser.Source) *md057Resources {
				return md057Native(t, layout, "gfm", "", source)
			})
			assertOnlyCoverage(t, out, true, "Required resource context is unavailable.")
		})
	}
	t.Run("supplied-partial-inventory", func(t *testing.T) {
		const source = "[a](unlisted.md) [b](dir/) [c](gone.md) [d](listed.md) [e](full/)\n"
		raw := `{"complete":false,"entries":[{"path":"docs/gone.md","kind":"missing"},{"path":"docs/listed.md","kind":"file"},{"path":"docs/dir","kind":"directory"},{"path":"docs/full","kind":"directory"},{"path":"docs/full/index.md","kind":"missing"}]}`
		out, _ := md057Lint(t, source, md057Settings(true), engine.DocumentContext{Flavor: "gfm"}, func(s parser.Source) *md057Resources { return md057Supplied(t, raw, s) })
		var findings, coverage []string
		for _, d := range out.Diagnostics {
			findings = append(findings, source[d.ByteRange.Start:d.ByteRange.End]+": "+d.Message)
		}
		for _, c := range out.Coverage {
			if c.Range == nil || c.Rule != "MD057" {
				t.Fatal(c)
			}
			coverage = append(coverage, fmt.Sprint(c.Range.Start.Column))
		}
		want := []string{"gone.md: Local link target does not exist.", "full/: Directory has no configured index file."}
		// The unlisted name and the directory whose index is not listed stay unknown.
		if out.Complete || !reflect.DeepEqual(findings, want) || !reflect.DeepEqual(coverage, []string{"5", "22"}) || len(out.Errors) != 0 {
			t.Fatal(findings, coverage, out)
		}
	})
}

func TestMD057SharesResourceContextWithMD051(t *testing.T) {
	const source = "# Self\n\n[a](gone.md#intro) [b](other.md#nope)\n"
	layout := []md057Entry{{Path: "docs/other.md", Kind: "file", Content: "# Intro\n"}}
	for _, md057 := range []bool{false, true} {
		s := md057Settings(true)
		on := true
		s["MD051"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"cross_file": true}}
		s["MD057"] = engine.RuleSetting{Enabled: &md057}
		out, provider := md057Lint(t, source, s, engine.DocumentContext{Flavor: "gfm"}, func(src parser.Source) *md057Resources {
			return md057Native(t, layout, "gfm", "", src)
		})
		var got []string
		for _, d := range out.Diagnostics {
			got = append(got, d.Rule+" "+source[d.ByteRange.Start:d.ByteRange.End])
		}
		want := []string{"MD051 other.md#nope"}
		if md057 {
			want = append([]string{"MD057 gone.md#intro"}, want...)
		}
		if !out.Complete || !reflect.DeepEqual(got, want) || len(out.Coverage)+len(out.Errors) != 0 || provider.calls == 0 {
			t.Fatal(got, out)
		}
	}
}

func TestMD057Directives(t *testing.T) {
	for _, allow := range []bool{true, false} {
		s := md057Settings(true)
		on := true
		s["MD057"] = engine.RuleSetting{Enabled: &on, AllowDirectives: &allow, Options: engine.Options{"check_images": true}}
		out, _ := md057Lint(t, "<!-- mdchk:disable MD057 -->\n[x](missing.md)\n", s, engine.DocumentContext{Flavor: "gfm"}, func(source parser.Source) *md057Resources {
			return md057Native(t, nil, "gfm", "", source)
		})
		visible, hidden := 0, 1
		if !allow {
			visible, hidden = 1, 0
		}
		if !out.Complete || len(out.Diagnostics) != visible || len(out.SuppressedDiagnostics) != hidden || len(out.Coverage)+len(out.Errors) != 0 {
			t.Fatal(allow, out)
		}
	}
}

// A link to the resource root passes MD057 without an index lookup, for native
// and supplied (virtual) roots alike. MD051 cross_file keeps its own directory
// semantics: a root fragment resolves the configured index for its anchors, and
// a root without that index is inapplicable to MD051, never a finding.
func TestMD057RootExemptionAcrossRootsAndMD051(t *testing.T) {
	const source = "# Self\n\n[a](../) [b](..) [c](../#top) [d](../#nope) [e](sub/)\n"
	settings := func() engine.Settings {
		s := md057Settings(true)
		on := true
		s["MD051"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"cross_file": true}}
		return s
	}
	collect := func(out result.Result) []string {
		var got []string
		for _, d := range out.Diagnostics {
			got = append(got, d.Rule+" "+source[d.ByteRange.Start:d.ByteRange.End])
		}
		return got
	}
	subIndexMissing := "MD057 sub/"
	for _, row := range []struct {
		name   string
		layout []md057Entry
		want   []string
	}{
		{"native-root-without-index", []md057Entry{{Path: "README.md", Kind: "file", Content: "# Top\n"}, {Path: "docs/sub/README.md", Kind: "file", Content: "# Sub\n"}}, []string{subIndexMissing}},
		{"native-root-with-index", []md057Entry{{Path: "index.md", Kind: "file", Content: "# Top\n"}, {Path: "docs/sub/README.md", Kind: "file", Content: "# Sub\n"}}, []string{"MD051 ../#nope", subIndexMissing}},
	} {
		t.Run(row.name, func(t *testing.T) {
			out, _ := md057Lint(t, source, settings(), engine.DocumentContext{Flavor: "gfm"}, func(s parser.Source) *md057Resources {
				return md057Native(t, row.layout, "gfm", "", s)
			})
			if got := collect(out); !out.Complete || !reflect.DeepEqual(got, row.want) || len(out.Coverage)+len(out.Errors) != 0 {
				t.Fatal(got, out)
			}
		})
	}
	for _, row := range []struct {
		name, raw string
		want      []string
	}{
		{"supplied-root-without-index", `{"complete":true,"entries":[{"path":"docs/sub","kind":"directory"}]}`, []string{subIndexMissing}},
		{"supplied-root-with-index", `{"complete":true,"entries":[{"path":"index.md","kind":"file","content":"# Top\n"},{"path":"docs/sub","kind":"directory"}]}`, []string{"MD051 ../#nope", subIndexMissing}},
	} {
		t.Run(row.name, func(t *testing.T) {
			out, _ := md057Lint(t, source, settings(), engine.DocumentContext{Flavor: "gfm"}, func(s parser.Source) *md057Resources { return md057Supplied(t, row.raw, s) })
			if got := collect(out); !out.Complete || !reflect.DeepEqual(got, row.want) || len(out.Coverage)+len(out.Errors) != 0 {
				t.Fatal(got, out)
			}
		})
	}
}

// Reporting provenance is charged once per source, so many links in a large
// document stay within the default budgets.
func TestMD057ManyLinksWithinDefaultBudget(t *testing.T) {
	var text strings.Builder
	text.WriteString("```\n" + strings.Repeat("x", 200000) + "\n```\n\n")
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&text, "[a](guide.md?v=%d)\n", i)
	}
	// Each finding costs the engine work proportional to the source, so only
	// a few targets are missing.
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&text, "![b](x%d.png)\n", i)
	}
	layout := []md057Entry{{Path: "docs/guide.md", Kind: "file", Content: "# Guide\n"}}
	out, _ := md057Lint(t, text.String(), md057Settings(true), engine.DocumentContext{Flavor: "gfm"}, func(source parser.Source) *md057Resources {
		return md057Native(t, layout, "gfm", "", source)
	})
	if !out.Complete || len(out.Diagnostics) != 10 || len(out.Errors)+len(out.Interruptions)+len(out.Coverage) != 0 {
		t.Fatal(len(out.Diagnostics), out.Errors, out.Interruptions, out.Coverage)
	}
}

// BenchmarkMD057ManyLinks lints one large document with L local links. Per-link
// cost must not grow with document size: reporting provenance is built once
// per source, so ns/op grows linearly in L at a fixed body size. Missing
// targets use a smaller body because the engine's own per-finding projection
// work scales with the source.
func BenchmarkMD057ManyLinks(b *testing.B) {
	for _, links := range []int{100, 1000, 5000} {
		for _, existing := range []bool{false, true} {
			if !existing && links > 1000 {
				// 5000 findings exhaust the engine's own default work budget.
				continue
			}
			// A one-line fenced block pads the body within the default node budget.
			padding := "```\n" + strings.Repeat("x", 20000) + "\n```\n\n"
			if existing {
				padding = "```\n" + strings.Repeat("x", 200000) + "\n```\n\n"
			}
			var text strings.Builder
			text.WriteString(padding)
			for i := 0; i < links; i++ {
				if existing {
					fmt.Fprintf(&text, "[a](guide.md?v=%d)\n", i)
				} else {
					fmt.Fprintf(&text, "[a](x%d.md)\n", i)
				}
			}
			source := text.String()
			b.Run(fmt.Sprintf("links-%d/existing-%v/bytes-%d", links, existing, len(source)), func(b *testing.B) {
				r := registry(b, Registrations())
				in, err := engine.NewInput("docs/self.md", []byte(source), md057Settings(true), engine.DocumentContext{Flavor: "gfm"})
				if err != nil {
					b.Fatal(err)
				}
				layout := []md057Entry{{Path: "docs/guide.md", Kind: "file", Content: "# Guide\n"}}
				want := links
				if existing {
					want = 0
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					provider := md057Native(b, layout, "gfm", "", in.Source())
					b.StartTimer()
					out, err := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Fragments: provider})
					if err != nil || len(out.Errors)+len(out.Interruptions)+len(out.Coverage) != 0 || len(out.Diagnostics) != want {
						b.Fatal(err, out.Errors, out.Interruptions, out.Coverage)
					}
				}
			})
		}
	}
}
