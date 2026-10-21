package gitignore

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
)

func testBudget(t *testing.T, work int64) *parser.OperationBudget {
	t.Helper()
	limits := parser.DefaultLimits()
	if work > 0 {
		limits.WorkUnits = work
	}
	b, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestNativeGitPatterns(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("native Git oracle unavailable")
	}
	cases := []struct {
		pattern string
		names   []string
		fold    bool
	}{
		{"*.md\n", []string{"a.md", "sub/a.md", "a.txt", "é.md"}, false},
		{"/foo\n", []string{"foo", "foo/a", "sub/foo", "foobar"}, false},
		{"abc/**/def\n", []string{"abc/def", "abc/x/def", "abc/x/y/def", "abc/xdef", "x/abc/def"}, false},
		{"**/foo\n", []string{"foo", "x/foo", "x/y/foo", "xfoo"}, false},
		{"abc/**\n", []string{"abc/x", "abc/x/y", "abcd/x"}, false},
		{"a**b\n", []string{"ab", "axyb", "a/x/b"}, false},
		{"cache/\n", []string{"cache/a", "x/cache/a", "cache.txt"}, false},
		{"*.md\n!keep.md\n", []string{"a.md", "keep.md", "sub/keep.md"}, false},
		{"[[:digit:]][a-c]?.md\n", []string{"1ax.md", "9cz.md", "xax.md", "1dx.md"}, false},
		{"[!a-c].md\n", []string{"d.md", "a.md", "é.md"}, false},
		{"\\#hash\n\\!bang\nspace\\ \ntrim   \n", []string{"#hash", "!bang", "space ", "space", "trim", "trim "}, false},
		{"foo\\bar\n", []string{"foobar", "foo\\bar"}, false},
		{"*.MD\r\n", []string{"a.md", "a.MD", "é.Md"}, true},
		{"[[:lower:]].md\n", []string{"a.md", "A.md", "1.md"}, true},
		{"[[:upper:]].md\n", []string{"a.md", "A.md", "1.md"}, true},
		{"[[:space:]].md\n", []string{"\n.md", "\t.md", " .md", "a.md"}, false},
		{"É.md\n", []string{"É.md", "é.md", "e.md"}, true},
		{"[\xff].md\n", []string{"\xff.md", "a.md"}, false},
		{"\xef\xbb\xbf*.md\n", []string{"a.md", "x/a.md", "a.txt"}, false},
		{"a/**/b/**/c\n", []string{"a/b/c", "a/x/b/y/c", "a/xb/c", "a/b/xc"}, false},
		{"[]a].md\n", []string{"].md", "a.md", "b.md"}, false},
		{"[a-c].md\n", []string{"a.md", "B.md", "D.md"}, true},
	}
	for _, tc := range cases {
		t.Run(strings.ReplaceAll(tc.pattern, "\n", ";"), func(t *testing.T) {
			if runtime.GOOS == "windows" && !utf8.ValidString(tc.pattern) {
				t.Skip("Windows argument conversion cannot preserve invalid UTF-8 bytes")
			}
			root := t.TempDir()
			home := t.TempDir()
			env := []string{"HOME=" + home, "XDG_CONFIG_HOME=" + home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + filepath.Join(home, "absent"), "PATH=" + os.Getenv("PATH"), "LC_ALL=C"}
			run := func(args ...string) *exec.Cmd {
				cmd := exec.Command(git, args...) //nolint:gosec // G204: test-only native Git oracle selected by LookPath; fixed commands and owned fixtures.
				cmd.Dir = root
				cmd.Env = env
				return cmd
			}
			if out, err := run("init", "-q").CombinedOutput(); err != nil {
				t.Fatalf("init: %v %s", err, out)
			}
			if tc.fold {
				if out, err := run("config", "core.ignoreCase", "true").CombinedOutput(); err != nil {
					t.Fatalf("config: %v %s", err, out)
				}
			} else {
				if err := run("config", "core.ignoreCase", "false").Run(); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(tc.pattern), 0600); err != nil {
				t.Fatal(err)
			}
			b := testBudget(t, 0)
			source, err := Parse(context.Background(), []byte(tc.pattern), "", tc.fold, b)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range tc.names {
				want := true
				err := run("check-ignore", "--no-index", "-q", "--", name).Run()
				if err != nil {
					var exit *exec.ExitError
					if !errors.As(err, &exit) || exit.ExitCode() != 1 {
						t.Fatalf("oracle %q: %v", name, err)
					}
					want = false
				}
				got, err := source.Match(context.Background(), b, name, false)
				if err != nil {
					t.Fatal(err)
				}
				if got.Ignored != want {
					t.Errorf("%q: ignored=%v want native Git %v (%+v)", name, got.Ignored, want, got)
				}
			}
		})
	}
}

