package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
)

type mkDocsFixture struct {
	scope     *Scope
	service   *resource.Service
	operation *resource.Operation
	view      resource.View
	budget    *parser.OperationBudget
}

func suppliedMkDocsFixture(t *testing.T, configPath string, entries map[string]any, complete bool, policy MkDocsPolicy) mkDocsFixture {
	t.Helper()
	ctx := context.Background()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	root, _ := resource.NewRoot(resource.SuppliedRoot)
	settings, _ := resource.NewSettings(parser.ParseOptions{}, "README.md", "github", "mkdocs-test")
	op, e := resource.NewOperation(ctx, budget, root, settings, nil)
	if e != nil {
		t.Fatal(e)
	}
	declared := make([]any, 0, len(entries))
	for name, entry := range entries {
		fields := entry.(map[string]any)
		fields["path"] = name
		declared = append(declared, fields)
	}
	raw, e := json.Marshal(map[string]any{"complete": complete, "entries": declared})
	if e != nil {
		t.Fatal(e)
	}
	input, e := resource.DecodeSupplied(ctx, budget, resource.DefaultInputLimits(), raw, nil)
	if e != nil {
		t.Fatal(e)
	}
	reader, e := resource.NewSuppliedReader(ctx, op, input, resource.DefaultInputLimits())
	if e != nil {
		t.Fatal(e)
	}
	service, e := resource.NewSuppliedService(reader, resource.DefaultServiceLimits())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := service.Close(); e != nil {
			t.Error(e)
		}
	})
	view, e := op.NewView(resource.Original, nil)
	if e != nil {
		t.Fatal(e)
	}
	policy.ConfigPath = configPath
	scope, e := NewScope(ctx, op, view, Settings{MkDocs: policy})
	if e != nil {
		t.Fatal(e)
	}
	scope, e = scope.WithMkDocs(ctx, service)
	if e != nil {
		t.Fatal(e)
	}
	return mkDocsFixture{scope, service, op, view, budget}
}
func mkDocsText(text string) any { return map[string]any{"kind": "file", "content": text} }
func mkDocsFile() any            { return map[string]any{"kind": "file"} }
func mkDocsDir() any             { return map[string]any{"kind": "directory"} }
func mustMkDocs(t *testing.T, f mkDocsFixture) MkDocsContext {
	t.Helper()
	c, e := f.scope.MkDocs(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func targetNames(c MkDocsContext) []string {
	var out []string
	for i := 0; i < c.KnownTargetCount(); i++ {
		v, _ := c.KnownTarget(i)
		key, _ := v.Target().Key()
		out = append(out, key.String())
	}
	return out
}
func pageClasses(c MkDocsContext) map[string]Eligibility {
	out := map[string]Eligibility{}
	i := c.Inventory()
	for n := 0; n < i.KnownCount(); n++ {
		p, _ := i.KnownPage(n)
		out[p.Key().String()] = p.Eligibility()
	}
	return out
}

func TestMkDocsProviderAcquiredUnselectedConfigAndSource(t *testing.T) {
	for _, prefix := range []string{"", "\xef\xbb\xbf"} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(prefix+ending, func(t *testing.T) {
				raw := prefix + strings.ReplaceAll("# é図\ndocs_dir: pages\nshared: &guide \"g\\u0075ide.md\"\nother: {safe: [42, true, null]}\nnav:\n - \"Début\": *guide\n - Section:\n    - asset.bin\n    - \"?query#bad%\"\n", "\n", ending)
				f := suppliedMkDocsFixture(t, "site/mkdocs.yml", map[string]any{"site/mkdocs.yml": mkDocsText(raw), "site/pages/guide.md": mkDocsFile(), "site/pages/asset.bin": mkDocsFile(), "site/pages/README.md": mkDocsFile()}, true, MkDocsPolicy{Enabled: true, CheckOmitted: true})
				c := mustMkDocs(t, f)
				if c.Status() != Available || !c.Complete() || !c.Static() || c.DocsRoot().Name() != "site/pages" || f.view.TargetCount() != 0 {
					t.Fatalf("context: status=%s complete=%v static=%v root=%s problems=%v", c.Status(), c.Complete(), c.Static(), c.DocsRoot().Name(), c.problems)
				}
				if got := targetNames(c); !reflect.DeepEqual(got, []string{"site/pages/guide.md", "site/pages/asset.bin", "site/pages/README.md"}) {
					t.Fatal(got)
				}
				items, e := c.Navigation().Entries(context.Background(), f.scope)
				if e != nil {
					t.Fatal(e)
				}
				if items[0].Title != "Début" || items[0].Destination != "guide.md" || len(items[1].Children) != 2 {
					t.Fatal(items)
				}
				spelling, e := items[0].Location.Source().Text(items[0].Location.Span())
				if e != nil || spelling != "*guide" {
					t.Fatalf("alias use range %q %v", spelling, e)
				}
				if items[0].Location.Source().Token() != c.ConfigSource().Token() {
					t.Fatal("foreign source")
				}
				whole, _ := c.ConfigSource().Span(0, c.ConfigSource().Len())
				bytes, _ := c.ConfigSource().Text(whole)
				if bytes != raw {
					t.Fatal("source mutated")
				}
				items[0].Destination = "changed"
				again, e := c.Navigation().Entries(context.Background(), f.scope)
				if e != nil || again[0].Destination != "guide.md" {
					t.Fatal("mutable copy", e)
				}
				allowed, e := f.scope.CanCheckOmissions(context.Background(), c.Navigation(), c.Inventory(), c.Static())
				if e != nil || !allowed {
					t.Fatal("complete inventory unusable", allowed, e)
				}
			})
		}
	}
}

