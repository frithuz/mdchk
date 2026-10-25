package metadata

import (
	"context"

	"github.com/frithuz/mdchk/internal/parser"
)

// tomlLexical is a nonrecursive admission scan, not a TOML parser. The maintained
// parser remains the syntax authority. It only skips quoted/comment payloads,
// bounds recursive value delimiters before native entry and records their exact
// authored envelopes (the upstream array AST deliberately has no Raw range).
type tomlLexical struct {
	ends       map[int]int
	standalone bool
}

func guardTOMLLexical(ctx context.Context, b Binding, text string) (tomlLexical, error) {
	out := tomlLexical{ends: make(map[int]int)}
	var stack []int
	value, header := false, false
	for i := 0; i < len(text); {
		if err := b.charge(ctx, 1, 0); err != nil {
			return out, err
		}
		c := text[i]
		switch c {
		case ' ', '\t', '\r':
			i++
		case '\n':
			if len(stack) == 0 {
				value, header = false, false
			}
			i++
		case '#':
			start := i
			for start > 0 && text[start-1] != '\n' {
				start--
			}
			standalone := true
			for j := start; j < i; j++ {
				if text[j] != ' ' && text[j] != '\t' && text[j] != '\r' {
					standalone = false
					break
				}
			}
			out.standalone = out.standalone || standalone
			for i < len(text) && text[i] != '\n' {
				if (i-start)%1024 == 0 {
					if err := b.charge(ctx, 0, 0); err != nil {
						return out, err
					}
				}
				i++
			}
		case '\'', '"':
			if value && !header {
				if err := tomlDepth(b, int64(len(stack))+2); err != nil {
					return out, err
				}
			}
			var err error
			i, err = tomlQuotedEnd(ctx, b, text, i)
			if err != nil {
				return out, err
			}
		case '=':
			value = true
			i++
		case '[', '{':
			if c == '[' && !value && len(stack) == 0 {
				header = true
				i++
				continue
			}
			if header {
				i++
				continue
			}
			if err := tomlDepth(b, int64(len(stack))+2); err != nil {
				return out, err
			}
			stack = append(stack, i)
			i++
		case ']', '}':
			if !header && len(stack) > 0 {
				start := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				out.ends[start] = i + 1
			}
			i++
		case ',', '.':
			i++
		default:
			if value && !header {
				if err := tomlDepth(b, int64(len(stack))+2); err != nil {
					return out, err
				}
			}
			i++
		}
	}
	return out, nil
}

// This mirrors only delimiter consumption in the pinned TOML 1.1 scanner:
// escapes consume their next byte, and a multiline closing run consumes all
// three through five quotes. In particular the extra one/two quotes are string
// content, not the opening of another string. Invalid strings remain the native
// parser's responsibility and cannot introduce native recursive value entry.
func tomlQuotedEnd(ctx context.Context, b Binding, text string, start int) (int, error) {
	quote := text[start]
	multi := start+2 < len(text) && text[start+1] == quote && text[start+2] == quote
	i := start + 1
	if multi {
		i = start + 3
	}
	nextCheckpoint := i + 1024
	for i < len(text) {
		if i >= nextCheckpoint {
			if err := b.charge(ctx, 0, 0); err != nil {
				return i, err
			}
			nextCheckpoint = i + 1024
		}
		if quote == '"' && text[i] == '\\' {
			i = min(i+2, len(text))
			continue
		}
		if text[i] != quote {
			i++
			continue
		}
		if !multi {
			return i + 1, nil
		}
		j := i
		for j < len(text) && text[j] == quote {
			if (j-i)%1024 == 0 {
				if err := b.charge(ctx, 0, 0); err != nil {
					return j, err
				}
			}
			j++
		}
		if j-i >= 3 {
			return j, nil
		}
		i = j
	}
	return len(text), nil
}

func tomlDepth(b Binding, depth int64) error {
	limit := b.state.budget.Limits().SyntaxDepth
	if depth > limit {
		return &parser.LimitExceeded{Name: "syntax-depth", Observed: depth, Allowed: limit}
	}
	return nil
}
