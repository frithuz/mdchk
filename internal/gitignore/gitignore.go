// Package gitignore implements bounded, byte-oriented Git ignore patterns.
// Source precedence and directory pruning belong to the caller.
package gitignore

import (
	"context"
	"errors"
	"strings"

	"github.com/frithuz/mdchk/internal/parser"
)

func take(ctx context.Context, b *parser.OperationBudget, n int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return b.ChargeWork(n)
}

// Decision identifies the last matching line of one source.
type Decision struct {
	Matched bool
	Ignored bool
	Line    int
	Pattern string
}

type rule struct {
	pattern   string
	original  string
	line      int
	negative  bool
	directory bool
	basename  bool
}

// Source is an immutable parsed ignore file, anchored at its repository-relative directory.
type Source struct {
	base  string
	fold  bool
	rules []rule
}

// Records reports the number of executable pattern records in this source.
func (s *Source) Records() int64 { return int64(len(s.rules)) }

// Parse parses an ignore source whose base is a repository-relative directory.
// Names and patterns use slash separators and Git's byte matching semantics.
func Parse(ctx context.Context, data []byte, base string, ignoreCase bool, budget *parser.OperationBudget) (*Source, error) {
	if budget == nil {
		return nil, errors.New("nil parser operation budget")
	}
	return ParseBounded(ctx, data, base, ignoreCase, budget, budget.Limits().DocumentNodes)
}

// ParseBounded also limits records to the caller's remaining aggregate allowance.
// A zero allowance permits only comments and blank lines.
func ParseBounded(ctx context.Context, data []byte, base string, ignoreCase bool, budget *parser.OperationBudget, maxRecords int64) (*Source, error) {
	if maxRecords < 0 {
		return nil, errors.New("negative Git ignore record allowance")
	}
	if err := take(ctx, budget, int64(len(data))+1); err != nil {
		return nil, err
	}
	if base != "" && (strings.HasPrefix(base, "/") || strings.Contains("/"+base+"/", "/../") || strings.Contains("/"+base+"/", "/./")) {
		return nil, errors.New("invalid Git ignore source base")
	}
	s := &Source{base: strings.TrimSuffix(base, "/"), fold: ignoreCase}
	line := 0
	for text := range strings.SplitSeq(string(data), "\n") {
		line++
		if err := take(ctx, budget, 1); err != nil {
			return nil, err
		}
		text = strings.TrimSuffix(text, "\r")
		if line == 1 {
			text = strings.TrimPrefix(text, "\xef\xbb\xbf")
		}
		original := text
		// A trailing space is significant only when escaped by an odd number
		// of immediately preceding backslashes.
		for len(text) > 0 && text[len(text)-1] == ' ' {
			n := 0
			for i := len(text) - 2; i >= 0 && text[i] == '\\'; i-- {
				n++
			}
			if n%2 == 1 {
				break
			}
			text = text[:len(text)-1]
		}
		if text == "" || text[0] == '#' {
			continue
		}
		r := rule{original: original, line: line}
		if text[0] == '!' {
			r.negative = true
			text = text[1:]
		}
		if text == "" {
			continue
		}
		if text[len(text)-1] == '/' {
			r.directory = true
			text = text[:len(text)-1]
		}
		r.basename = !strings.Contains(text, "/")
		if strings.HasPrefix(text, "/") {
			r.basename = false
			text = text[1:]
		}
		r.pattern = text
		cap := budget.Limits().DocumentNodes
		limitName := "document-nodes"
		if maxRecords < cap {
			cap = maxRecords
			limitName = "total-nodes"
		}
		if int64(len(s.rules)) >= cap {
			return nil, &parser.LimitExceeded{Name: limitName, Observed: int64(len(s.rules)) + 1, Allowed: cap}
		}
		if err := budget.ChargeNodes(1); err != nil {
			return nil, err
		}
		s.rules = append(s.rules, r)
	}
	return s, nil
}

// Match evaluates a repository-relative name. The caller must not descend into
// ignored directories: a child negation cannot reopen a pruned parent.
func (s *Source) Match(ctx context.Context, budget *parser.OperationBudget, name string, isDir bool) (Decision, error) {
	var result Decision
	if err := take(ctx, budget, 1); err != nil {
		return result, err
	}
	if s.base != "" {
		prefix := s.base + "/"
		if !strings.HasPrefix(name, prefix) {
			return result, nil
		}
		name = name[len(prefix):]
	}
	for _, r := range s.rules {
		if err := take(ctx, budget, 1); err != nil {
			return Decision{}, err
		}
		// Try the path and its directory ancestors. Directory patterns also
		// apply to every descendant of the matched directory.
		for end := len(name); end > 0; {
			dir := end < len(name) || isDir
			if err := take(ctx, budget, int64(end)+1); err != nil {
				return Decision{}, err
			}
			candidate := name[:end]
			if r.basename {
				if slash := strings.LastIndexByte(candidate, '/'); slash >= 0 {
					candidate = candidate[slash+1:]
				}
			}
			if !r.directory || dir {
				ok, err := wildmatch(ctx, budget, r.pattern, candidate, s.fold)
				if err != nil {
					return Decision{}, err
				}
				if ok {
					result = Decision{Matched: true, Ignored: !r.negative, Line: r.line, Pattern: r.original}
					break
				}
			}
			end = strings.LastIndexByte(name[:end], '/')
		}
	}
	return result, nil
}