func TestMkDocsProviderShapesAndDynamicBoundary(t *testing.T) {
	tests := []struct {
		name, config     string
		mode             NavMode
		static, complete bool
	}{
		{"absent", "site_name: X\n", AutomaticNav, true, true}, {"null", "nav: null\n", AutomaticNav, true, true}, {"manual-empty", "nav: []\n", ManualNav, true, true},
		{"search", "plugins: [search]\nnav: []\n", ManualNav, true, true}, {"disabled", "plugins: [{custom: {enabled: false}}]\nnav: []\n", ManualNav, true, true},
		{"string-false", "plugins: [{custom: {enabled: 'false'}}]\nnav: []\n", ManualNav, false, false}, {"plugin-null", "plugins: [{custom: null}]\nnav: []\n", ManualNav, false, false},
		{"hook", "hooks: [do-not-execute.py]\nnav: [guide.md]\n", ManualNav, false, false}, {"inherit", "INHERIT: forbidden.yml\nnav: [guide.md]\n", ManualNav, false, false},
		{"wrong-nav", "nav: {}\n", UnknownNav, false, false}, {"bad-entry", "nav: [guide.md, 17]\n", ManualNav, false, false},
		{"duplicate-unrelated", "foo: 1\nfoo: 2\nnav: [guide.md]\n", ManualNav, false, false},
		{"custom-tag", "foo: !environment VALUE\nnav: [guide.md]\n", ManualNav, false, false},
		{"safe-core", "other: {a: !!int 42, b: !!str\tfalse, c: !!bool true, d: 1.2}\nnav: [guide.md]\n", ManualNav, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := suppliedMkDocsFixture(t, "mkdocs.yml", map[string]any{"mkdocs.yml": mkDocsText(tt.config), "docs/guide.md": mkDocsFile()}, true, MkDocsPolicy{Enabled: true, CheckOmitted: true})
			c := mustMkDocs(t, f)
			if c.Navigation().Mode() != tt.mode || c.Static() != tt.static || c.Complete() != tt.complete {
				t.Fatalf("mode=%s static=%v complete=%v problems=%v", c.Navigation().Mode(), c.Static(), c.Complete(), c.problems)
			}
			if strings.Contains(tt.config, "guide.md") {
				target, ok := c.KnownTarget(0)
				if !ok || target.Existence() != resource.File {
					t.Fatal("lost independent positive target", target)
				}
			}
			allowed, e := f.scope.CanCheckOmissions(context.Background(), c.Navigation(), c.Inventory(), c.Static())
			if e != nil || allowed != (tt.mode == ManualNav && tt.static && tt.complete) {
				t.Fatal("omission authority", allowed, e)
			}
		})
	}
}

