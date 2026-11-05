package rules

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/integration"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
)

// md074Record is one expected MD074 record. Unlocated coverage has zero
// coordinates and no byte range.
type md074Record struct {
	Message     string `json:"message"`
	Reason      string `json:"reason,omitempty"`
	StartLine   int    `json:"start_line,omitempty"`
	StartColumn int    `json:"start_column,omitempty"`
	EndLine     int    `json:"end_line,omitempty"`
	EndColumn   int    `json:"end_column,omitempty"`
	ByteStart   *int   `json:"byte_start,omitempty"`
	ByteEnd     *int   `json:"byte_end,omitempty"`
}

type md074Expected struct {
	Options struct {
		ConfigPath    string `json:"config_path"`
		CheckAbsolute bool   `json:"check_absolute"`
		CheckOmitted  bool   `json:"check_omitted"`
	} `json:"options"`
	IndexFile   string        `json:"index_file"`
	Diagnostics []md074Record `json:"diagnostics"`
	Coverage    []md074Record `json:"coverage"`
}

// md074Cases are the reusable project fixtures. Every directory below
// testdata/md074 must appear here, and every name must have a directory.
var md074Cases = []string{
	"active-plugin", "crlf-config", "custom-index-file", "directory-index", "hooks", "inherit",
	"leading-slash-checked", "malformed-yaml", "missing-config", "nav-absent-omitted", "nav-empty-omitted",
	"nav-null-omitted", "nested-sections", "nonlocal-entries", "omitted-pages", "query-and-fragment",
	"rule-example-accepted", "rule-example-violating", "search-plugin", "subdirectory-config",
}

// md074Run drives one fixture project through a native read-only resource
// session, exactly as lint binds it, and projects the rule's records.
func md074Run(t *testing.T, dir string, want md074Expected) (diagnostics, coverage []md074Record) {
	t.Helper()
	ctx := context.Background()
	dir, err := filepath.Abs(dir)
	if err == nil {
		dir, err = filepath.EvalSymlinks(dir)
	}
	if err != nil {
		t.Fatal(err)
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	native, err := resource.OpenFilesystemRoot(ctx, budget, dir, resource.DefaultFilesystemLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := native.Close(); err != nil {
			t.Error(err)
		}
	})
	index := want.IndexFile
	if index == "" {
		index = "index.md"
	}
	settings, err := resource.NewSettings(parser.ParseOptions{}, index, "github", "md074-test")
	if err != nil {
		t.Fatal(err)
	}
	operation, err := resource.NewOperation(ctx, budget, native.Root(), settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := native.Reader(operation)
	if err != nil {
		t.Fatal(err)
	}
	service, err := resource.NewFilesystemService(reader, resource.DefaultServiceLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})
	view, err := operation.NewView(resource.Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	config := want.Options.ConfigPath
	if config == "" {
		config = "mkdocs.yml"
	}
	scope, err := integration.NewScope(ctx, operation, view, integration.Settings{MkDocs: integration.MkDocsPolicy{Enabled: true, ConfigPath: config, CheckAbsolute: want.Options.CheckAbsolute, CheckOmitted: want.Options.CheckOmitted}})
	if err != nil {
		t.Fatal(err)
	}
	if scope, err = scope.WithMkDocs(ctx, service); err != nil {
		t.Fatal(err)
	}
	mkdocs, err := scope.MkDocs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := MkDocsNavigation(ctx, scope, mkdocs)
	if err != nil {
		t.Fatal(err)
	}
	project := func(record MkDocsRecord, reason string) md074Record {
		got := md074Record{Message: record.Message, Reason: reason}
		if !record.Located {
			return got
		}
		span, bytes, snap, err := engine.ProjectSpan(ctx, budget, mkdocs.ConfigSource(), record.Span)
		if err != nil {
			t.Fatal(err)
		}
		if snap.View != "original" || snap.SizeBytes != mkdocs.ConfigSource().Len() {
			t.Fatal("snapshot", snap)
		}
		got.StartLine, got.StartColumn, got.EndLine, got.EndColumn = span.Start.Line, span.Start.Column, span.End.Line, span.End.Column
		got.ByteStart, got.ByteEnd = &bytes.Start, &bytes.End
		return got
	}
	diagnostics, coverage = []md074Record{}, []md074Record{}
	for _, finding := range outcome.Findings {
		if !finding.Located {
			t.Fatal("unlocated finding", finding)
		}
		diagnostics = append(diagnostics, project(finding, ""))
	}
	for _, record := range outcome.Coverage {
		coverage = append(coverage, project(record, result.ContextUnavailable))
	}
	return diagnostics, coverage
}

func TestMD074ProjectFixtures(t *testing.T) {
	root := filepath.Join("testdata", "md074")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}
	if !reflect.DeepEqual(dirs, md074Cases) {
		t.Fatalf("fixture directories %v, referenced cases %v", dirs, md074Cases)
	}
	for _, name := range md074Cases {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(root, name)
			raw, err := os.ReadFile(filepath.Join(dir, "expected.json")) //nolint:gosec // Read only the checked-in fixture tree.
			if err != nil {
				t.Fatal(err)
			}
			var want md074Expected
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			if want.Diagnostics == nil || want.Coverage == nil {
				t.Fatal("expected.json must list diagnostics and coverage")
			}
			before := md074Tree(t, dir)
			diagnostics, coverage := md074Run(t, dir, want)
			if !reflect.DeepEqual(diagnostics, want.Diagnostics) {
				t.Errorf("diagnostics\ngot  %s\nwant %s", md074JSON(diagnostics), md074JSON(want.Diagnostics))
			}
			if !reflect.DeepEqual(coverage, want.Coverage) {
				t.Errorf("coverage\ngot  %s\nwant %s", md074JSON(coverage), md074JSON(want.Coverage))
			}
			if after := md074Tree(t, dir); !reflect.DeepEqual(before, after) {
				t.Fatal("MD074 analysis changed the fixture tree")
			}
		})
	}
}

