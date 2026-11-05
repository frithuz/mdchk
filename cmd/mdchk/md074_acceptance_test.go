package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

type md074Located struct {
	path                         string
	startLine, startCol, endLine int
	endCol, byteStart, byteEnd   int
	coverage                     bool
	message                      string
}

// md074Collect returns every MD074 record. Coverage has no byte range, and
// unlocated coverage has zero coordinates.
func md074Collect(t *testing.T, out result.Result) []md074Located {
	t.Helper()
	var got []md074Located
	for _, d := range out.Diagnostics {
		if d.Rule == "MD074" {
			if d.FixAvailable {
				t.Fatal("MD074 offered a fix", d)
			}
			got = append(got, md074Located{d.Path, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column, d.ByteRange.Start, d.ByteRange.End, false, d.Message})
		}
	}
	for _, c := range out.Coverage {
		if c.Rule == "MD074" {
			if c.Reason != "context-unavailable" {
				t.Fatal("unexpected MD074 coverage reason", c)
			}
			located := md074Located{path: c.Path, byteStart: -1, byteEnd: -1, coverage: true, message: c.Message}
			if c.Range != nil {
				located.startLine, located.startCol, located.endLine, located.endCol = c.Range.Start.Line, c.Range.Start.Column, c.Range.End.Line, c.Range.End.Column
			}
			got = append(got, located)
		}
	}
	return got
}

func md074Configuration(rule string) string {
	return strings.Replace(cleanConfiguration(), "MD074: {enabled: false}", "MD074: "+rule, 1)
}

func md074Project(t *testing.T, conf string, files map[string]string) string {
	t.Helper()
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", conf)
	putConfigurationFile(t, root, "README.md", "# Readme\n")
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, contents := range files {
		putConfigurationFile(t, root, name, contents)
	}
	return root
}

const (
	md074LintOnly = "MkDocs navigation is checked only by lint."
	md074Omitted  = "Pages omitted from MkDocs navigation could not be checked."
)

func md074Missing(destination string) string {
	return "MkDocs navigation target does not exist: " + destination + "."
}

// md074LintOnlyRuns checks that fix --dry-run, inspect and fix each record
// exactly one unlocated lint-only coverage entry and nothing else for MD074.
func md074LintOnlyRuns(t *testing.T, binary, root, target string) {
	t.Helper()
	want := []md074Located{{"", 0, 0, 0, 0, -1, -1, true, md074LintOnly}}
	dry := md014NativeRun(t, binary, root, "fix", "--dry-run", target, "--format=json")
	planned := md074JSON(t, dry, root, "fix")
	if have := md074Collect(t, planned); dry.Status != 15 || !reflect.DeepEqual(have, want) || len(planned.Changes)+len(planned.Documents)+len(planned.DeferredFixes) != 0 {
		t.Fatal("fix --dry-run", dry.Status, have, dry.Stdout)
	}
	seen := md014NativeRun(t, binary, root, "inspect", target, "--format=json")
	inspected := md074JSON(t, seen, root, "inspect")
	if have := md074Collect(t, inspected); seen.Status != 15 || !reflect.DeepEqual(have, want) || inspected.Complete {
		t.Fatal("inspect", seen.Status, have, seen.Stdout)
	}
	applied := md014NativeRun(t, binary, root, "fix", target, "--format=json")
	fixed := md074JSON(t, applied, root, "fix")
	if have := md074Collect(t, fixed); applied.Status != 15 || !reflect.DeepEqual(have, want) || len(fixed.Changes) != 0 {
		t.Fatal("fix", applied.Status, have, applied.Stdout)
	}
}

