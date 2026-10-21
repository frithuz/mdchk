package config

import (
	"context"
	"errors"
	"fmt"
	"github.com/frithuz/mdchk/internal/parser"
	"path"
	"strings"
	"unicode/utf8"
)

// Pattern limits bound compilation, input storage, and conservative matching work.
// Work counts segment-state visits plus len(pattern)*len(name) for path.Match.
const (
	MaxPatternBytes     = 4096
	MaxPatternSegments  = 256
	MaxPatterns         = 1024
	MaxPatternListBytes = 1 << 20
	MaxMatchNameBytes   = 16384
	MaxMatchSegments    = 1024
	MaxPatternMatchWork = 8 << 20
)

// ErrPatternLimit identifies a deterministic compilation or matching limit.
var ErrPatternLimit = errors.New("pattern limit exceeded")

// PatternError identifies an invalid pattern by its zero-based list index. It
// deliberately has no configuration provenance; validators attach existing Origin.
type PatternError struct {
	Index   int
	Pattern string
	Err     error
}

func (e *PatternError) Error() string {
	return fmt.Sprintf("pattern %d %q: %v", e.Index, e.Pattern, e.Err)
}
func (e *PatternError) Unwrap() error { return e.Err }

type compiledPattern struct {
	segments  []string
	directory bool
}

// Patterns is an immutable, concurrently usable compiled pattern list. Its zero
// value matches nothing. CompilePatterns snapshots its input.
type Patterns struct{ patterns []compiledPattern }

// CompilePatterns validates every pattern before returning a matcher. No partial
// matcher is returned on error. Syntax is Go path.Match per segment plus ** as a
// whole segment; patterns are case-sensitive and anchored to the logical base.
// No filesystem operations, configuration decoding, or policy decisions occur.
func CompilePatterns(patterns []string) (*Patterns, error) {
	if len(patterns) > MaxPatterns {
		return nil, fmt.Errorf("%w: pattern count", ErrPatternLimit)
	}
	result := &Patterns{}
	total := 0
	for i, pattern := range patterns {
		total += len(pattern)
		if total > MaxPatternListBytes {
			return nil, &PatternError{i, pattern, fmt.Errorf("%w: pattern list bytes", ErrPatternLimit)}
		}
		p, err := compilePattern(pattern)
		if err != nil {
			return nil, &PatternError{i, pattern, err}
		}
		result.patterns = append(result.patterns, p)
	}
	return result, nil
}