func TestTrackedNegationOracle(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("native Git oracle unavailable")
	}
	root := t.TempDir()
	home := t.TempDir()
	run := func(args ...string) *exec.Cmd {
		c := exec.Command(git, args...) //nolint:gosec // G204: test-only native Git oracle selected by LookPath; fixed commands and owned fixtures.
		c.Dir = root
		c.Env = []string{"HOME=" + home, "XDG_CONFIG_HOME=" + home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + filepath.Join(home, "none"), "PATH=" + os.Getenv("PATH")}
		return c
	}
	if out, err := run("init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(root, ".mdchk.yaml"), []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run("add", "--force", ".mdchk.yaml").Run(); err != nil {
		t.Fatal(err)
	}
	data := []byte("*.yaml\n!keep.yaml\n")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run("check-ignore", "--no-index", "-q", ".mdchk.yaml").Run(); err != nil {
		t.Fatal(err)
	}
	if err := run("check-ignore", "-q", ".mdchk.yaml").Run(); err == nil {
		t.Fatal("ordinary oracle unexpectedly includes tracked files")
	}
	if err := run("check-ignore", "--no-index", "-v", "keep.yaml").Run(); err != nil {
		t.Fatal("verbose negative match must succeed")
	}
	if err := run("check-ignore", "--no-index", "-q", "keep.yaml").Run(); err == nil {
		t.Fatal("negative match must not mean ignored")
	}
	b := testBudget(t, 0)
	s, err := Parse(context.Background(), data, "", false, b)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		ignored bool
	}{{".mdchk.yaml", true}, {"keep.yaml", false}} {
		d, err := s.Match(context.Background(), b, tc.name, false)
		if err != nil || d.Ignored != tc.ignored || !d.Matched {
			t.Fatalf("%s: %+v %v", tc.name, d, err)
		}
	}
}

