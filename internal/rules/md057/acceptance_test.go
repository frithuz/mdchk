package md057_test

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
	"github.com/frithuz/mdchk/internal/result"
)

// TestMain removes this process's private builds once every test has finished
// with them.
func TestMain(m *testing.M) {
	m.Run()
	acceptance.Cleanup()
}

type md057Observed struct {
	path                         string
	startLine, startCol, endLine int
	endCol, byteStart, byteEnd   int
	coverage                     bool
	message                      string
}

func md057Collect(t *testing.T, out result.Result) []md057Observed {
	t.Helper()
	var got []md057Observed
	for _, d := range out.Diagnostics {
		if d.Rule == "MD057" {
			if d.FixAvailable {
				t.Fatal("MD057 offered a fix", d)
			}
			got = append(got, md057Observed{d.Path, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column, d.ByteRange.Start, d.ByteRange.End, false, d.Message})
		}
	}
	for _, c := range out.Coverage {
		if c.Rule == "MD057" {
			if c.Range == nil || c.Reason != "context-unavailable" {
				t.Fatal("coverage without a source range", c)
			}
			got = append(got, md057Observed{c.Path, c.Range.Start.Line, c.Range.Start.Column, c.Range.End.Line, c.Range.End.Column, -1, -1, true, c.Message})
		}
	}
	return got
}

func md057Configuration(rule string) string {
	return strings.Replace(acceptance.CleanConfiguration(), "MD057: {enabled: false}", "MD057: "+rule, 1)
}

func md057Project(t *testing.T, conf string) string {
	t.Helper()
	root := acceptance.Project(t)
	acceptance.PutFile(t, root, ".mdchk.yaml", conf)
	acceptance.PutFile(t, root, "docs/guide.md", "# Guide\n")
	acceptance.PutFile(t, root, "docs/logo.png", "\xff\x00\xfe")
	acceptance.PutFile(t, root, "docs/sub/index.md", "# Sub\n")
	acceptance.PutFile(t, root, "docs/bare/README.md", "# Bare\n")
	acceptance.PutFile(t, root, "docs/sub2/index.md/index.md", "# Nested\n")
	return root
}

const (
	md057Missing  = "Local link target does not exist."
	md057Hint     = "Local link target is a file, but the destination requires a directory."
	md057NoIndex  = "Directory has no configured index file."
	md057IndexDir = "Directory index path is a directory, not a file."
	md057Context  = "Required resource context is unavailable."
)