func TestMkDocsProviderTargetNormalizationAndDocsSubroot(t *testing.T) {
	raw := "docs_dir: pages\nnav:\n - guide.md\n - /guide.md\n - ../pages-evil/guide.md\n - '%2e%2e/pages-evil/guide.md'\n - '%2520.md'\n - a+b.md\n - 'guide.md#bad%'\n - ''\n - dir/\n - https://example.com/x\n - //example.com/x\n - file:///guide.md\n - 'C:/guide.md'\n - x%2fy.md\n - Guide.md\n - binary.bin\n"
	for _, absolute := range []bool{false, true} {
		t.Run(map[bool]string{false: "skip-absolute", true: "docs-absolute"}[absolute], func(t *testing.T) {
			entries := map[string]any{"site/mkdocs.yml": mkDocsText(raw), "site/pages/guide.md": mkDocsFile(), "site/pages-evil/guide.md": mkDocsFile(), "site/pages/%20.md": mkDocsFile(), "site/pages/a+b.md": mkDocsFile(), "site/pages/README.md": mkDocsFile(), "site/pages/dir/README.md": mkDocsDir(), "site/pages/binary.bin": mkDocsFile()}
			f := suppliedMkDocsFixture(t, "site/mkdocs.yml", entries, true, MkDocsPolicy{Enabled: true, CheckAbsolute: absolute})
			c := mustMkDocs(t, f)
			targets, e := c.Targets(context.Background(), f.scope)
			if e != nil {
				t.Fatal(e)
			}
			if len(targets) != 16 {
				t.Fatal(len(targets))
			}
			for _, i := range []int{0, 4, 5, 6, 7, 15} {
				if targets[i].Existence() != resource.File {
					t.Fatalf("target %d: %+v", i, targets[i])
				}
			}
			if absolute && targets[1].Existence() != resource.File || !absolute && targets[1].Class() != resource.Ignored {
				t.Fatal("absolute docs base", targets[1])
			}
			for _, i := range []int{2, 3, 11, 12, 13} {
				if targets[i].Class() != resource.BlockedReference {
					t.Fatalf("escaped/blocked target %d: %+v", i, targets[i])
				}
			}
			for _, i := range []int{9, 10} {
				if targets[i].Class() != resource.Ignored {
					t.Fatal("remote", targets[i])
				}
			}
			if targets[14].Existence() != resource.Missing || targets[8].Existence() != resource.Directory || targets[8].RequiredType() != resource.RegularFile {
				t.Fatal("case or nonrecursive index", targets[14], targets[8])
			}
		})
	}
}

func TestMkDocsProviderLiteralDocsRootAndUnavailableConfig(t *testing.T) {
	for _, docs := range []string{"", "pages#part?x", "pages%20"} {
		t.Run(docs, func(t *testing.T) {
			root := "site"
			if docs != "" {
				root += "/" + docs
			}
			f := suppliedMkDocsFixture(t, "site/mkdocs.yml", map[string]any{"site/mkdocs.yml": mkDocsText("docs_dir: '" + docs + "'\nnav: ['']"), root + "/README.md": mkDocsFile()}, true, MkDocsPolicy{Enabled: true})
			c := mustMkDocs(t, f)
			if c.DocsRoot().Name() != root || !reflect.DeepEqual(targetNames(c), []string{root + "/README.md"}) {
				t.Fatal(c.DocsRoot().Name(), targetNames(c))
			}
		})
	}
	for _, config := range []string{"", "nav: [", "nav: []\n---\nnav: []", "docs_dir: 3\nnav: []", "docs_dir: ../../outside\nnav: []", "x: &a [*a]\nnav: []", "nav: [*unknown]"} {
		t.Run(config, func(t *testing.T) {
			f := suppliedMkDocsFixture(t, "mkdocs.yml", map[string]any{"mkdocs.yml": mkDocsText(config), "docs": mkDocsDir()}, true, MkDocsPolicy{Enabled: true})
			c := mustMkDocs(t, f)
			if c.Complete() || c.Status() != Unavailable && len(c.problems) == 0 {
				t.Fatalf("invalid config claimed complete: %+v", c)
			}
		})
	}
	for _, entry := range []any{nil, mkDocsFile(), map[string]any{"kind": "unavailable"}} {
		t.Run("missing-content", func(t *testing.T) {
			entries := map[string]any{}
			if entry != nil {
				entries["mkdocs.yml"] = entry
			}
			f := suppliedMkDocsFixture(t, "mkdocs.yml", entries, true, MkDocsPolicy{Enabled: true})
			c := mustMkDocs(t, f)
			if c.Status() != Unavailable || c.ConfigSource().Token() != (parser.SourceToken{}) {
				t.Fatal(c.Status())
			}
		})
	}
}

