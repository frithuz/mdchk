package config

import (
	"errors"
	"path"
	"strings"
	"sync"
	"testing"
)

func TestPatternExclusionContract(t *testing.T) {
	tests := []struct {
		pattern, name         string
		dir, direct, excluded bool
	}{
		{"vendor", "vendor", false, true, true}, {"vendor", "vendor", true, true, true},
		{"vendor", "vendor/readme.txt", false, false, true}, {"vendor", "packages/vendor/a.md", false, false, false},
		{"**/vendor/", "vendor", true, true, true}, {"**/vendor/", "vendor", false, false, false},
		{"**/vendor/", "packages/vendor", true, true, true}, {"**/vendor/", "packages/vendor", false, false, false},
		{"**/vendor/", "packages/vendor/a.md", false, false, true},
		{"docs/generated/**", "docs/generated", true, true, true}, {"docs/generated/**", "docs/generated", false, false, false},
		{"docs/generated/**", "docs/generated/a.txt", false, true, true}, {"docs/generated/**", "other/docs/generated", true, false, false},
		{"*.md", "a.md", false, true, true}, {"*.md", ".hidden.md", false, true, true}, {"*.md", "a.MD", false, false, false},
		{"*.md", "docs/a.md", false, false, false}, {"*.md", "archive.md/plain.txt", false, false, true},
		{"**/*.md", "a.md", false, true, true}, {"**/*.md", "docs/.a.md", false, true, true}, {"**/*.md", "docs/README.MD", false, false, false},
		{"a/**/b", "a/b", false, true, true}, {"a/**/b", "a/x/y/b", false, true, true},
		{"a/*/b", "a/b", false, false, false}, {"a/*/b", "a/x/b", false, true, true}, {"a/*/b", "a/x/y/b", false, false, false},
		{"**", "", true, true, true}, {"a/**/**", "a", true, true, true}, {"a/**/**", "a", false, false, false},
		{"a/**/b/**", "a/b", false, false, false}, {"a/**/b/**", "a/b", true, true, true},
		{"a/**/b/**", "a/b/c", false, true, true}, {"docs/", "docs/a.txt", false, false, true},
		{`literal/\*\?\[\]\!\{\}\\`, "literal/*?[]!{}\\", false, true, true},
		{`\!name`, "!name", false, true, true}, {`\\name`, "\\name", false, true, true},
		{"?", "é", false, true, true}, {"?", "é", false, false, false},
		{"[^a-z]", "É", false, true, true}, {"[!a]", "!", false, true, true}, {"[!a]", "b", false, false, false},
		{"[z-a]", "z", false, false, false}, {"[**]", "*", false, true, true},
		{`\**`, "*tail", false, true, true}, {`*\*`, "tail*", false, true, true},
		{`\*\*`, "**", false, true, true}, {"[{]", "{", false, true, true}, {"[}]", "}", false, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+":"+tt.name, func(t *testing.T) {
			p, err := CompilePatterns([]string{tt.pattern})
			if err != nil {
				t.Fatal(err)
			}
			direct, err := p.Match(tt.name, tt.dir, true)
			if err != nil || direct != tt.direct {
				t.Fatalf("Match=%v,%v want %v", direct, err, tt.direct)
			}
			excluded, err := p.Excludes(tt.name, tt.dir, true)
			if err != nil || excluded != tt.excluded {
				t.Fatalf("Excludes=%v,%v want %v", excluded, err, tt.excluded)
			}
		})
	}
}