func driveQualified(s string) bool {
	return len(s) >= 2 && ((s[0] >= 'a' && s[0] <= 'z') || (s[0] >= 'A' && s[0] <= 'Z')) && s[1] == ':'
}
func compilePattern(s string) (compiledPattern, error) {
	var p compiledPattern
	if len(s) > MaxPatternBytes {
		return p, fmt.Errorf("%w: pattern bytes", ErrPatternLimit)
	}
	if s == "" || !utf8.ValidString(s) || strings.ContainsRune(s, 0) || strings.HasPrefix(s, "/") || driveQualified(s) || strings.HasPrefix(s, "!") {
		return p, errors.New("expected a nonempty relative pattern without leading negation")
	}
	// A leading escaped backslash followed by a further backslash separator is
	// UNC-shaped. Other escaped backslashes remain ordinary literal characters.
	if strings.HasPrefix(s, `\\`) && strings.Contains(s[2:], `\`) {
		return p, errors.New("UNC patterns are unsupported")
	}
	p.directory = strings.HasSuffix(s, "/")
	if p.directory {
		s = strings.TrimSuffix(s, "/")
	}
	p.segments = strings.Split(s, "/")
	if len(p.segments) > MaxPatternSegments {
		return p, fmt.Errorf("%w: pattern segments", ErrPatternLimit)
	}
	for _, segment := range p.segments {
		if segment == "" || segment == "." || segment == ".." {
			return p, errors.New("empty, dot, and dot-dot segments are unsupported")
		}
		if segment == "**" {
			continue
		}
		if _, err := path.Match(segment, ""); err != nil {
			return p, err
		}
		inClass := false
		previousStar := false
		for i := 0; i < len(segment); i++ {
			c := segment[i]
			if c == '\\' {
				i++
				previousStar = false
				continue
			}
			if c == '[' {
				inClass = true
				previousStar = false
				continue
			}
			if c == ']' && inClass {
				inClass = false
				continue
			}
			if inClass {
				continue
			}
			if c == '{' || c == '}' {
				return p, errors.New("brace expansion is unsupported; escape literal braces")
			}
			if c == '*' && previousStar {
				return p, errors.New("globstar must be exactly one whole ** segment")
			}
			previousStar = c == '*'
		}
	}
	return p, nil
}

// Match reports whether the complete logical entry matches directly. Excludes
// additionally checks ancestor directories. inBase=false returns false without
// inspecting name: adapters must establish containment before calling. For an
// in-base entry, name is a UTF-8, '/'-separated normalized relative name; "" is
// the base directory (isDir must be true). Dot segments, absolute/drive names,
// repeated/trailing separators and NUL are errors, never cleaned into scope.
// Backslashes in names are literal characters, never native separators. Callers
// supply directory identity; no symlinks, extensions, or filesystem are inspected.
// Terminal /** includes its base only when that entry is a directory.
func (p Patterns) Match(name string, isDir, inBase bool) (bool, error) {
	return p.match(name, isDir, inBase, false, nil)
}

// Excludes reports a direct match or a matching ancestor directory. Thus an
// explicitly submitted file beneath an excluded directory is still excluded.
// Use Match for consumers such as slide annotations that need direct matching.
func (p Patterns) Excludes(name string, isDir, inBase bool) (bool, error) {
	return p.match(name, isDir, inBase, true, nil)
}

// ExcludesBounded uses the same grammar and matcher with shared operation work.
func (p Patterns) ExcludesBounded(ctx context.Context, budget *parser.OperationBudget, name string, isDir, inBase bool) (bool, error) {
	charge := func(n int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return budget.ChargeWork(int64(n))
	}
	if err := charge(len(name) + 1); err != nil {
		return false, err
	}
	return p.match(name, isDir, inBase, true, charge)
}
func (p Patterns) match(name string, isDir, inBase, ancestors bool, charge func(int) error) (bool, error) {
	if !inBase {
		return false, nil
	}
	if len(name) > MaxMatchNameBytes {
		return false, fmt.Errorf("%w: name bytes", ErrPatternLimit)
	}
	if !utf8.ValidString(name) || strings.ContainsRune(name, 0) || strings.HasPrefix(name, "/") || driveQualified(name) || (name == "" && !isDir) {
		return false, errors.New("expected a normalized relative logical entry")
	}
	var names []string
	if name != "" {
		names = strings.Split(name, "/")
	}
	if len(names) > MaxMatchSegments {
		return false, fmt.Errorf("%w: name segments", ErrPatternLimit)
	}
	for _, s := range names {
		if s == "" || s == "." || s == ".." {
			return false, errors.New("expected a normalized relative logical entry")
		}
	}
	work := 0
	for _, pattern := range p.patterns {
		matched, err := pattern.match(names, isDir, ancestors, &work, charge)
		if err != nil || matched {
			return matched, err
		}
	}
	return false, nil
}

// Each row memoizes reachable (pattern index, name index) states. Ancestors
// are tested during this single scan instead of restarting at every prefix.
func (p compiledPattern) match(names []string, isDir, ancestors bool, work *int, charge func(int) error) (bool, error) {
	states := make([]bool, len(p.segments)+1)
	next := make([]bool, len(states))
	states[0] = true
	for n := 0; n <= len(names); n++ {
		for i, s := range p.segments {
			if charge != nil {
				if err := charge(1); err != nil {
					return false, err
				}
			}
			*work++
			if *work > MaxPatternMatchWork {
				return false, fmt.Errorf("%w: matching work", ErrPatternLimit)
			}
			// Leave terminal **'s zero-length acceptance to the directory test below.
			if states[i] && s == "**" && i+1 < len(p.segments) {
				states[i+1] = true
			}
		}
		directory := n < len(names) || isDir
		accepted := states[len(p.segments)]
		if p.segments[len(p.segments)-1] == "**" && directory && states[len(p.segments)-1] {
			accepted = true
		}
		if accepted && (!p.directory || directory) && (n == len(names) || ancestors) {
			return true, nil
		}
		if n == len(names) {
			break
		}
		clear(next)
		for i, s := range p.segments {
			if !states[i] {
				continue
			}
			if s == "**" {
				next[i] = true
				if i+1 == len(p.segments) {
					next[i+1] = true
				}
				continue
			}
			// Charge a conservative bound before entering the standard-library matcher.
			units := (len(s) + 1) * (len(names[n]) + 1)
			if charge != nil {
				if err := charge(units); err != nil {
					return false, err
				}
			}
			*work += units
			if *work > MaxPatternMatchWork {
				return false, fmt.Errorf("%w: matching work", ErrPatternLimit)
			}
			matched, _ := path.Match(s, names[n]) // All segments were fully validated.
			if matched {
				next[i+1] = true
			}
		}
		states, next = next, states
	}
	return false, nil
}