func TestMkDocsProviderEligibleSetIndependentOfLintSelection(t *testing.T) {
	config := "nav: []\nexclude_docs: |\n  hidden/\n  !hidden/keep.md\n  index.md\n  unicode-?.md\ndraft_docs: |\n  draft.md\n  overlap.md\nnot_in_nav: |\n  quiet.md\n  overlap.md\n"
	names := []string{"ordinary.md", "other.markdown", "alt.mdown", "alt.mkdn", "alt.mkd", "UPPER.MD", "unsupported.data", "binary.bin", ".secret.md", "templates/theme.md", "hidden/no.md", "hidden/keep.md", "index.md", "README.md", "draft.md", "quiet.md", "overlap.md", "unicode-é.md", "unicode-図.md", "unicode-ab.md", "sub/index.mkd", "sub/README.md"}
	expected := map[string]Eligibility{"docs/ordinary.md": EligiblePage, "docs/other.markdown": EligiblePage, "docs/alt.mdown": ConflictingReadme, "docs/alt.mkdn": EligiblePage, "docs/alt.mkd": EligiblePage, "docs/.secret.md": ExcludedPage, "docs/templates/theme.md": ExcludedPage, "docs/hidden/no.md": ExcludedPage, "docs/hidden/keep.md": EligiblePage, "docs/index.md": ExcludedPage, "docs/README.md": EligiblePage, "docs/draft.md": DraftPage, "docs/quiet.md": NotInNavPage, "docs/overlap.md": DraftPage, "docs/unicode-é.md": ExcludedPage, "docs/unicode-図.md": ExcludedPage, "docs/unicode-ab.md": EligiblePage, "docs/sub/index.mkd": EligiblePage, "docs/sub/README.md": ConflictingReadme}
	for _, complete := range []bool{false, true} {
		for _, unavailable := range []bool{false, true} {
			t.Run(map[bool]string{false: "partial", true: "complete"}[complete]+map[bool]string{false: "", true: "-unavailable"}[unavailable], func(t *testing.T) {
				entries := map[string]any{"mkdocs.yml": mkDocsText(config)}
				for _, name := range names {
					entries["docs/"+name] = mkDocsFile()
				}
				if unavailable {
					entries["docs/unknown"] = map[string]any{"kind": "unavailable"}
				}
				f := suppliedMkDocsFixture(t, "mkdocs.yml", entries, complete, MkDocsPolicy{Enabled: true, CheckOmitted: true})
				c := mustMkDocs(t, f)
				if got := pageClasses(c); !reflect.DeepEqual(got, expected) {
					t.Fatalf("eligible classes\ngot %v\nwant%v", got, expected)
				}
				allowed, e := f.scope.CanCheckOmissions(context.Background(), c.Navigation(), c.Inventory(), c.Static())
				if e != nil || allowed != (complete && !unavailable) {
					t.Fatal("inventory completeness", allowed, e)
				}
			})
		}
	}
}