// The CRLF fixture keeps its line endings through checkout.
func TestMD074CRLFFixtureBytes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "md074", "crlf-config", "mkdocs.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(raw, '\r') {
		t.Fatal("CRLF fixture lost its carriage returns")
	}
}

func md074Tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path) //nolint:gosec // Read only the checked-in fixture tree.
		tree[path] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func md074JSON(records []md074Record) string {
	data, _ := json.Marshal(records)
	return string(data)
}

// MD074 is available in the production registry, stays a report-only,
// default-disabled rule and is never invoked by the per-document loop, even
// when a test registry supplies a callback for it.
func TestMD074AvailableProjectRuleNeverRunsPerDocument(t *testing.T) {
	production, err := engine.NewCatalogRegistry(Registrations())
	if err != nil {
		t.Fatal(err)
	}
	if !production.Available("MD074") || !engine.ProjectRule("MD074") || engine.ProjectRule("MD057") {
		t.Fatal("MD074 is not an available project rule")
	}
	for _, m := range production.Metadata() {
		if m.ID == "MD074" && (m.DefaultEnabled || m.Fix != engine.NoFix) {
			t.Fatal("MD074 metadata", m)
		}
	}
	for _, registration := range Registrations() {
		if registration.ID == "MD074" {
			t.Fatal("MD074 has a per-document registration")
		}
	}
	called := false
	probe := func(context.Context, engine.RuleContext) engine.RuleOutcome {
		called = true
		return engine.RuleOutcome{Findings: []engine.Finding{}}
	}
	registry, err := engine.NewCatalogRegistry([]engine.Registration{{ID: "MD074", Check: probe}})
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	settings := engine.Settings{}
	for _, m := range registry.Metadata() {
		off := false
		settings[m.ID] = engine.RuleSetting{Enabled: &off}
	}
	settings["MD074"] = engine.RuleSetting{Enabled: &enabled}
	for _, r := range []*engine.Registry{production, registry} {
		selection, err := r.SelectRules(settings, engine.ValidationProviders{})
		if err != nil {
			t.Fatal(err)
		}
		plan, err := selection.Plan(engine.DocumentContext{Flavor: "gfm"}, nil)
		if err != nil {
			t.Fatal("explicitly enabled MD074 is unavailable", err)
		}
		in, err := plan.NewInput("doc.md", []byte("<!-- mdchk:enable MD074 -->\n# Doc\n"))
		if err != nil {
			t.Fatal(err)
		}
		out, err := plan.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if called || len(out.Diagnostics) != 0 || len(out.Coverage) != 0 || !out.Complete {
			t.Fatal("per-document MD074 evaluation", called, out.Diagnostics, out.Coverage)
		}
	}
}