func TestAnchorsAndBudgets(t *testing.T) {
	ctx := context.Background()
	b := testBudget(t, 0)
	s, err := Parse(ctx, []byte("/file\n"), "sub", false, b)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		want bool
	}{{"sub/file", true}, {"file", false}, {"other/sub/file", false}, {"sub/nested/file", false}} {
		d, err := s.Match(ctx, b, tc.name, false)
		if err != nil || d.Ignored != tc.want {
			t.Fatalf("%s: %+v %v", tc.name, d, err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Parse(canceled, []byte("*"), "", false, b); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.Match(canceled, b, "sub/file", false); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := Parse(ctx, []byte("abc"), "", false, testBudget(t, 3)); err == nil {
		t.Fatal("expected parsing work bound")
	}
	storm := strings.Repeat("*a", 100) + "b"
	sb := testBudget(t, 5000)
	ss, err := Parse(ctx, []byte(storm), "", false, sb)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ss.Match(ctx, sb, strings.Repeat("a", 1000), false)
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) {
		t.Fatalf("expected bounded wildcard work: %v", err)
	}
}

func TestExactWorkAllowance(t *testing.T) {
	ctx := context.Background()
	if _, err := Parse(ctx, []byte("abc"), "", false, testBudget(t, 5)); err != nil {
		t.Fatalf("exact parse allowance: %v", err)
	}
	if _, err := Parse(ctx, []byte("abc"), "", false, testBudget(t, 4)); err == nil {
		t.Fatal("one below parse allowance succeeded")
	}
	for _, tc := range []struct {
		work int64
		ok   bool
	}{{12, true}, {11, false}} {
		b := testBudget(t, tc.work)
		s, err := Parse(ctx, []byte("a"), "", false, b)
		if err != nil {
			t.Fatal(err)
		}
		d, err := s.Match(ctx, b, "a", false)
		if (err == nil) != tc.ok {
			t.Fatalf("work %d: %+v %v", tc.work, d, err)
		}
	}
}

func TestSourceRecordBound(t *testing.T) {
	limits := parser.DefaultLimits()
	limits.DocumentNodes = 2
	newBudget := func() *parser.OperationBudget {
		b, err := parser.NewOperationBudget(limits)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	s, err := Parse(context.Background(), []byte("a\nb\n# comment\n\n"), "", false, newBudget())
	if err != nil || s.Records() != 2 {
		t.Fatalf("exact records: %+v %v", s, err)
	}
	_, err = Parse(context.Background(), []byte("a\nb\nc\n"), "", false, newBudget())
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || limit.Name != "document-nodes" || limit.Observed != 3 || limit.Allowed != 2 {
		t.Fatalf("one over record bound: %v", err)
	}
}

func TestRemainingRecordAllowance(t *testing.T) {
	ctx := context.Background()
	s, err := ParseBounded(ctx, []byte("a\nb\n"), "", false, testBudget(t, 0), 2)
	if err != nil || s.Records() != 2 {
		t.Fatalf("exact aggregate allowance: %+v %v", s, err)
	}
	_, err = ParseBounded(ctx, []byte("a\nb\nc\n"), "", false, testBudget(t, 0), 2)
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || limit.Name != "total-nodes" || limit.Observed != 3 || limit.Allowed != 2 {
		t.Fatalf("aggregate overflow: %v", err)
	}
	s, err = ParseBounded(ctx, []byte("# comment\n\n"), "", false, testBudget(t, 0), 0)
	if err != nil || s.Records() != 0 {
		t.Fatalf("zero allowance comments: %+v %v", s, err)
	}
	_, err = ParseBounded(ctx, []byte("a\n"), "", false, testBudget(t, 0), 0)
	if !errors.As(err, &limit) || limit.Name != "total-nodes" || limit.Observed != 1 || limit.Allowed != 0 {
		t.Fatalf("zero allowance record: %v", err)
	}
}

func FuzzBoundedMatching(f *testing.F) {
	for _, seed := range []string{"a/**/b\n!b\n", "[[:space:]]\n", "\\#x\nfoo\\ \n", "***[!z-a]\\"} {
		f.Add(seed, "a/b")
	}
	f.Fuzz(func(t *testing.T, patterns, name string) {
		if len(patterns) > 2048 || len(name) > 256 {
			return
		}
		l := parser.DefaultLimits()
		l.WorkUnits = 1000000
		l.DocumentNodes = 2048
		l.TotalNodes = 2048
		b, e := parser.NewOperationBudget(l)
		if e != nil {
			t.Fatal(e)
		}
		data := []byte(patterns)
		source, e := Parse(context.Background(), data, "", false, b)
		if string(data) != patterns {
			t.Fatal("parse mutated caller bytes")
		}
		if e != nil {
			return
		}
		first, e := source.Match(context.Background(), b, name, false)
		if e != nil {
			return
		}
		// Returned decisions cannot mutate the retained matcher or its source text.
		first.Pattern = "caller mutation"
		b, e = parser.NewOperationBudget(l)
		if e != nil {
			t.Fatal(e)
		}
		second, e := source.Match(context.Background(), b, name, false)
		if e != nil {
			t.Fatal(e)
		}
		if second.Matched && second.Pattern == first.Pattern && !strings.Contains(patterns, "caller mutation") {
			t.Fatal("decision aliased matcher")
		}
	})
}