func TestMkDocsProviderControlsAndInactiveNoAcquire(t *testing.T) {
	f := suppliedMkDocsFixture(t, "mkdocs.yml", map[string]any{"mkdocs.yml": mkDocsText("nav: [guide.md]"), "docs/guide.md": mkDocsFile()}, true, MkDocsPolicy{Enabled: true})
	c := mustMkDocs(t, f)
	if c.KnownTargetCount() != 1 {
		t.Fatal("no warm target")
	}
	if e := f.service.Close(); e != nil {
		t.Fatal(e)
	}
	target, ok := c.KnownTarget(0)
	if !ok || target.Existence() != resource.File {
		t.Fatal("lost warm positive")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	limit := f.budget.ChargeWork(f.budget.Limits().WorkUnits + 1)
	_, e := f.scope.MkDocs(ctx)
	var actual, want *parser.LimitExceeded
	if !errors.Is(e, context.Canceled) || !errors.As(e, &actual) || !errors.As(limit, &want) || *actual != *want {
		t.Fatal("lost canonical stop", e, limit)
	}
	inactive := suppliedMkDocsFixture(t, "mkdocs.yml", nil, true, MkDocsPolicy{})
	got := mustMkDocs(t, inactive)
	if got.Status() != Inactive {
		t.Fatal(got.Status())
	}
}

func TestFilesystemMkDocsProviderConfinedConfigInventoryAndTargets(t *testing.T) {
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	for name, content := range map[string]string{"site/mkdocs.yml": "docs_dir: pages\nnav: [guide.md, dir/, ../outside.md]\n", "site/pages/guide.md": "# page", "site/pages/dir/README.md": "index", "site/pages/omitted.mdown": "other", "site/outside.md": "outside"} {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if e := os.MkdirAll(filepath.Dir(full), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(full, []byte(content), 0600); e != nil {
			t.Fatal(e)
		}
	}
	ctx := context.Background()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	native, e := resource.OpenFilesystemRoot(ctx, budget, dir, resource.DefaultFilesystemLimits())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := native.Close(); e != nil {
			t.Error(e)
		}
	})
	settings, _ := resource.NewSettings(parser.ParseOptions{}, "README.md", "github", "native-mkdocs-test")
	op, e := resource.NewOperation(ctx, budget, native.Root(), settings, nil)
	if e != nil {
		t.Fatal(e)
	}
	reader, e := native.Reader(op)
	if e != nil {
		t.Fatal(e)
	}
	service, e := resource.NewFilesystemService(reader, resource.DefaultServiceLimits())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := service.Close(); e != nil {
			t.Error(e)
		}
	})
	view, _ := op.NewView(resource.Original, nil)
	scope, e := NewScope(ctx, op, view, Settings{MkDocs: MkDocsPolicy{Enabled: true, ConfigPath: "site/mkdocs.yml", CheckOmitted: true}})
	if e != nil {
		t.Fatal(e)
	}
	scope, e = scope.WithMkDocs(ctx, service)
	if e != nil {
		t.Fatal(e)
	}
	c, e := scope.MkDocs(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if !c.ConfigComplete() || c.TargetsComplete() || !reflect.DeepEqual(targetNames(c), []string{"site/pages/guide.md", "site/pages/dir/README.md", ""}) {
		t.Fatal("native targets", targetNames(c), c.problems)
	}
	expected := map[string]Eligibility{"site/pages/guide.md": EligiblePage, "site/pages/dir/README.md": EligiblePage, "site/pages/omitted.mdown": EligiblePage}
	if !reflect.DeepEqual(pageClasses(c), expected) {
		t.Fatal(pageClasses(c))
	}
}

func TestMkDocsProviderProblemCopiesAndBoundedDecode(t *testing.T) {
	f := suppliedMkDocsFixture(t, "mkdocs.yml", map[string]any{"mkdocs.yml": mkDocsText("nav: [guide.md]\n"), "docs/guide.md": mkDocsFile()}, true, MkDocsPolicy{Enabled: true})
	c := mustMkDocs(t, f)
	// Fixed metadata copies are detached even when a known fact carries a quota.
	first := MkDocsTarget{problem: resource.Problem{Kind: resource.Limited, Limit: &result.LimitObservation{Name: "work-units", Observed: 9, Allowed: 8}}}
	returned := first.Problem()
	returned.Limit.Name = "changed"
	if first.Problem().Limit.Name != "work-units" {
		t.Fatal("returned problem mutated retained fact")
	}
	c.configObservation.Problem = resource.Problem{Kind: resource.Limited, Limit: &result.LimitObservation{Name: "resource-lookups", Observed: 2, Allowed: 1}}
	returned = c.ConfigObservation().Problem
	returned.Limit.Allowed = 999
	if c.ConfigObservation().Problem.Limit.Allowed != 1 {
		t.Fatal("config observation alias")
	}
	// The actual acquired Source enters the shared guarded decoder. Tiny node
	// admission fails before dependency lexing, never becoming malformed YAML.
	limits := parser.DefaultLimits()
	limits.TotalNodes = 64
	limits.DocumentNodes = 64
	bounded := mkDocsPatternScopeWithLimits(t, limits)
	_, e := decodeMkDocsYAML(context.Background(), bounded, c.ConfigSource())
	var limit *parser.LimitExceeded
	if !errors.As(e, &limit) || limit.Name != "total-nodes" {
		t.Fatal("unbounded YAML entry", e)
	}
}

