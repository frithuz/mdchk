package config

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
)

func assertCommentPosition(t *testing.T, o Origin, line, column int) {
	t.Helper()
	if o.Position == nil || o.Position.Line != line || o.Position.Column != column {
		t.Fatalf("origin %+v position %+v; want %d:%d", o, o.Position, line, column)
	}
}

func TestYAMLCommentLineOrigins(t *testing.T) {
	for _, tc := range []struct{ name, prefix string }{
		{"CRLF", "version: 1\r\n# 雪\r\n"},
		{"LF", "version: 1\n# 雪\n"},
		{"CR", "version: 1\r# 雪\r"},
		{"inline", "version: 1 # 雪\r\n# 次\r\n"},
		{"unicode-prefix", "version: 1\r\nflavor: '雪' # 次\r\n"},
		{"mixed", "version: 1\r# 雪\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte(tc.prefix + "rules:\r\n  MD013: {length: -1}\r\n# EOF 雪")
			saved := bytes.Clone(input)
			d, err := DecodeYAML("fixture.yaml", input)
			if err != nil {
				t.Fatal(err)
			}
			v := contractEntry(t, contractEntry(t, contractEntry(t, d.Syntax(), "rules"), "MD013"), "length")
			assertCommentPosition(t, v.Origin(), 4, 19)
			if !bytes.Equal(input, saved) {
				t.Fatal("input mutated")
			}
		})
	}
	d := decodeContractYAML(t, "version: 1\r\n# 一\r\n# 二\r\nrules:\r\n  first: &a {text: 雪}\r\n  # 三\r\n  second: *a\r\n")
	second := contractEntry(t, contractEntry(t, d.Syntax(), "rules"), "second")
	assertCommentPosition(t, second.Origin(), 7, 11)
	assertCommentPosition(t, contractEntry(t, second, "text").Origin(), 7, 11)
}

func TestYAMLCommentErrorOrigins(t *testing.T) {
	for _, tc := range []struct {
		source                 string
		line, column, previous int
	}{
		{"version: 1\r\n# 雪\r\nversion: 1\r\n", 3, 1, 1},
		{"version: 1\r\n# 雪\r\nrules:\r\n  x: {a: 1}\r\n  # 次\r\n  x: {}\r\n", 6, 3, 4},
		{"version: 1\r\n# 雪\r\nflavor: null\r\n", 3, 9, 0},
		{"version: 1\r\n# 雪\r\nrules: {x: *missing}\r\n", 3, 12, 0},
	} {
		_, err := DecodeYAML("fixture.yaml", []byte(tc.source))
		var de *DecodeError
		if !errors.As(err, &de) {
			t.Fatalf("want decode error: %v", err)
		}
		assertCommentPosition(t, de.Origin, tc.line, tc.column)
		if tc.previous > 0 {
			if de.Previous == nil {
				t.Fatal("missing duplicate first origin")
			}
			assertCommentPosition(t, *de.Previous, tc.previous, tc.column)
		}
	}
}

func TestYAMLBlocksAfterCRLFComments(t *testing.T) {
	for _, header := range []string{"|", ">", "!!str |", "!!str >"} {
		for _, body := range []string{"", "      雪\r\n      next\r\n"} {
			for _, headerComment := range []string{"", " # header 雪"} {
				t.Run(header+body+headerComment, func(t *testing.T) {
					text := "version: 1\r\n# before 雪\r\nrules:\r\n  future:\r\n    text: &a " + header + headerComment + "\r\n" + body + "    sibling: next # after\r\n    alias: *a\r\n# end\r\n"
					d := decodeContractYAML(t, text)
					options := contractEntry(t, contractEntry(t, d.Syntax(), "rules"), "future")
					v := contractEntry(t, options, "text")
					want := ""
					if body != "" {
						want = "雪\nnext\n"
						if strings.Contains(header, ">") {
							want = "雪 next\n"
						}
					}
					if v.StringValue() != want {
						t.Fatalf("block text %q want %q", v.StringValue(), want)
					}
					rawHeader := strings.TrimPrefix(header, "!!str ")
					if v.Spelling() != rawHeader+headerComment+"\r\n"+body {
						t.Fatalf("spelling %q", v.Spelling())
					}
					assertCommentPosition(t, v.Origin(), 5, 14)
					siblingLine := 6
					if body != "" {
						siblingLine = 8
					}
					assertCommentPosition(t, contractEntry(t, options, "sibling").Origin(), siblingLine, 14)
					alias := contractEntry(t, options, "alias")
					assertCommentPosition(t, alias.Origin(), siblingLine+1, 12)
					if alias.StringValue() != want || alias.Spelling() != v.Spelling() {
						t.Fatal("block alias lost value/spelling")
					}
				})
			}
		}
	}
}

func TestCommentLineRepairOnlyChangesLines(t *testing.T) {
	text := "version: 1\r\n# 雪\r\nrules:\r\n  x:\r\n    a: | # header\r\n      foo\r\n    b: ok # after\r\n    c: end\r\n"
	tokens := lexer.Tokenize(text)
	before := make([]token.Token, len(tokens))
	positions := make([]token.Position, len(tokens))
	for i, tk := range tokens {
		before[i] = *tk
		positions[i] = *tk.Position
	}
	normalizeCommentLines(text, yamlLineStarts(text), tokens)
	for i, tk := range tokens {
		got := *tk
		got.Position = before[i].Position
		if !reflect.DeepEqual(got, before[i]) {
			t.Fatalf("token %d metadata changed", i)
		}
		pos := *tk.Position
		pos.Line = positions[i].Line
		if pos != positions[i] {
			t.Fatalf("token %d non-line position changed", i)
		}
	}
	for _, mutate := range []func(*token.Token){
		func(tk *token.Token) { tk.Value = "wrong" },
		func(tk *token.Token) { tk.Position.Column++ },
		func(tk *token.Token) { tk.Origin = "# 雪\r\n" },
	} {
		tokens := lexer.Tokenize("# 雪\r\nversion: 1\r\n")
		mutate(tokens[0])
		line := tokens[1].Position.Line
		normalizeCommentLines("# 雪\r\nversion: 1\r\n", yamlLineStarts("# 雪\r\nversion: 1\r\n"), tokens)
		if tokens[1].Position.Line != line {
			t.Fatal("unverified comment boundary corrected")
		}
	}
}

func TestCRLFCommentRepairKeepsSyntaxRejections(t *testing.T) {
	for _, fragment := range []string{
		"rules: {x: [unterminated}\r\n",
		"rules:\r\n  x:\r\n    text: |0\r\n      bad\r\n",
		"rules:\r\n  x:\r\n    text: |++\r\n      bad\r\n",
		"rules: {x: &a {value: *a}}\r\n",
		"rules: {x: *missing}\r\n",
		"rules:\r\n  x:\r\n    text: |\r\n    next: [bad}\r\n",
	} {
		if d, err := DecodeYAML("fixture.yaml", []byte("version: 1\r\n# 雪\r\n"+fragment)); err == nil || d != nil {
			t.Fatalf("accepted malformed input %q", fragment)
		}
	}
}