func folded(c byte, fold bool) byte {
	if fold && c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// wildmatch uses two dynamic-programming rows. Work is polynomial and stack
// depth is independent of source or path length.
func wildmatch(ctx context.Context, budget *parser.OperationBudget, pattern, name string, fold bool) (bool, error) {
	type token struct {
		kind  byte
		value byte
		chars *[256]bool
	}
	if err := take(ctx, budget, int64(len(pattern)+len(name))+1); err != nil {
		return false, err
	}
	tokens := make([]token, 0, len(pattern))
	for p := 0; p < len(pattern); {
		t := token{kind: pattern[p]}
		next := p + 1
		switch t.kind {
		case '*':
			for next < len(pattern) && pattern[next] == '*' {
				next++
			}
			double := next-p >= 2 && (p == 0 || pattern[p-1] == '/') && (next == len(pattern) || pattern[next] == '/')
			if double {
				t.kind = 'D'
				if next < len(pattern) {
					t.kind = 'S'
					next++
				}
			}
		case '[':
			t.chars = new([256]bool)
			if err := take(ctx, budget, int64(256*(len(pattern)-p))); err != nil {
				return false, err
			}
			valid := false
			for c := 0; c < 256; c++ {
				var end int
				var ok bool
				t.chars[c], end, ok = bracket(pattern, p, byte(c), fold)
				if ok {
					next = end
					valid = true
				}
			}
			if !valid {
				return false, nil
			}
		case '?':
		default:
			t.kind = 'L'
			t.value = pattern[p]
			if t.value == '\\' {
				if next == len(pattern) {
					return false, nil
				}
				t.value = pattern[next]
				next++
			}
		}
		tokens = append(tokens, t)
		p = next
	}
	next := make([]bool, len(name)+1)
	current := make([]bool, len(name)+1)
	next[len(name)] = true
	for i := len(tokens) - 1; i >= 0; i-- {
		t := tokens[i]
		slashTail := false
		for n := len(name); n >= 0; n-- {
			if err := take(ctx, budget, 1); err != nil {
				return false, err
			}
			current[n] = false
			switch t.kind {
			case '*', 'D', 'S':
				current[n] = next[n]
				if n < len(name) {
					if t.kind == 'S' {
						slashTail = slashTail || (name[n] == '/' && next[n+1])
						current[n] = current[n] || slashTail
					} else if t.kind == 'D' || name[n] != '/' {
						current[n] = current[n] || current[n+1]
					}
				}
			case '?':
				current[n] = n < len(name) && name[n] != '/' && next[n+1]
			case '[':
				current[n] = n < len(name) && name[n] != '/' && t.chars[name[n]] && next[n+1]
			case 'L':
				current[n] = n < len(name) && folded(t.value, fold) == folded(name[n], fold) && next[n+1]
			}
		}
		next, current = current, next
	}
	return next[0], nil
}

func bracket(pattern string, start int, c byte, fold bool) (bool, int, bool) {
	i := start + 1
	neg := false
	if i < len(pattern) && (pattern[i] == '!' || pattern[i] == '^') {
		neg = true
		i++
	}
	hit := false
	first := true
	for i < len(pattern) {
		if pattern[i] == ']' && !first {
			return hit != neg, i + 1, true
		}
		first = false
		if strings.HasPrefix(pattern[i:], "[:") {
			if end := strings.Index(pattern[i+2:], ":]"); end >= 0 {
				name := pattern[i+2 : i+2+end]
				yes, valid := class(name, c)
				if fold && (name == "lower" || name == "upper") {
					yes = c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
				}
				if !valid {
					return false, 0, false
				}
				hit = hit || yes
				i += end + 4
				continue
			}
		}
		lo := pattern[i]
		i++
		if lo == '\\' && i < len(pattern) {
			lo = pattern[i]
			i++
		}
		if i+1 < len(pattern) && pattern[i] == '-' && pattern[i+1] != ']' {
			i++
			hi := pattern[i]
			i++
			if hi == '\\' && i < len(pattern) {
				hi = pattern[i]
				i++
			}
			v := folded(c, fold)
			hit = hit || (v >= folded(lo, fold) && v <= folded(hi, fold))
		} else {
			hit = hit || folded(lo, fold) == folded(c, fold)
		}
	}
	return false, 0, false
}

func class(name string, c byte) (bool, bool) {
	alpha := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
	digit := c >= '0' && c <= '9'
	switch name {
	case "alnum":
		return alpha || digit, true
	case "alpha":
		return alpha, true
	case "blank":
		return c == ' ' || c == '\t', true
	case "cntrl":
		return c < 32 || c == 127, true
	case "digit":
		return digit, true
	case "graph":
		return c > 32 && c < 127, true
	case "lower":
		return c >= 'a' && c <= 'z', true
	case "print":
		return c >= 32 && c < 127, true
	case "punct":
		return c > 32 && c < 127 && !alpha && !digit, true
	case "space":
		return c == ' ' || c >= '\t' && c <= '\r', true
	case "upper":
		return c >= 'A' && c <= 'Z', true
	case "xdigit":
		return digit || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F', true
	default:
		return false, false
	}
}