func TestMkDocsProviderSafeScalarRangesAndMalformedBoundaries(t *testing.T) {
	values := []struct{ yaml, want string }{
		{`"g\u0075ide.md"`, "guide.md"}, {`!!str guide.md`, "guide.md"}, {`!<tag:yaml.org,2002:str> guide.md`, "guide.md"}, {"\"a\tb.md\"", "a\tb.md"},
	}
	for _, tt := range values {
		t.Run(tt.yaml, func(t *testing.T) {
			raw := "# 図\r\nnav: [" + tt.yaml + "]"
			f := suppliedMkDocsFixture(t, "mkdocs.yml", map[string]any{"mkdocs.yml": mkDocsText(raw), "docs": mkDocsDir()}, true, MkDocsPolicy{Enabled: true})
			c := mustMkDocs(t, f)
			entry, ok := c.Navigation().KnownEntry(0)
			if !ok || entry.Destination != tt.want {
				t.Fatal("decoded scalar", entry, c.problems)
			}
			text, e := entry.Location.Source().Text(entry.Location.Span())
			if e != nil || text != tt.yaml {
				t.Fatalf("authored scalar range: got%q want%q %v", text, tt.yaml, e)
			}
		})
	}
	for _, raw := range []string{"nav: [\"broken\\xZZ\"]", "nav: []\nother: >0\n value", "nav: [guide.md]\nother: {<<: {a: b}}", "nav: [guide.md]\nother: {duplicate: 1, duplicate: 2}"} {
		t.Run(raw, func(t *testing.T) {
			f := suppliedMkDocsFixture(t, "mkdocs.yml", map[string]any{"mkdocs.yml": mkDocsText(raw), "docs/guide.md": mkDocsFile()}, true, MkDocsPolicy{Enabled: true})
			c := mustMkDocs(t, f)
			if c.Complete() || len(c.problems) == 0 {
				t.Fatal("malformed boundary supplied authority", c)
			}
		})
	}
}

func FuzzMkDocsProviderSourceAndConfinement(f *testing.F) {
	for _, seed := range []string{"nav: []", "nav: [guide.md, ../escape.md]", "docs_dir: docs\r\nnav: ['?x#y']", "nav: [*missing]", "x: !environment SECRET\nnav: [guide.md]"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 512 || !utf8.ValidString(raw) {
			t.Skip()
		}
		fixture := suppliedMkDocsFixture(t, "mkdocs.yml", map[string]any{"mkdocs.yml": mkDocsText(raw), "docs/guide.md": mkDocsFile(), "docs/README.md": mkDocsFile()}, false, MkDocsPolicy{Enabled: true, CheckOmitted: true, CheckAbsolute: true})
		c, e := fixture.scope.MkDocs(context.Background())
		if e != nil {
			return
		}
		for i := 0; i < c.KnownTargetCount(); i++ {
			target, _ := c.KnownTarget(i)
			if target.Location().Source().Token() != c.ConfigSource().Token() {
				t.Fatal("foreign source target")
			}
			if _, e := c.ConfigSource().Text(target.Location().Span()); e != nil {
				t.Fatal("unconfined source range", e)
			}
			if target.Class() == resource.Local {
				key, ok := target.Target().Key()
				if !ok {
					t.Fatal("final local target lacks real index key")
				}
				root := c.DocsRoot().Name()
				if root != "" && key.String() != root && !strings.HasPrefix(key.String(), root+"/") {
					t.Fatal("docs subroot escaped", root, key)
				}
			}
		}
		if c.Navigation().Mode() != "" {
			allowed, e := fixture.scope.CanCheckOmissions(context.Background(), c.Navigation(), c.Inventory(), c.Static())
			if e != nil || allowed {
				t.Fatal("partial input minted omission completeness", e)
			}
		}
	})
}