// These rows run the compiled CLI over native fixtures through lint, inspect
// and fix --dry-run: the shared resource service and strict no-follow reader
// decide every outcome, and no command creates or changes a file.
func TestCompiledPlatformMD057MissingLocalTargets(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, tc := range []struct {
		name, rule, source, extra string
		target                    string
		status                    int
		want                      []md057Observed
		prepare                   func(t *testing.T, root string)
	}{
		{name: "missing", rule: "{}", source: "[Guide](missing.md)\n", status: 10, want: []md057Observed{{"docs/self.md", 1, 9, 1, 19, 8, 18, false, md057Missing}}},
		{name: "found", rule: "{}", source: "[Guide](guide.md)\n"},
		{name: "image-missing", rule: "{}", source: "![logo](missing.png)\n", status: 10, want: []md057Observed{{"docs/self.md", 1, 9, 1, 20, 8, 19, false, md057Missing}}},
		{name: "image-check-off", rule: "{check_images: false}", source: "![logo](missing.png)\n"},
		{name: "image-existing-binary", rule: "{}", source: "![logo](logo.png)\n"},
		{name: "fragment-on-missing-file", rule: "{}", source: "[x](missing.md#intro) [y](guide.md#%ZZ)\n", status: 10, want: []md057Observed{{"docs/self.md", 1, 5, 1, 21, 4, 20, false, md057Missing}}},
		{name: "ignored", rule: "{}", source: "[a](https://example.test/a.md) [b](//host/a.md) [c](#top) [d]() `[e](missing.md)`\n\n<!-- [f](missing.md) -->\n\n```\n[g](missing.md)\n```\n"},
		{name: "directories", rule: "{}", source: "[a](sub/) [b](bare/) [c](guide.md/) [d](sub2/)\n", status: 10, want: []md057Observed{
			{"docs/self.md", 1, 15, 1, 20, 14, 19, false, md057NoIndex},
			{"docs/self.md", 1, 26, 1, 35, 25, 34, false, md057Hint},
			{"docs/self.md", 1, 41, 1, 46, 40, 45, false, md057IndexDir},
		}},
		{name: "index-file", rule: "{}", extra: "resources: {index_file: README.md}\n", source: "[b](bare/) [a](sub/)\n", status: 10, want: []md057Observed{{"docs/self.md", 1, 16, 1, 20, 15, 19, false, md057NoIndex}}},
		// The project root takes the single index attempt like any directory.
		{name: "project-root", rule: "{}", source: "[a](../) [b](..) [c](./)\n", status: 10, want: []md057Observed{
			{"docs/self.md", 1, 5, 1, 8, 4, 7, false, md057NoIndex},
			{"docs/self.md", 1, 14, 1, 16, 13, 15, false, md057NoIndex},
			{"docs/self.md", 1, 22, 1, 24, 21, 23, false, md057NoIndex},
		}},
		{name: "project-root-from-root", rule: "{}", source: "[a](.) [b](./) [c](docs/)\n", target: "top.md", status: 10, want: []md057Observed{
			{"top.md", 1, 5, 1, 6, 4, 5, false, md057NoIndex},
			{"top.md", 1, 12, 1, 14, 11, 13, false, md057NoIndex},
			{"top.md", 1, 20, 1, 25, 19, 24, false, md057NoIndex},
		}},
		{name: "project-root-with-index", rule: "{}", source: "[a](../) [b](..)\n", prepare: func(t *testing.T, root string) {
			acceptance.PutFile(t, root, "index.md", "# Project\n")
		}},
		{name: "reference-crlf", rule: "{}", source: "# Self\r\n\r\n[x][r]\r\n\r\n[r]: missing.md\r\n", status: 10, want: []md057Observed{{"docs/self.md", 3, 1, 3, 7, 10, 16, false, md057Missing}}},
		{name: "containers", rule: "{}", source: "- [a](missing.md)\n\n> > [b](missing.md)\n", status: 10, want: []md057Observed{
			{"docs/self.md", 1, 7, 1, 17, 6, 16, false, md057Missing},
			{"docs/self.md", 3, 9, 3, 19, 27, 37, false, md057Missing},
		}},
		{name: "root-escape", rule: "{}", source: "[x](../../outside.md)\n", status: 15, want: []md057Observed{{"docs/self.md", 1, 5, 1, 21, -1, -1, true, md057Context}}},
		{name: "symlink-blocked", rule: "{}", source: "[x](linked.md)\n", status: 15, want: []md057Observed{{"docs/self.md", 1, 5, 1, 14, -1, -1, true, md057Context}}, prepare: func(t *testing.T, root string) {
			if runtime.GOOS == "windows" {
				t.Skip("symbolic link creation needs privileges")
			}
			if err := os.Symlink("guide.md", filepath.Join(root, "docs", "linked.md")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := md057Project(t, md057Configuration(tc.rule)+tc.extra)
			target := tc.target
			if target == "" {
				target = "docs/self.md"
			}
			acceptance.PutFile(t, root, target, tc.source)
			if tc.prepare != nil {
				tc.prepare(t, root)
			}
			before := acceptance.ConfigurationBytes(t, root)
			got := acceptance.MD014NativeRun(t, binary, root, "lint", target, "--format=json")
			out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "lint")
			if have := md057Collect(t, out); got.Status != tc.status || !reflect.DeepEqual(have, tc.want) || len(out.Errors) != 0 {
				t.Fatal(got.Status, have, got.Stdout)
			}
			seen, _ := acceptance.RunInspection(t, binary, root, root, "inspect", target, "--format=json")
			if have := md057Collect(t, seen.Result); len(have) != len(tc.want) {
				t.Fatal("inspect", have, tc.want)
			}
			dry := acceptance.MD014NativeRun(t, binary, root, "fix", "--dry-run", target, "--format=json")
			fixed := acceptance.MD014NativeJSON(t, []byte(dry.Stdout), "fix")
			if have := md057Collect(t, fixed); len(have) != len(tc.want) || len(fixed.Changes)+len(fixed.Documents) != 0 {
				t.Fatal("fix --dry-run", have, dry.Stdout)
			}
			applied := acceptance.MD014NativeRun(t, binary, root, "fix", target, "--format=json")
			if out := acceptance.MD014NativeJSON(t, []byte(applied.Stdout), "fix"); len(out.Changes) != 0 {
				t.Fatal("fix changed a file", applied.Stdout)
			}
			if after := acceptance.ConfigurationBytes(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("MD057 lookups changed the fixture tree")
			}
		})
	}
}