// These rows run the compiled CLI over native projects. Lint runs MD074 once
// over the MkDocs configuration through the shared read-only resource session;
// fix and inspect only record that the check is lint-only. No run creates or
// changes a file.
func TestCompiledPlatformMD074MkDocsNavigation(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, tc := range []struct {
		name, rule, extra string
		files             map[string]string
		args              []string
		status            int
		want              []md074Located
	}{
		{name: "missing", rule: "{enabled: true}", files: map[string]string{"mkdocs.yml": "nav: [{Guide: missing.md}]\n"}, status: 10, want: []md074Located{{"mkdocs.yml", 1, 15, 1, 25, 14, 24, false, md074Missing("missing.md")}}},
		{name: "found", rule: "{enabled: true}", files: map[string]string{"mkdocs.yml": "nav: [{Guide: guide.md}]\n", "docs/guide.md": "# Guide\n"}},
		{name: "nonlocal", rule: "{enabled: true}", files: map[string]string{"mkdocs.yml": "nav:\n  - https://example.com/x.md\n  - //example.com/x.md\n  - /missing.md\n"}},
		{name: "absolute-checked", rule: "{enabled: true, check_absolute: true}", files: map[string]string{"mkdocs.yml": "nav: [/missing.md, /guide.md, //example.com/x.md]\n", "docs/guide.md": "# Guide\n"}, status: 10, want: []md074Located{{"mkdocs.yml", 1, 7, 1, 18, 6, 17, false, md074Missing("/missing.md")}}},
		{name: "directory-index", rule: "{enabled: true}", files: map[string]string{"mkdocs.yml": "nav: [dir/, 'guide.md?x=1#top']\n", "docs/dir/index.md": "# Dir\n", "docs/guide.md": "# Guide\n"}},
		{name: "custom-index-found", rule: "{enabled: true}", extra: "resources: {index_file: README.md}\n", files: map[string]string{"mkdocs.yml": "nav: [dir/]\n", "docs/dir/README.md": "# Dir\n"}},
		{name: "custom-index-missing", rule: "{enabled: true}", extra: "resources: {index_file: README.md}\n", files: map[string]string{"mkdocs.yml": "nav: [dir/]\n", "docs/dir/index.md": "# Dir\n"}, status: 10, want: []md074Located{{"mkdocs.yml", 1, 7, 1, 11, 6, 10, false, md074Missing("dir/")}}},
		{name: "nested", rule: "{enabled: true}", files: map[string]string{"mkdocs.yml": "nav: [{Part: [{Sub: [missing.md]}]}]\n"}, status: 10, want: []md074Located{{"mkdocs.yml", 1, 22, 1, 32, 21, 31, false, md074Missing("missing.md")}}},
		{name: "omitted", rule: "{enabled: true, check_omitted: true}", files: map[string]string{"mkdocs.yml": "nav: [a.md]\nnot_in_nav: |\n  quiet.md\n", "docs/a.md": "# A\n", "docs/b.md": "# B\n", "docs/quiet.md": "# Quiet\n"}, status: 10, want: []md074Located{{"mkdocs.yml", 1, 6, 1, 12, 5, 11, false, "Page is not included in MkDocs navigation: b.md."}}},
		{name: "omitted-nav-absent", rule: "{enabled: true, check_omitted: true}", files: map[string]string{"mkdocs.yml": "site_name: X\n", "docs/a.md": "# A\n", "docs/b.md": "# B\n"}},
		{name: "omitted-nav-null", rule: "{enabled: true, check_omitted: true}", files: map[string]string{"mkdocs.yml": "nav: null\n", "docs/a.md": "# A\n", "docs/b.md": "# B\n"}},
		{name: "omitted-nav-empty", rule: "{enabled: true, check_omitted: true}", files: map[string]string{"mkdocs.yml": "nav: []\n", "docs/a.md": "# A\n", "docs/b.md": "# B\n"}, status: 10, want: []md074Located{
			{"mkdocs.yml", 1, 6, 1, 8, 5, 7, false, "Page is not included in MkDocs navigation: a.md."},
			{"mkdocs.yml", 1, 6, 1, 8, 5, 7, false, "Page is not included in MkDocs navigation: b.md."},
		}},
		{name: "omitted-dynamic", rule: "{enabled: true, check_omitted: true}", files: map[string]string{"mkdocs.yml": "hooks: [x.py]\nnav: [a.md]\n", "docs/a.md": "# A\n"}, status: 15, want: []md074Located{
			{"mkdocs.yml", 1, 8, 1, 14, -1, -1, true, "Dynamic MkDocs setting is unsupported: hooks"},
			{"mkdocs.yml", 2, 6, 2, 12, -1, -1, true, md074Omitted},
		}},
		{name: "missing-config", rule: "{enabled: true}", status: 15, want: []md074Located{{"mkdocs.yml", 0, 0, 0, 0, -1, -1, true, "MkDocs configuration is missing, unreadable or not a regular file: mkdocs.yml."}}},
		{name: "malformed", rule: "{enabled: true}", files: map[string]string{"mkdocs.yml": "nav: [\n"}, status: 15, want: []md074Located{{"mkdocs.yml", 1, 1, 2, 1, -1, -1, true, "Malformed MkDocs YAML."}}},
		{name: "inherit", rule: "{enabled: true}", files: map[string]string{"mkdocs.yml": "INHERIT: base.yml\nnav: [guide.md]\n", "base.yml": "nav: [x.md]\n", "docs/guide.md": "# Guide\n"}, status: 15, want: []md074Located{{"mkdocs.yml", 1, 10, 1, 18, -1, -1, true, "Dynamic MkDocs setting is unsupported: INHERIT"}}},
		{name: "hooks", rule: "{enabled: true}", files: map[string]string{"mkdocs.yml": "hooks: [x.py]\nnav: [guide.md]\n", "docs/guide.md": "# Guide\n"}, status: 15, want: []md074Located{{"mkdocs.yml", 1, 8, 1, 14, -1, -1, true, "Dynamic MkDocs setting is unsupported: hooks"}}},
		{name: "active-plugin", rule: "{enabled: true}", files: map[string]string{"mkdocs.yml": "plugins: [blog]\nnav: [missing.md]\n"}, status: 15, want: []md074Located{
			{"mkdocs.yml", 1, 11, 1, 15, -1, -1, true, "Active or malformed plugin has unknown navigation effects."},
			{"mkdocs.yml", 2, 7, 2, 17, -1, -1, true, "MkDocs navigation target is missing, but dynamic settings may generate it: missing.md."},
		}},
		{name: "search-plugin", rule: "{enabled: true}", files: map[string]string{"mkdocs.yml": "plugins: [search]\nnav: [missing.md]\n"}, status: 10, want: []md074Located{{"mkdocs.yml", 2, 7, 2, 17, 24, 34, false, md074Missing("missing.md")}}},
		{name: "subdirectory", rule: "{enabled: true, config_path: site/mkdocs.yml}", files: map[string]string{"site/mkdocs.yml": "docs_dir: pages\nnav: [guide.md, missing.md]\n", "site/pages/guide.md": "# Guide\n", "docs/missing.md": "# Elsewhere\n"}, status: 10, want: []md074Located{{"site/mkdocs.yml", 2, 17, 2, 27, 32, 42, false, md074Missing("missing.md")}}},
		// An empty selection keeps its usage error, and MD074 still runs.
		{name: "empty-selection", rule: "{enabled: true}", files: map[string]string{"mkdocs.yml": "nav: [missing.md]\n"}, args: []string{"docs"}, status: 11, want: []md074Located{{"mkdocs.yml", 1, 7, 1, 17, 6, 16, false, md074Missing("missing.md")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := md074Project(t, md074Configuration(tc.rule)+tc.extra, tc.files)
			before := configurationBytes(t, root)
			args := tc.args
			if args == nil {
				args = []string{"README.md"}
			}
			got := md014NativeRun(t, binary, root, append([]string{"lint", "--format=json"}, args...)...)
			out := md074JSON(t, got, root, "lint")
			if have := md074Collect(t, out); got.Status != tc.status || !reflect.DeepEqual(have, tc.want) {
				t.Fatal(got.Status, have, got.Stdout)
			}
			for _, file := range out.Files {
				if strings.HasSuffix(file.Path, "mkdocs.yml") {
					t.Fatal("configuration became a lint target", out.Files)
				}
			}
			for _, name := range out.CheckedFiles {
				if strings.HasSuffix(name, "mkdocs.yml") {
					t.Fatal("configuration became a checked file", out.CheckedFiles)
				}
			}
			if tc.status != 11 {
				md074LintOnlyRuns(t, binary, root, "README.md")
			}
			if after := configurationBytes(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("MD074 changed the fixture tree")
			}
		})
	}
}

// CRLF positions match the LF configuration; byte offsets include each CR.
func TestCompiledMD074CRLFConfiguration(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	lf := "site_name: Example\nnav:\n  - Guide: missing.md\n"
	var runs [2][]md074Located
	for i, config := range []string{lf, strings.ReplaceAll(lf, "\n", "\r\n")} {
		root := md074Project(t, md074Configuration("{enabled: true}"), map[string]string{"mkdocs.yml": config})
		got := md014NativeRun(t, binary, root, "lint", "--format=json", "README.md")
		runs[i] = md074Collect(t, md074JSON(t, got, root, "lint"))
		if got.Status != 10 || len(runs[i]) != 1 {
			t.Fatal(got.Status, got.Stdout)
		}
	}
	want := []md074Located{{"mkdocs.yml", 3, 12, 3, 22, 35, 45, false, md074Missing("missing.md")}}
	if !reflect.DeepEqual(runs[0], want) {
		t.Fatal("LF", runs[0])
	}
	want[0].byteStart, want[0].byteEnd = 37, 47
	if !reflect.DeepEqual(runs[1], want) {
		t.Fatal("CRLF", runs[1])
	}
}

// Markdown directives cannot suppress MD074: the finding stays a diagnostic
// and the directive reports that it does not control the rule.
func TestCompiledMD074DirectivesDoNotSuppress(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	conf := strings.Replace(md074Configuration("{enabled: true}"), "directives: {enabled: false}\n", "", 1)
	root := md074Project(t, conf, map[string]string{"mkdocs.yml": "nav: [{Guide: missing.md}]\n"})
	putConfigurationFile(t, root, "README.md", "# Readme\n\n<!-- mdchk:disable MD074 -->\n")
	got := md014NativeRun(t, binary, root, "lint", "--format=json", "README.md")
	out := md074JSON(t, got, root, "lint")
	want := []md074Located{{"mkdocs.yml", 1, 15, 1, 25, 14, 24, false, md074Missing("missing.md")}}
	if have := md074Collect(t, out); got.Status != 10 || !reflect.DeepEqual(have, want) || len(out.SuppressedDiagnostics) != 0 {
		t.Fatal(got.Status, have, got.Stdout)
	}
	inapplicable := false
	for _, raw := range out.Notes {
		var note result.Note
		if err := json.Unmarshal(raw, &note); err != nil {
			t.Fatal(err)
		}
		inapplicable = inapplicable || note.Code == "directive-inapplicable"
	}
	if !inapplicable {
		t.Fatal("no directive-inapplicable note", out.Notes)
	}
}

// The GitHub and VS Code formats report the finding at the configuration.
func TestCompiledMD074EditorFormats(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	root := md074Project(t, md074Configuration("{enabled: true}"), map[string]string{"mkdocs.yml": "nav: [{Guide: missing.md}]\n"})
	for format, want := range map[string]string{
		"github": "::error file=mkdocs.yml,line=1,title=MD074::MkDocs navigation target does not exist: missing.md.\n",
		"vscode": "mkdocs.yml:1:15: error: MD074: MkDocs navigation target does not exist\\x3a missing.md.\n",
	} {
		got := md014NativeRun(t, binary, root, "lint", "--format="+format, "README.md")
		if got.Status != 10 || !strings.Contains(got.Stdout, want) {
			t.Fatal(format, got.Status, got.Stdout)
		}
	}
}

// Disabled MD074 opens no MkDocs context: an unreadable or invalid
// configuration leaves the output identical to a project without one.
func TestCompiledMD074DisabledReadsNothing(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	run := func(files map[string]string, prepare func(root string)) configurationRun {
		root := md074Project(t, md074Configuration("{enabled: false}"), files)
		if prepare != nil {
			prepare(root)
		}
		return md014NativeRun(t, binary, root, "lint", "--format=json", "README.md")
	}
	base := run(nil, nil)
	if base.Status != 0 {
		t.Fatal(base.Status, base.Stdout)
	}
	for name, got := range map[string]configurationRun{
		"missing-target": run(map[string]string{"mkdocs.yml": "nav: [missing.md]\n"}, nil),
		"malformed":      run(map[string]string{"mkdocs.yml": "nav: [\n"}, nil),
		"unreadable": run(map[string]string{"mkdocs.yml": "nav: [missing.md]\n"}, func(root string) {
			if runtime.GOOS == "windows" || os.Geteuid() == 0 {
				return
			}
			if err := os.Chmod(filepath.Join(root, "mkdocs.yml"), 0o000); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "mkdocs.yml"), 0o600) })
		}),
	} {
		if got.Status != base.Status || got.Stdout != base.Stdout {
			t.Fatal(name, got.Status, got.Stdout)
		}
	}
}