func TestPatternInvalidGrammar(t *testing.T) {
	invalid := []string{"", "/", "/a", "C:a", "z:/a", `\\server\share`, "!a", "a//b", "a//", "a///", ".", "..", "a/./b", "a/../b", "[", "[]", "[^]", "[a-]", `a\`, `never/[`, "ab**cd", "***", "a/***/b", "**a", "a**", "a{b,c}", "a}", "a/{b,c}", "a\x00b", string([]byte{0xff})}
	for _, pattern := range invalid {
		t.Run(pattern, func(t *testing.T) {
			p, err := CompilePatterns([]string{"valid", pattern})
			if err == nil || p != nil {
				t.Fatalf("CompilePatterns=%v,%v", p, err)
			}
			var detail *PatternError
			if !errors.As(err, &detail) || detail.Index != 1 || detail.Pattern != pattern {
				t.Fatalf("missing pattern index: %v", err)
			}
		})
	}
	// Full segment validation must not stop after an earlier literal mismatch.
	for _, s := range []string{"does-not-match[", `does-not-match\`, "valid/does-not-match["} {
		if _, err := CompilePatterns([]string{s}); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
}

func TestPatternLogicalInputBoundary(t *testing.T) {
	p, err := CompilePatterns([]string{"**"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../a", "a/../b", "./a", "a/./b", "/a", "C:a", "a//b", "a/", "a\x00b", string([]byte{0xff})} {
		if _, err := p.Excludes(name, false, true); err == nil {
			t.Errorf("accepted input %q", name)
		}
		if got, err := p.Excludes(name, false, false); err != nil || got {
			t.Errorf("outside input %q=%v,%v", name, got, err)
		}
	}
	if _, err := p.Match("", false, true); err == nil {
		t.Fatal("accepted regular-file root")
	}
	if got, err := p.Match(`a\b`, false, true); err != nil || !got {
		t.Fatalf("literal backslash name=%v,%v", got, err)
	}
	var empty Patterns
	if got, err := empty.Match("a.md", false, true); err != nil || got {
		t.Fatalf("zero value=%v,%v", got, err)
	}
}

func TestPatternSingleSegmentDifferential(t *testing.T) {
	patterns := []string{"*", "?", "a?*", "[abc]", "[^a-z]", "[!a]", "[z-a]", "[**]", `\*\*`, `\**`, `*\*`, `[\]]`, `[[]`, `\a`, `\!`, `[{}]`, `\\`, "[é-界]"}
	names := []string{"a", "b", "!", "*", "**", "a*", "*a", "aé", "é", "界", "é", "[", "]", "{", "}", "\\", ".hidden"}
	for _, s := range patterns {
		p, err := CompilePatterns([]string{s})
		if err != nil {
			t.Fatalf("compile %q: %v", s, err)
		}
		for _, name := range names {
			want, err := path.Match(s, name)
			if err != nil {
				t.Fatal(err)
			}
			got, err := p.Match(name, false, true)
			if err != nil || got != want {
				t.Errorf("%q/%q=%v,%v want %v", s, name, got, err, want)
			}
		}
	}
}

func TestPatternSnapshotConcurrentReuse(t *testing.T) {
	input := []string{"**/vendor/", "docs/generated/**"}
	p, err := CompilePatterns(input)
	if err != nil {
		t.Fatal(err)
	}
	input[0] = "unrelated"
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 100 {
				if got, err := p.Excludes("a/vendor/readme.md", false, true); err != nil || !got {
					t.Errorf("match=%v,%v", got, err)
				}
				if got, err := p.Excludes("a/readme.md", false, true); err != nil || got {
					t.Errorf("nonmatch=%v,%v", got, err)
				}
			}
		})
	}
	wg.Wait()
}

func TestPatternDeterministicLimits(t *testing.T) {
	compileLimits := [][]string{make([]string, MaxPatterns+1), {strings.Repeat("x", MaxPatternBytes+1)}, {strings.Repeat("x/", MaxPatternSegments) + "x"}}
	list := make([]string, MaxPatterns)
	for i := range list {
		list[i] = strings.Repeat("x", MaxPatternBytes)
	}
	compileLimits = append(compileLimits, list)
	for _, patterns := range compileLimits {
		p, err := CompilePatterns(patterns)
		if p != nil || !errors.Is(err, ErrPatternLimit) {
			t.Fatalf("compile limit=%v,%v", p, err)
		}
	}
	p, err := CompilePatterns([]string{"**"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{strings.Repeat("x", MaxMatchNameBytes+1), strings.Repeat("x/", MaxMatchSegments) + "x"} {
		if _, err := p.Match(name, false, true); !errors.Is(err, ErrPatternLimit) {
			t.Fatalf("input limit=%v", err)
		}
	}
	// Many ambiguous globstar placements finish with a deterministic no-match.
	adversarial := strings.Repeat("**/a/", 100) + "z"
	p, err = CompilePatterns([]string{adversarial})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := p.Excludes(strings.Repeat("a/", 300)+"b", false, true); err != nil || got {
		t.Fatalf("adversarial=%v,%v", got, err)
	}
	// Large segment products are rejected before calling path.Match.
	p, err = CompilePatterns([]string{strings.Repeat("?", MaxPatternBytes)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Match(strings.Repeat("a", MaxMatchNameBytes), false, true); !errors.Is(err, ErrPatternLimit) {
		t.Fatalf("work limit=%v", err)
	}
}

func FuzzPatternMatching(f *testing.F) {
	for _, s := range []string{"**/vendor/", "a/**/b", "[**]", `\**`, "[bad", "docs/generated/**"} {
		f.Add(s, "a/vendor/readme.md", false)
	}
	f.Fuzz(func(t *testing.T, s, name string, dir bool) {
		p, err := CompilePatterns([]string{s})
		if err != nil {
			return
		}
		first, e1 := p.Excludes(name, dir, true)
		second, e2 := p.Excludes(name, dir, true)
		if first != second || (e1 == nil) != (e2 == nil) {
			t.Fatal("nondeterministic matching")
		}
	})
}