// An unreadable directory is an operational failure, not absence: one located
// coverage record and one attributed io error.
func TestCompiledPlatformMD057UnreadableDirectory(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions are not enforced for this user")
	}
	binary := acceptance.SharedExecutable(t)
	root := md057Project(t, md057Configuration("{}"))
	acceptance.PutFile(t, root, "docs/self.md", "[x](locked/a.md) [y](locked/a.md#b)\n")
	before := acceptance.ConfigurationBytes(t, root)
	locked := filepath.Join(root, "docs", "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	got := acceptance.MD014NativeRun(t, binary, root, "lint", "docs/self.md", "--format=json")
	if err := os.Chmod(locked, 0o700); err != nil { //nolint:gosec // Restore the synthetic directory before hashing the tree.
		t.Fatal(err)
	}
	out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "lint")
	have := md057Collect(t, out)
	if got.Status != 13 || len(have) != 2 || !have[0].coverage || !have[1].coverage || len(out.Errors) != 1 || out.Errors[0].Category != result.IO {
		t.Fatal(got.Status, have, got.Stdout)
	}
	for _, c := range out.Coverage {
		if c.Rule == "MD057" && (len(c.CauseIDs) == 0 || c.CauseIDs[0] != out.Errors[0].ID) {
			t.Fatal("coverage is not attributed to the io error", c)
		}
	}
	if err := os.Remove(locked); err != nil {
		t.Fatal(err)
	}
	if after := acceptance.ConfigurationBytes(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("MD057 lookups changed the fixture tree")
	}
}

// MD057 owns missing documents; MD051 never reports them. Disabling either
// rule leaves the other unchanged, and both share one resource session.
func TestCompiledPlatformMD057AndMD051Ownership(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, tc := range []struct {
		name, md051, md057 string
		md051Findings      int
		md057Findings      int
	}{
		{"both", "{cross_file: true}", "{}", 1, 1},
		{"md057-disabled", "{cross_file: true}", "{enabled: false}", 1, 0},
		{"md051-disabled", "{enabled: false}", "{}", 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conf := strings.Replace(md057Configuration(tc.md057), "MD051: {enabled: false}", "MD051: "+tc.md051, 1)
			root := md057Project(t, conf)
			acceptance.PutFile(t, root, "docs/self.md", "# Self\n\n[x](gone.md#intro) [y](guide.md#nope)\n")
			got := acceptance.MD014NativeRun(t, binary, root, "lint", "docs/self.md", "--format=json")
			out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "lint")
			count := map[string]int{}
			for _, d := range out.Diagnostics {
				count[d.Rule]++
				if d.Rule == "MD057" && d.Message != md057Missing {
					t.Fatal(d)
				}
			}
			if count["MD051"] != tc.md051Findings || count["MD057"] != tc.md057Findings || len(out.Coverage)+len(out.Errors) != 0 {
				t.Fatal(count, got.Stdout)
			}
		})
	}
	t.Run("missing-document-belongs-to-md057", func(t *testing.T) {
		root := md057Project(t, strings.Replace(md057Configuration("{}"), "MD051: {enabled: false}", "MD051: {cross_file: true}", 1))
		acceptance.PutFile(t, root, "docs/self.md", "# Self\n\n[x](gone.md#intro)\n")
		got := acceptance.MD014NativeRun(t, binary, root, "lint", "docs/self.md", "--format=json")
		out := acceptance.MD014NativeJSON(t, []byte(got.Stdout), "lint")
		if have := md057Collect(t, out); got.Status != 10 || len(out.Diagnostics) != 1 || len(have) != 1 || have[0].message != md057Missing {
			t.Fatal(got.Status, got.Stdout)
		}
	})
}
