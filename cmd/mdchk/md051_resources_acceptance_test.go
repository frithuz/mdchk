package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

// These rows run the compiled CLI over native fixtures: the shared resource
// service, strict no-follow reader and fragment resolver decide each outcome.
func TestCompiledPlatformMD051ResourceFragments(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		name, crossFile, link string
		findings, coverage    int
		errors, symlink       bool
		prepare               func(t *testing.T, root string)
	}{
		{name: "found", crossFile: "true", link: "[x](other.md#intro)"},
		{name: "found-query", crossFile: "true", link: "[x](other.md?v=1#intro)"},
		{name: "absent", crossFile: "true", link: "[x](other.md#nope)", findings: 1},
		{name: "case-sensitive", crossFile: "true", link: "[x](other.md#Intro)", findings: 1},
		{name: "percent-decoded", crossFile: "true", link: "[x](other.md#caf%C3%A9)"},
		{name: "malformed-fragment", crossFile: "true", link: "[x](other.md#%ZZ)", findings: 1},
		{name: "empty-fragment", crossFile: "true", link: "[x](other.md#)"},
		{name: "missing-document-belongs-to-md057", crossFile: "true", link: "[x](gone.md#intro)"},
		{name: "directory-index-found", crossFile: "true", link: "[x](sub/#top)"},
		{name: "directory-index-absent", crossFile: "true", link: "[x](sub/#nope)", findings: 1},
		{name: "directory-without-index", crossFile: "true", link: "[x](bare/#top)"},
		{name: "self-path-found", crossFile: "true", link: "[x](./self.md#self)"},
		{name: "self-path-absent", crossFile: "true", link: "[x](./self.md#nope)", findings: 1},
		{name: "self-path-without-cross-file", crossFile: "false", link: "[x](self.md#nope)", findings: 1},
		{name: "self-path-found-without-cross-file", crossFile: "false", link: "[x](../docs/self.md#self)"},
		{name: "self-directory-index", crossFile: "false", link: "[x](./#self)", prepare: func(t *testing.T, root string) {
			putConfigurationFile(t, root, "docs/index.md", "# Self\n\n[x](./#self) [y](./#nope)\n")
		}, findings: 0},
		{name: "cross-file-off-skips-other-documents", crossFile: "false", link: "[x](other.md#nope)"},
		{name: "invalid-utf8-document", crossFile: "true", link: "[x](broken.md#intro)", coverage: 1, errors: true},
		{name: "root-escape", crossFile: "true", link: "[x](../../outside.md#intro)", coverage: 1},
		{name: "symlink-blocked", crossFile: "true", link: "[x](linked.md#intro)", coverage: 1, symlink: true, prepare: func(t *testing.T, root string) {
			if runtime.GOOS == "windows" {
				t.Skip("symbolic link creation needs privileges")
			}
			if err := os.Symlink("other.md", filepath.Join(root, "docs", "linked.md")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "non-markdown-target-unavailable", crossFile: "true", link: "[x](notes.txt#intro)", coverage: 1},
		{name: "over-long-path-is-skipped-per-link", crossFile: "false", link: "[x](" + strings.Repeat("a", 5000) + ".md#x)"},
		{name: "remote-ignored", crossFile: "true", link: "[x](https://example.test/a.md#nope)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(cleanConfiguration(), "MD051: {enabled: false}", "MD051: {cross_file: "+tc.crossFile+"}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "docs/self.md", "# Self\n\n"+tc.link+"\n")
			putConfigurationFile(t, root, "docs/other.md", "# Intro\n\n# Café\n")
			putConfigurationFile(t, root, "docs/sub/index.md", "# Top\n")
			putConfigurationFile(t, root, "docs/bare/readme.md", "# Top\n")
			putConfigurationFile(t, root, "docs/notes.txt", "# Intro\n")
			putConfigurationFile(t, root, "docs/broken.md", "# Intro \xff\n")
			if tc.prepare != nil {
				tc.prepare(t, root)
			}
			var before map[string]readinessEntry
			if !tc.symlink {
				before = readinessState(t, root)
			}
			got := md014NativeRun(t, binary, root, "lint", "docs/self.md", "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "lint")
			findings, coverage := 0, 0
			for _, d := range out.Diagnostics {
				if d.Rule == "MD051" {
					findings++
				}
			}
			for _, c := range out.Coverage {
				if c.Rule == "MD051" {
					coverage++
					if c.Reason != "context-unavailable" || c.Range == nil {
						t.Fatal(c)
					}
				}
			}
			if findings != tc.findings || coverage != tc.coverage || (len(out.Errors) != 0) != tc.errors {
				t.Fatal(findings, coverage, out.Errors, got.Stdout)
			}
			if !tc.symlink {
				readinessAssertEqual(t, "resource lookups are read-only", before, readinessState(t, root))
			}
			if tc.symlink {
				if data, err := os.ReadFile(filepath.Join(root, "docs", "other.md")); err != nil || string(data) != "# Intro\n\n# Café\n" {
					t.Fatal(err)
				}
			}
		})
	}
}

type md051Located struct {
	path                           string
	startLine, startCol, endLine   int
	endCol, byteStart, byteEnd     int
	coverageNotDiagnostic, private bool
}

func md051Collect(t *testing.T, out result.Result) []md051Located {
	t.Helper()
	var got []md051Located
	for _, d := range out.Diagnostics {
		if d.Rule == "MD051" {
			got = append(got, md051Located{d.Path, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column, d.ByteRange.Start, d.ByteRange.End, false, false})
		}
	}
	for _, c := range out.Coverage {
		if c.Rule == "MD051" {
			if c.Range == nil {
				t.Fatal("coverage without a source range", c)
			}
			got = append(got, md051Located{c.Path, c.Range.Start.Line, c.Range.Start.Column, c.Range.End.Line, c.Range.End.Column, -1, -1, true, false})
		}
	}
	return got
}