func TestMkDocsProviderReadmeConflictsAfterInclusion(t *testing.T) {
	entries := map[string]any{"mkdocs.yml": mkDocsText("nav: []\ndraft_docs: draft/index.md\nnot_in_nav: quiet/index.md\nexclude_docs: excluded/index.html\n"), "docs/draft/README.md": mkDocsFile(), "docs/draft/index.md": mkDocsFile(), "docs/quiet/README.md": mkDocsFile(), "docs/quiet/index.md": mkDocsFile(), "docs/asset/README.md": mkDocsFile(), "docs/asset/index.html": mkDocsFile(), "docs/excluded/README.md": mkDocsFile(), "docs/excluded/index.html": mkDocsFile()}
	fixture := suppliedMkDocsFixture(t, "mkdocs.yml", entries, true, MkDocsPolicy{Enabled: true, CheckOmitted: true})
	c := mustMkDocs(t, fixture)
	expected := map[string]Eligibility{"docs/draft/README.md": EligiblePage, "docs/draft/index.md": DraftPage, "docs/quiet/README.md": ConflictingReadme, "docs/quiet/index.md": NotInNavPage, "docs/asset/README.md": ConflictingReadme, "docs/excluded/README.md": EligiblePage}
	if !reflect.DeepEqual(pageClasses(c), expected) {
		t.Fatal("README exclusion/inclusion oracle", pageClasses(c), expected)
	}
}

func TestMkDocsProviderQuotedNumericEscapes(t *testing.T) {
	for _, tc := range []struct {
		name, escaped, decoded string
		valid                  bool
	}{
		{"surrogate pair", `\uD83D\uDE00`, "😀", true},
		{"scalar", `\U0001F600`, "😀", true},
		{"bmp", `\u00E9`, "é", true},
		{"hex", `\x61`, "a", true},
		{"isolated high", `\uD83D`, "", false},
		{"isolated low", `\uDE00`, "", false},
		{"mismatched pair", `\uD83D\u0041`, "", false},
		{"wide surrogate", `\U0000D83D`, "", false},
		{"out of range", `\U00110000`, "", false},
		{"nonhex", `\uZZZZ`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := "nav: [\"" + tc.escaped + ".md\"]\n"
			f := suppliedMkDocsFixture(t, "mkdocs.yml", map[string]any{"mkdocs.yml": mkDocsText(config), "docs": mkDocsDir(), "docs/" + tc.decoded + ".md": mkDocsFile()}, true, MkDocsPolicy{Enabled: true})
			c := mustMkDocs(t, f)
			if tc.valid {
				if !c.ConfigComplete() || !reflect.DeepEqual(targetNames(c), []string{"docs/" + tc.decoded + ".md"}) {
					t.Fatalf("valid escape lost: complete=%v targets=%v", c.ConfigComplete(), targetNames(c))
				}
			} else if c.ConfigComplete() {
				t.Fatal("malformed escape accepted")
			}
		})
	}
}

func TestMkDocsNeutralYAMLRetainsDiagnosticsAndSiblingFacts(t *testing.T) {
	for _, tc := range []struct{ config, message, spelling string }{{"site_name: Good\nbad: \"\\uZZZZ\"\n", "Malformed MkDocs quoted escape.", `"\uZZZZ"`}, {"site_name: Good\nx: 1\nx: 2\n", "Duplicate YAML key.", "x"}} {
		fixture := suppliedMkDocsFixture(t, "mkdocs.yml", map[string]any{"mkdocs.yml": mkDocsText(tc.config)}, true, MkDocsPolicy{Enabled: true})
		source := parser.NewSource([]byte(tc.config))
		decoded, err := decodeMkDocsYAML(context.Background(), fixture.scope, source)
		if err != nil || decoded.root == nil {
			t.Fatal("neutral boundary lost partial configuration", err)
		}
		good, ok := decoded.root.field("site_name")
		if !ok || good.kind != "string" || good.text != "Good" {
			t.Fatal("reliable sibling lost")
		}
		found := false
		for _, problem := range decoded.problems {
			if problem.Message() == tc.message {
				found = true
				loc := problem.Location()
				text, err := source.Text(loc.Span())
				if err != nil || loc.Source().Token() != source.Token() || text != tc.spelling {
					t.Fatal("original diagnostic location lost", text, err)
				}
			}
		}
		if !found {
			t.Fatal("specific diagnostic lost", tc.message, decoded.problems)
		}
	}
}
