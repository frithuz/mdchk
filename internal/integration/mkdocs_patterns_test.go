package integration

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

// Fixed independent oracle: CPython 3.15, pathspec==0.11.1 installed in a
// disposable venv, GitIgnoreSpec.from_lines(...).match_file(name). Primary inputs:
// https://github.com/cpburnz/python-pathspec/blob/v0.11.1/pathspec/gitignore.py
// https://github.com/cpburnz/python-pathspec/blob/v0.11.1/pathspec/patterns/gitwildmatch.py
// MkDocs 1.6.1 accepts pathspec>=0.11.1; this reproducible minimum-version
// baseline is a verification input, not an invented exact product dependency.
func TestMkDocsPatternsPinnedUnicodeAndPriorityOracle(t *testing.T) {
	for _, tc := range []struct {
		patterns []string
		names    []string
		want     []bool
	}{
		{[]string{".*", "/templates/", "!.visible.md"}, []string{".hidden.md", "a/.hidden.md", ".hidden/a.md", ".visible.md", "a/.visible.md", "templates/a.md", "a/templates/a.md"}, []bool{true, true, true, false, false, true, false}},
		{[]string{"é?.md"}, []string{"é図.md", "éaa.md", "a/é図.md", "e図.md"}, []bool{true, false, true, false}},
		{[]string{"[é-ê]?.md"}, []string{"é図.md", "êx.md", "ëx.md", "éxx.md"}, []bool{true, true, false, false}},
		{[]string{"*.md", "!docs/"}, []string{"docs/a.md", "docs/b.txt", "a.md"}, []bool{true, false, true}},
		{[]string{"*.md", "!docs/", "docs/a.md"}, []string{"docs/a.md", "docs/b.md"}, []bool{true, true}},
		{[]string{"!docs/a.md", "docs/"}, []string{"docs/a.md", "docs/b.md"}, []bool{true, true}},
		{[]string{"docs/", "!docs/a.md"}, []string{"docs/a.md", "docs/b.md"}, []bool{false, true}},
		{[]string{"a/**/b.md"}, []string{"a/b.md", "a/x/b.md", "a/x/y/b.md", "x/a/b.md"}, []bool{true, true, true, false}},
		{[]string{"**/"}, []string{"a.md", "a/b.md"}, []bool{false, true}},
		{[]string{"**"}, []string{"a.md", "a/b.md"}, []bool{true, true}},
		{[]string{"foo", "!foo/"}, []string{"foo", "foo/a.md", "a/foo", "a/foo/a.md"}, []bool{true, false, true, false}},
		{[]string{"[[]?.md"}, []string{"[é.md", "[ab.md"}, []bool{true, false}},
		{[]string{"[]-].md"}, []string{"].md", "-.md", "x.md"}, []bool{true, true, false}},
		{[]string{"[^a].md"}, []string{"^.md", "a.md", "x.md"}, []bool{true, true, false}},
		{[]string{`\#?.md`, `\!?.md`, `a\*.md`}, []string{"#é.md", "!図.md", "a*.md", "ab.md"}, []bool{true, true, true, false}},
		{[]string{"/a.md", "dir/a.md"}, []string{"a.md", "x/a.md", "dir/a.md", "x/dir/a.md"}, []bool{true, false, true, false}},
		{[]string{"*.MD"}, []string{"page.MD", "page.md"}, []bool{true, false}},
		{[]string{"a/**/**/b?.md"}, []string{"a/bé.md", "a/x/b図.md", "a/x/y/bab.md"}, []bool{true, true, false}},
		{[]string{"dir/", "!dir/deep/a.md"}, []string{"dir/a.md", "dir/deep/a.md", "dir/deep/b.md"}, []bool{true, false, true}},
		{[]string{"*.md", "!foo.md", "foo/"}, []string{"foo.md", "foo/page.md", "nested/foo.md"}, []bool{false, true, false}},
		{[]string{" # ignored ", "/", "", "*.md"}, []string{"a.md", "a.txt"}, []bool{true, false}},
		{[]string{"[abc.md"}, []string{"[abc.md", "a.md"}, []bool{true, false}},
	} {
		t.Run(strings.Join(tc.patterns, "|"), func(t *testing.T) {
			s, _, _ := contractScope(t, "", Settings{})
			matcher, err := newMkDocsPatterns(context.Background(), s, tc.patterns)
			if err != nil {
				t.Fatal(err)
			}
			var got []bool
			for _, name := range tc.names {
				match, err := matcher.Match(context.Background(), s, name)
				if err != nil {
					t.Fatal(name, err)
				}
				got = append(got, match)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("names %q: got %v, want %v", tc.names, got, tc.want)
			}
		})
	}
}