// config validate applies the config_path domain and the shared rule schema.
func TestCompiledMD074ConfigurationValidation(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for rule, status := range map[string]int{
		`{enabled: true, config_path: "../mkdocs.yml"}`:                  12,
		`{enabled: true, config_path: "/abs/mkdocs.yml"}`:                12,
		`{enabled: true, config_path: ""}`:                               12,
		`{enabled: true, config_path: "https://example.com/mkdocs.yml"}`: 12,
		`{enabled: true, check_omitted: "yes"}`:                          12,
		`{bogus: 1}`:                                                     12,
		`{enabled: true}`:                                                0,
		`{enabled: true, config_path: site/mkdocs.yml, check_omitted: true, check_absolute: true}`: 0,
	} {
		root := md074Project(t, md074Configuration(rule), nil)
		got := md014NativeRun(t, binary, root, "config", "validate", "--format=json")
		out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "config validate")
		if got.Status != status {
			t.Fatal(rule, got.Status, got.Stdout)
		}
		if status == 12 && (len(out.Errors) == 0 || out.Errors[0].Path != ".mdchk.yaml" || out.Errors[0].Range == nil) {
			t.Fatal("unlocated configuration error", rule, got.Stdout)
		}
		lint := md014NativeRun(t, binary, root, "lint", "--format=json", "README.md")
		if status == 12 && lint.Status != 12 || status == 0 && lint.Status == 12 {
			t.Fatal("lint", rule, lint.Status, lint.Stdout)
		}
	}
}