func md051Project(t *testing.T, crossFile string) string {
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", strings.Replace(cleanConfiguration(), "MD051: {enabled: false}", "MD051: {cross_file: "+crossFile+"}", 1))
	putConfigurationFile(t, root, "top.md", "# Top\n")
	putConfigurationFile(t, root, "docs/other.md", "# Intro\n")
	putConfigurationFile(t, root, "docs/broken.md", "# Intro \xff\n")
	putConfigurationFile(t, root, "docs/sub/index.md", "# Top \xff\n")
	return root
}

// Reports keep the invocation-relative path while resolution uses the bound
// root-relative key, so the same document yields identical facts from any cwd.
func TestCompiledPlatformMD051ResourceFragmentsFromSubdirectory(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	const source = "# Self\n\n[a](other.md#nope) [b](../top.md#nope) [c](../top.md#top) [d](../gone.md#x) [e](broken.md#x)\n"
	for _, tc := range []struct {
		name, cwd, target, path string
	}{
		{"root", "", "docs/self.md", "docs/self.md"},
		{"subdirectory", "docs", "self.md", "self.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := md051Project(t, "true")
			putConfigurationFile(t, root, "docs/self.md", source)
			cwd := root
			if tc.cwd != "" {
				cwd = filepath.Join(root, tc.cwd)
			}
			before := readinessState(t, root)
			got := md014NativeRun(t, binary, cwd, "lint", tc.target, "--format=json")
			out := md014NativeJSON(t, []byte(got.Stdout), "lint")
			want := []md051Located{
				{tc.path, 3, 5, 3, 18, 12, 25, false, false},
				{tc.path, 3, 24, 3, 38, 31, 45, false, false},
				{tc.path, 3, 81, 3, 92, -1, -1, true, false},
			}
			have := md051Collect(t, out)
			if len(have) != len(want) {
				t.Fatal(have, got.Stdout)
			}
			for i := range want {
				if have[i] != want[i] {
					t.Fatalf("%d: %+v != %+v", i, have[i], want[i])
				}
			}
			readinessAssertEqual(t, "subdirectory lookups are read-only", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD051ResourceFragmentsCRLFRanges(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := md051Project(t, "true")
	putConfigurationFile(t, root, "docs/self.md", "# Self\r\n\r\n[x](other.md#nope) [y](broken.md#a)\r\n")
	got := md014NativeRun(t, binary, root, "lint", "docs/self.md", "--format=json")
	out := md014NativeJSON(t, []byte(got.Stdout), "lint")
	have := md051Collect(t, out)
	want := []md051Located{
		{"docs/self.md", 3, 5, 3, 18, 14, 27, false, false},
		{"docs/self.md", 3, 24, 3, 35, -1, -1, true, false},
	}
	if len(have) != 2 || have[0] != want[0] || have[1] != want[1] {
		t.Fatal(have, got.Stdout)
	}
}

func TestCompiledPlatformMD051ResourceFragmentsContainersReferencesAndQueries(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := md051Project(t, "true")
	putConfigurationFile(t, root, "docs/self.md", "# Self\n\n> [x](other.md#nope)\n\n- [y](other.md#nope)\n\n[z][r] [w]\n\n[r]: other.md#nope\n[w]: other.md#intro\n\n[q](self.md?v=1#nope) [s](self.md?v=1#self) [t](?v=1#nope)\n")
	got := md014NativeRun(t, binary, root, "lint", "docs/self.md", "--format=json")
	out := md014NativeJSON(t, []byte(got.Stdout), "lint")
	have := md051Collect(t, out)
	want := []md051Located{
		{"docs/self.md", 3, 7, 3, 20, 14, 27, false, false},
		{"docs/self.md", 5, 7, 5, 20, 36, 49, false, false},
		{"docs/self.md", 7, 1, 7, 7, 52, 58, false, false},
		{"docs/self.md", 12, 5, 12, 21, 108, 124, false, false},
		{"docs/self.md", 12, 49, 12, 58, 152, 161, false, false},
	}
	if len(have) != len(want) {
		t.Fatal(have, got.Stdout)
	}
	for i := range want {
		if have[i] != want[i] {
			t.Fatalf("%d: %+v != %+v", i, have[i], want[i])
		}
	}
}

func TestCompiledPlatformMD051ResourceFragmentsMultipleLinks(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := md051Project(t, "true")
	putConfigurationFile(t, root, "docs/self.md", "# Self\n\n[a](sub/#x) [b](sub/#y) [c](broken.md#x) [d](broken.md#y) [e](other.md#intro)\n")
	got := md014NativeRun(t, binary, root, "lint", "docs/self.md", "--format=json")
	out := md014NativeJSON(t, []byte(got.Stdout), "lint")
	have := md051Collect(t, out)
	coverage, findings := 0, 0
	for _, h := range have {
		if h.coverageNotDiagnostic {
			coverage++
		} else {
			findings++
		}
	}
	// The directory index and the broken document are each read once, yet every
	// affected link keeps its own located coverage record.
	if findings != 0 || coverage != 4 || len(out.Errors) == 0 {
		t.Fatal(have, out.Errors, got.Stdout)
	}
	seen := map[[2]int]bool{}
	for _, h := range have {
		seen[[2]int{h.startLine, h.startCol}] = true
	}
	if len(seen) != 4 {
		t.Fatal("coverage ranges are not distinct", have)
	}
}
