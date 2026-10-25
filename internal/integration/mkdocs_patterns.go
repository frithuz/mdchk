package integration

import (
	"context"
	"regexp"
	"regexp/syntax"
	"strings"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
)

// mkDocsPatterns implements MkDocs 1.6.1's GitIgnoreSpec inputs. The reference
// baseline is python-pathspec v0.11.1 (MkDocs permits that version or newer),
// patterns/gitwildmatch.py and gitignore.py. This adapter uses Unicode RE2, never
// Git's byte matcher or parent-directory traversal pruning. Patterns are compiled
// once; each name is an already confined docs-root-relative FILE name.
type mkDocsPatterns struct {
	scope *Scope
	rules []mkDocsPattern
}
type mkDocsPattern struct {
	include bool
	regex   *regexp.Regexp
	states  int64
}
type mkDocsPatternError struct{ reason string }

func (e *mkDocsPatternError) Error() string { return "Invalid MkDocs eligibility pattern: " + e.reason }

func newMkDocsPatterns(ctx context.Context, s *Scope, inputs []string) (*mkDocsPatterns, error) {
	if s == nil || ctx == nil {
		return nil, ErrContract
	}
	if err := s.checkpoint(ctx, int64(len(inputs))+1, 1); err != nil {
		return nil, err
	}
	total := int64(0)
	for _, input := range inputs {
		total += int64(len(input))
		if total > s.operation.Budget().Limits().DocumentBytes {
			return nil, &parser.LimitExceeded{Name: "pattern-bytes", Observed: total, Allowed: s.operation.Budget().Limits().DocumentBytes}
		}
	}
	// Reserve raw Unicode decoding, all split-line views and worst-case retained
	// translated/compiled instructions before entering either decoder or RE2.
	if err := s.checkpoint(ctx, 64*total+1, 64*total+int64(len(inputs))+1); err != nil {
		return nil, err
	}
	out := &mkDocsPatterns{scope: s}
	for _, input := range inputs {
		if !utf8.ValidString(input) {
			return nil, &mkDocsPatternError{"invalid UTF-8"}
		}
		for _, line := range mkDocsSplitLines(input) {
			line = strings.TrimFunc(line, mkDocsPythonSpace)
			if line == "" || strings.HasPrefix(line, "#") || line == "/" {
				continue
			}
			include := true
			if strings.HasPrefix(line, "!") {
				include = false
				line = line[1:]
			}
			expression, err := mkDocsPatternRegex(line)
			if err != nil {
				return nil, err
			}
			// Character-class normalization and RE2 compilation are bounded by
			// this conservative quadratic reservation; matching is linear in
			// compiled instructions times input bytes. No uncharged regex entry.
			n := int64(len(expression)) + 1
			if err := s.checkpoint(ctx, 2*n*n+1, 32*n+1); err != nil {
				return nil, err
			}
			parsed, err := syntax.Parse(expression, syntax.Perl)
			if err != nil {
				return nil, &mkDocsPatternError{"invalid character class"}
			}
			program, err := syntax.Compile(parsed.Simplify())
			if err != nil {
				return nil, &mkDocsPatternError{"invalid pattern program"}
			}
			compiled, err := regexp.Compile(expression)
			if err != nil {
				return nil, &mkDocsPatternError{"invalid character class"}
			}
			out.rules = append(out.rules, mkDocsPattern{include, compiled, int64(len(program.Inst))})
		}
	}
	return out, s.checkpoint(ctx, 0, 0)
}

func (p *mkDocsPatterns) Match(ctx context.Context, s *Scope, name string) (bool, error) {
	if p == nil || p.scope != s || s == nil || ctx == nil {
		return false, ErrContract
	}
	if int64(len(name)) > s.operation.Budget().Limits().DocumentBytes {
		return false, &parser.LimitExceeded{Name: "pattern-name-bytes", Observed: int64(len(name)), Allowed: s.operation.Budget().Limits().DocumentBytes}
	}
	if err := s.checkpoint(ctx, int64(len(name))+1, 0); err != nil {
		return false, err
	}
	if !utf8.ValidString(name) {
		return false, ErrContract
	}
	matched, priority := false, 0
	for _, rule := range p.rules {
		// Reserve both NFA queues/threads and the bounded bit-state engine
		// bitmap before stdlib entry, even if its internal pool is warm.
		states := rule.states * (int64(len(name)) + 1)
		if err := s.checkpoint(ctx, 8*states+1, 16*rule.states+(states+63)/64+4); err != nil {
			return false, err
		}
		indices := rule.regex.FindStringSubmatchIndex(name)
		if indices == nil {
			continue
		}
		directory := len(indices) > 2 && indices[2] >= 0
		current := 2
		if directory {
			current = 1
		}
		if rule.include && directory || current >= priority {
			matched, priority = rule.include, current
		}
	}
	if err := s.checkpoint(ctx, 0, 0); err != nil {
		return false, err
	}
	return matched, nil
}