// Every reusable rule fixture produces its expected records through the
// compiled CLI, and no command changes any fixture byte.
func TestCompiledMD074ProjectFixtures(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	base := filepath.Join("..", "..", "internal", "rules", "testdata", "md074")
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source := filepath.Join(base, name)
			raw, err := os.ReadFile(filepath.Join(source, "expected.json")) //nolint:gosec // Read only the checked-in fixture tree.
			if err != nil {
				t.Fatal(err)
			}
			var expected struct {
				Options struct {
					ConfigPath    string `json:"config_path"`
					CheckAbsolute bool   `json:"check_absolute"`
					CheckOmitted  bool   `json:"check_omitted"`
				} `json:"options"`
				IndexFile   string `json:"index_file"`
				Diagnostics []struct {
					Message     string `json:"message"`
					StartLine   int    `json:"start_line"`
					StartColumn int    `json:"start_column"`
					EndLine     int    `json:"end_line"`
					EndColumn   int    `json:"end_column"`
					ByteStart   int    `json:"byte_start"`
					ByteEnd     int    `json:"byte_end"`
				} `json:"diagnostics"`
				Coverage []struct {
					Message string `json:"message"`
				} `json:"coverage"`
			}
			if err := json.Unmarshal(raw, &expected); err != nil {
				t.Fatal(err)
			}
			rule, _ := json.Marshal(map[string]any{"enabled": true, "config_path": expected.Options.ConfigPath, "check_absolute": expected.Options.CheckAbsolute, "check_omitted": expected.Options.CheckOmitted})
			extra := ""
			if expected.IndexFile != "" {
				extra = "resources: {index_file: " + expected.IndexFile + "}\n"
			}
			files := map[string]string{}
			err = filepath.WalkDir(source, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				data, err := os.ReadFile(path) //nolint:gosec // Read only the checked-in fixture tree.
				rel, _ := filepath.Rel(source, path)
				files[filepath.ToSlash(rel)] = string(data)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			original := configurationBytes(t, source)
			root := md074Project(t, md074Configuration(string(rule))+extra, files)
			before := configurationBytes(t, root)
			got := md014NativeRun(t, binary, root, "lint", "--format=json", "README.md")
			out := md074JSON(t, got, root, "lint")
			var diagnostics, coverage []string
			for _, d := range expected.Diagnostics {
				diagnostics = append(diagnostics, strings.Join([]string{d.Message, jsonInts(d.StartLine, d.StartColumn, d.EndLine, d.EndColumn, d.ByteStart, d.ByteEnd)}, " "))
			}
			for _, c := range expected.Coverage {
				coverage = append(coverage, c.Message)
			}
			var gotDiagnostics, gotCoverage []string
			for _, l := range md074Collect(t, out) {
				if l.coverage {
					gotCoverage = append(gotCoverage, l.message)
				} else {
					gotDiagnostics = append(gotDiagnostics, strings.Join([]string{l.message, jsonInts(l.startLine, l.startCol, l.endLine, l.endCol, l.byteStart, l.byteEnd)}, " "))
				}
			}
			status := 0
			if len(diagnostics) > 0 {
				status = 10
			}
			if len(coverage) > 0 {
				status = 15
			}
			if got.Status != status || !reflect.DeepEqual(gotDiagnostics, diagnostics) || !reflect.DeepEqual(gotCoverage, coverage) {
				t.Fatal(got.Status, gotDiagnostics, gotCoverage, got.Stdout)
			}
			md074LintOnlyRuns(t, binary, root, "README.md")
			if after := configurationBytes(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("lint, fix or inspect changed the fixture project")
			}
			if after := configurationBytes(t, source); !reflect.DeepEqual(original, after) {
				t.Fatal("the checked-in fixture changed")
			}
		})
	}
}