func TestMkDocsPatternsMultilineOwnershipAndForeignScope(t *testing.T) {
	s, _, _ := contractScope(t, "", Settings{})
	lines := []string{"\u2003*.md\u2003\r\n!é.md\u2028/private/\u0085#comment\n\\#?.txt"}
	matcher, err := newMkDocsPatterns(context.Background(), s, lines)
	if err != nil {
		t.Fatal(err)
	}
	lines[0] = "*"
	for name, want := range map[string]bool{"page.md": true, "é.md": false, "private/a.txt": true, "nested/private/a.txt": false, "#図.txt": true, "image.png": false} {
		got, err := matcher.Match(context.Background(), s, name)
		if err != nil || got != want {
			t.Fatalf("%q got %v %v, want %v", name, got, err, want)
		}
	}
	foreign, _, _ := contractScope(t, "", Settings{})
	if _, err := matcher.Match(context.Background(), foreign, "page.md"); !errors.Is(err, ErrContract) {
		t.Fatal("foreign scope admitted", err)
	}
	for _, pattern := range []string{"trailing\\", "!", "[z-a].md", string([]byte{0xff})} {
		_, err := newMkDocsPatterns(context.Background(), s, []string{pattern})
		var invalid *mkDocsPatternError
		if !errors.As(err, &invalid) {
			t.Fatalf("%q: expected pattern uncertainty, got %v", pattern, err)
		}
	}
}

func mkDocsPatternScopeWithLimits(t *testing.T, limits parser.Limits) *Scope {
	t.Helper()
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := resource.NewRoot(resource.SuppliedRoot)
	policy, _ := resource.NewSettings(parser.ParseOptions{}, "index.md", "github", "pattern-test")
	op, err := resource.NewOperation(context.Background(), budget, root, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err := op.NewView(resource.Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &Scope{operation: op, view: view}
}

func TestMkDocsPatternsSharedBoundsAndFirstStop(t *testing.T) {
	ctx := context.Background()
	limits := parser.DefaultLimits()
	limits.DocumentBytes = 8
	s := mkDocsPatternScopeWithLimits(t, limits)
	if _, err := newMkDocsPatterns(ctx, s, []string{"12345678"}); err != nil {
		t.Fatal("exact pattern byte boundary", err)
	}
	_, err := newMkDocsPatterns(ctx, s, []string{"123456789"})
	var bound *parser.LimitExceeded
	if !errors.As(err, &bound) || bound.Name != "pattern-bytes" || bound.Observed != 9 || bound.Allowed != 8 {
		t.Fatal("pattern boundary+1", err)
	}
	limits = parser.DefaultLimits()
	limits.TotalNodes = 64
	limits.DocumentNodes = 64
	s = mkDocsPatternScopeWithLimits(t, limits)
	_, err = newMkDocsPatterns(ctx, s, []string{"*.md"})
	if !errors.As(err, &bound) || bound.Name != "total-nodes" {
		t.Fatal("no preallocation node admission", err)
	}
	limits = parser.DefaultLimits()
	s = mkDocsPatternScopeWithLimits(t, limits)
	matcher, err := newMkDocsPatterns(ctx, s, []string{"é?.md"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = matcher.Match(ctx, s, "é図.md")
	if err != nil {
		t.Fatal(err)
	}
	first := s.operation.Budget().ChargeWork(limits.WorkUnits + 1)
	if !errors.As(first, &bound) {
		t.Fatal(first)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = matcher.Match(canceled, s, "é図.md")
	var retained *parser.LimitExceeded
	if !errors.Is(err, context.Canceled) || !errors.As(err, &retained) || *retained != *bound {
		t.Fatal("caller stop hid exact first quota", err, first)
	}
}