// Python str.strip includes the C0 separators U+001C..U+001F in addition to the
// Unicode White_Space property. Keep its exact set rather than OS/text policy.
func mkDocsPythonSpace(r rune) bool {
	return r >= '\t' && r <= '\r' || r >= 0x1c && r <= 0x20 || r == 0x85 || r == 0xa0 || r == 0x1680 || r >= 0x2000 && r <= 0x200a || r == 0x2028 || r == 0x2029 || r == 0x202f || r == 0x205f || r == 0x3000
}
func mkDocsSplitLines(input string) []string {
	var out []string
	start := 0
	for i, r := range input {
		if r != '\n' && r != '\r' && r != '\v' && r != '\f' && r != 0x1c && r != 0x1d && r != 0x1e && r != 0x85 && r != 0x2028 && r != 0x2029 {
			continue
		}
		if r == '\n' && i > 0 && input[i-1] == '\r' {
			start = i + 1
			continue
		}
		out = append(out, input[start:i])
		start = i + utf8.RuneLen(r)
	}
	if start < len(input) {
		out = append(out, input[start:])
	}
	return out
}

func mkDocsPatternRegex(pattern string) (string, error) {
	parts := strings.Split(pattern, "/")
	// Adjacent complete ** segments collapse; embedded ** remains segment *.
	compact := parts[:0]
	for _, part := range parts {
		if part == "**" && len(compact) > 0 && compact[len(compact)-1] == "**" {
			continue
		}
		compact = append(compact, part)
	}
	parts = compact
	if len(parts) == 2 && parts[0] == "**" && parts[1] == "" {
		return `^.+(/).*\z`, nil
	}
	if parts[0] == "" {
		parts = parts[1:]
	} else if len(parts) == 1 || len(parts) == 2 && parts[1] == "" {
		if parts[0] != "**" {
			parts = append([]string{"**"}, parts...)
		}
	}
	if len(parts) == 0 {
		return "", &mkDocsPatternError{"empty root pattern"}
	}
	if parts[len(parts)-1] == "" && len(parts) > 1 {
		parts[len(parts)-1] = "**"
	}
	var out strings.Builder
	out.WriteString("^")
	needSlash := false
	last := len(parts) - 1
	for i, part := range parts {
		switch part {
		case "**":
			switch {
			case i == 0 && i == last:
				out.WriteString(`[^/]+(?:(/).*)?`)
			case i == 0:
				out.WriteString(`(?:.+/)?`)
				needSlash = false
			case i == last:
				out.WriteString(`(/).*`)
			default:
				out.WriteString(`(?:/.+)?`)
				needSlash = true
			}
		case "*":
			if needSlash {
				out.WriteByte('/')
			}
			out.WriteString(`[^/]+`)
			if i == last {
				out.WriteString(`(?:(/).*)?`)
			}
			needSlash = true
		default:
			if needSlash {
				out.WriteByte('/')
			}
			segment, err := mkDocsSegmentRegex(part)
			if err != nil {
				return "", err
			}
			out.WriteString(segment)
			if i == last {
				out.WriteString(`(?:(/).*)?`)
			}
			needSlash = true
		}
	}
	out.WriteString(`\z`)
	return out.String(), nil
}
func mkDocsSegmentRegex(segment string) (string, error) {
	chars := []rune(segment)
	var out strings.Builder
	for i := 0; i < len(chars); i++ {
		r := chars[i]
		switch r {
		case '\\':
			i++
			if i == len(chars) {
				return "", &mkDocsPatternError{"trailing escape"}
			}
			out.WriteString(regexp.QuoteMeta(string(chars[i])))
		case '*':
			out.WriteString(`[^/]*`)
		case '?':
			out.WriteString(`[^/]`)
		case '[':
			j := i + 1
			if j < len(chars) && chars[j] == '!' {
				j++
			}
			if j < len(chars) && chars[j] == ']' {
				j++
			}
			for j < len(chars) && chars[j] != ']' {
				j++
			}
			if j == len(chars) {
				out.WriteString(`\[`)
				continue
			}
			out.WriteByte('[')
			k := i + 1
			if chars[k] == '!' {
				out.WriteByte('^')
				k++
			} else if chars[k] == '^' {
				out.WriteString(`\^`)
				k++
			}
			for ; k < j; k++ {
				switch chars[k] {
				case '\\':
					out.WriteString(`\\`)
				case ']':
					out.WriteString(`\]`)
				case '[':
					out.WriteString(`\[`)
				default:
					out.WriteRune(chars[k])
				}
			}
			out.WriteByte(']')
			i = j
		default:
			out.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	return out.String(), nil
}

var _ error = (*mkDocsPatternError)(nil)