// md074JSON validates a report against the closed schema and the shared
// semantic checks. MD074 diagnostics name the MkDocs configuration, which is
// not a lint target, so the target-only diagnostic check does not apply; the
// snapshot of every located record must still match the fixture bytes.
func md074JSON(t *testing.T, run configurationRun, root, operation string) result.Result {
	t.Helper()
	run.Result = validateConfigurationOperationJSON(t, []byte(run.Stdout), operation)
	b, err := os.ReadFile("../../docs/contracts/results/result.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema, wire any
	if err = json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(run.Stdout), &wire); err != nil {
		t.Fatal(err)
	}
	if err = md014SchemaCheck(schema, schema, wire); err != nil {
		t.Fatal("closed normative result schema", err, run.Stdout)
	}
	if operation != "inspect" {
		checkConfigurationLinks(t, root, run)
	}
	return run.Result
}

func jsonInts(values ...int) string {
	data, _ := json.Marshal(values)
	return string(data)
}

// Projection shares the operation budget: a configuration whose record
// positions exceed the work limit is interrupted and incomplete, never a
// complete result with every finding.
func TestCompiledMD074ProjectionHonorsOperationBudget(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	var config strings.Builder
	config.WriteString("# " + strings.Repeat("x", 100000) + "\nnav:\n")
	for i := range 3000 {
		config.WriteString("  - m" + strconv.Itoa(i) + ".md\n")
	}
	root := md074Project(t, md074Configuration("{enabled: true}"), map[string]string{"mkdocs.yml": config.String()})
	got := md014NativeRun(t, binary, root, "lint", "--format=json", "README.md")
	out := md074JSON(t, got, root, "lint")
	limited, coverage := false, false
	for _, c := range out.Coverage {
		coverage = coverage || c.Path == "mkdocs.yml" && c.Rule == "MD074" && c.Reason == "limit-exceeded"
	}
	for _, stop := range out.Interruptions {
		limited = limited || stop.Path == "mkdocs.yml" && stop.Reason == "limit-exceeded"
	}
	if got.Status != 15 || out.Complete || !limited || !coverage || len(out.Diagnostics) >= 3000 {
		t.Fatal(got.Status, out.Complete, out.Interruptions, len(out.Diagnostics))
	}
}
