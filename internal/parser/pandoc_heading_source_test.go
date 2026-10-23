package parser

import (
	"context"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestPandocHeadingRegularSourceGrammar(t *testing.T) {
	// Literal independently authored boundaries, including successful fallback of
	// try parenthesizedChars/angle and title enders followed by alphanumeric text.
	for _, tc := range []struct {
		source string
		end    int
	}{
		{"()tail", 2}, {"(url)tail", 5}, {"(a b)tail", 5}, {"(a\tb)tail", 5},
		{"(a (b)c)tail", 8}, {"((unbalanced)tail", 0},
		{`(a\)b)tail`, 6}, {`(a\\)tail`, 5},
		{"(a\nb)tail", 5}, {"(a\n\nb)tail", 0},
		{"(<a)b>)tail", 7}, {"(<unclosed)tail", 11},
		{"(a \"title\")tail", 11}, {"(a\n\"title\")tail", 11},
		{"(a \" title\")tail", 0}, {"(a \"title\"x)tail", 0},
		{"(a \"t\"x\" z\")tail", 12},
		{"(a 'title')tail", 11}, {"(a (title))tail", 11},
		{"(data:text/plain;base64,YQ==)tail", 29},
		{"(data:text/plain;base64,YQ\n\n==)tail", 31},
		{"(a \"t\n\nt\")tail", 0},
	} {
		b := pandocHeadingBoundaries{ctx: context.Background(), text: tc.source}
		got := b.linkEnd(0)
		if got != tc.end {
			t.Errorf("%q end=%d want%d", tc.source, got, tc.end)
		}
	}
}

func TestPandocHeadingRegularSourceOwnership(t *testing.T) {
	for _, lines := range [][]string{
		{"[x](url {#wrong}", "more){#inline}"},
		{"![x](url {#wrong}", "more){#inline}"},
		{"[x](url", `"title {#wrong}`, `more"){#inline}`},
		{"[x](url", `"title) {#wrong}`, `more"){#inline}`},
		{"[x](<url {#wrong}", "more>){#inline}"},
		{"[x](<url) {#wrong}", "rest>){#inline}"},
		{"[x](data:text/plain;k=) {#wrong}", "rest;base64,YQ==){#inline}"},
		{`[x](url\) {#wrong}`, "more){#inline}"},
		{"[x](url (nested {#wrong}", "more)){#inline}"},
		{"[x](<fallback {#wrong}", "more){#inline}"},
	} {
		for _, prefix := range []string{"", "> ", "- "} {
			for _, eol := range []string{"\n", "\r\n"} {
				for _, separate := range []bool{false, true} {
					continuation := prefix
					if prefix == "- " {
						continuation = "  "
					}
					var input strings.Builder
					type fact struct {
						start, end    int
						raw, semantic string
					}
					var facts []fact
					for i, line := range lines {
						if i == 0 {
							input.WriteString(prefix + "# ")
						} else {
							input.WriteString(continuation)
						}
						from := input.Len()
						input.WriteString(line)
						facts = append(facts, fact{from, input.Len(), line, line})
						if i == len(lines)-1 {
							if separate {
								input.WriteString(" {#heading}")
							}
						} else {
							from = input.Len()
							facts = append(facts, fact{from, from + len(eol), eol, "\n"})
						}
						input.WriteString(eol)
					}
					original := input.String()
					r := pandocBlocks(t, original)
					nodes := r.Document.Root().Children()
					if prefix == "> " {
						nodes = nodes[0].Children()
					}
					if prefix == "- " {
						nodes = nodes[0].Children()[0].Children()
					}
					if len(nodes) != 1 || nodes[0].Kind() != Heading {
						t.Fatalf("tree %q: %v", original, pandocKinds(r.Document.Root()))
					}
					n := nodes[0]
					assertByteSpan(t, n.Span(), len(prefix), len(original), original[len(prefix):])
					a := n.Properties().Attributes
					if !separate && a != nil || separate && (a == nil || a.ID != Some("heading")) {
						t.Fatalf("owner %q: %+v", original, a)
					}
					parts := n.Content()
					if len(parts) != len(facts) {
						t.Fatalf("parts %q: %+v", original, parts)
					}
					for i, f := range facts {
						assertByteSpan(t, parts[i].Source, f.start, f.end, f.raw)
						if parts[i].Semantic != Some(f.semantic) {
							t.Fatalf("semantic %q part%d", original, i)
						}
					}
				}
			}
		}
	}
	// A malformed source cannot move any later physical line into the heading.
	for _, input := range []string{
		"# [x](url {#heading}\nmore{#inline}\n",
		"# [x](url {#heading}\n\nmore){#inline}\n",
		"# [x](url \" title\" {#heading}\nmore){#inline}\n",
	} {
		r := pandocBlocks(t, input)
		nodes := r.Document.Root().Children()
		firstEnd := strings.IndexByte(input, '\n') + 1
		if len(nodes) != 2 || nodes[0].Kind() != Heading || nodes[0].Span().End() != firstEnd || nodes[1].Kind() != Paragraph {
			t.Fatalf("fallback %q", input)
		}
		if a := nodes[0].Properties().Attributes; a == nil || a.ID != Some("heading") {
			t.Fatalf("fallback owner %q %+v", input, a)
		}
	}
}

func TestPandocHeadingFailedRegularSourceScaling(t *testing.T) {
	for _, n := range []int{1024, 2048, 4096, 8192, 16384} {
		for _, suffix := range []string{"", " {#heading}"} {
			pending := strings.Repeat("[x](", n)
			input := "# " + pending + suffix + "\n"
			budget, _ := NewOperationBudget(DefaultLimits())
			r := parseWithProfile(context.Background(), NewSource([]byte(input)), budget, pandocBlocksProfile)
			if r.Failure != nil || r.Interruption != "" {
				t.Fatalf("n%d: %+v", n, r)
			}
			nodes := r.Document.Root().Children()
			if len(nodes) != 1 || nodes[0].Kind() != Heading {
				t.Fatal("tree")
			}
			node := nodes[0]
			assertByteSpan(t, node.Span(), 0, len(input), input)
			a := node.Properties().Attributes
			if suffix == "" && a != nil || suffix != "" && (a == nil || a.ID != Some("heading")) {
				t.Fatal("later real attribute owner")
			}
			content := node.Content()
			if len(content) != 1 || content[0].Semantic != Some(pending) {
				t.Fatal("pending")
			}
			assertByteSpan(t, content[0].Source, 2, 2+len(pending), pending)
			if budget.work > int64(len(input))*150 {
				t.Fatalf("overlapping failed source work n%d work%d", n, budget.work)
			}
		}
	}
}

// Simple forward, backtracking reference for the source grammar. It shares no
// endpoint tables or production boundary decisions. Bounded short strings keep
// this intentionally unoptimized oracle separate from the scaling tests.
func TestPandocHeadingRegularSourceReference(t *testing.T) {
	lit := func(s string, i int) int {
		if i == len(s) {
			return 0
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		if r == '\\' && i+n < len(s) {
			q, m := utf8.DecodeRuneInString(s[i+n:])
			if q != '\n' && q != '\r' && !(unicode.IsLetter(q) || unicode.IsNumber(q)) {
				return i + n + m
			}
		}
		if r == '\n' {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
				j++
			}
			if j < len(s) && s[j] == '\n' {
				return 0
			}
		}
		return i + n
	}
	horizontal := func(s string, i int) int {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		return i
	}
	var balanced func(string, int) int
	balanced = func(s string, i int) int {
		if i == len(s) || s[i] != '(' {
			return 0
		}
		i++
		for i < len(s) {
			if s[i] == ')' {
				return i + 1
			}
			var next int
			if s[i] == '(' {
				next = balanced(s, i)
			} else {
				next = lit(s, i)
			}
			if next == 0 {
				return 0
			}
			i = next
		}
		return 0
	}
	var quoted func(string, int) int
	quoted = func(s string, i int) int {
		q := s[i]
		i++
		if i < len(s) {
			r, _ := utf8.DecodeRuneInString(s[i:])
			if unicode.IsSpace(r) {
				return 0
			}
		}
		for i < len(s) {
			if s[i] == q {
				r, _ := utf8.DecodeRuneInString(s[i+1:])
				if i+1 == len(s) || !(unicode.IsLetter(r) || unicode.IsNumber(r)) {
					return i + 1
				}
				if end := quoted(s, i); end > 0 {
					i = end
					continue
				}
			}
			next := lit(s, i)
			if next == 0 {
				return 0
			}
			i = next
		}
		return 0
	}
	title := func(s string, i int) int {
		i = horizontal(s, i)
		if i < len(s) && s[i] == '\n' {
			i = horizontal(s, i+1)
		}
		if i < len(s) && (s[i] == '"' || s[i] == '\'') {
			return quoted(s, i)
		}
		return 0
	}
	reference := func(s string) int {
		i := horizontal(s, 1)
		angle := 0
		if i < len(s) && s[i] == '<' {
			for j := i + 1; j < len(s); {
				if s[j] == '>' {
					angle = j + 1
					break
				}
				next := lit(s, j)
				if next == 0 {
					break
				}
				j = next
			}
		}
		if angle > 0 {
			i = angle
		} else {
			for i < len(s) {
				if s[i] == ')' {
					break
				}
				if s[i] == '(' {
					if end := balanced(s, i); end > 0 {
						i = end
						continue
					}
				}
				if s[i] == ' ' {
					next := horizontal(s, i)
					if next < len(s) && strings.ContainsRune("\"')", rune(s[next])) {
						break
					}
					i = next
					continue
				}
				if s[i] == '\n' && title(s, i) > 0 {
					break
				}
				next := lit(s, i)
				if next == 0 {
					break
				}
				i = next
			}
		}
		if end := title(s, i); end > 0 {
			i = end
		}
		i = horizontal(s, i)
		if i < len(s) && s[i] == ')' {
			return i + 1
		}
		return 0
	}
	count := 0
	var walk func(string, int)
	walk = func(s string, remaining int) {
		text := "(" + s
		b := pandocHeadingBoundaries{ctx: context.Background(), text: text}
		if got, want := b.linkEnd(0), reference(text); got != want {
			t.Fatalf("reference %q got%d want%d", text, got, want)
		}
		count++
		if remaining > 0 {
			for _, c := range []string{"a", "(", ")", " ", "\t", "\n", "\"", "'", "\\", "<", ">"} {
				walk(s+c, remaining-1)
			}
		}
	}
	walk("", 4)
	if count != 16105 {
		t.Fatalf("oracle case inventory %d", count)
	}
}

func TestPandocHeadingRegularSourceDocumentScaling(t *testing.T) {
	for _, unit := range []string{"# [x](\n", "# [x](<url) {#heading}\n", "# [x](data:text/plain;k=) {#heading}\n", "# [x](url\nmore){#inline}\n"} {
		for _, count := range []int{128, 512, 1024} {
			input := strings.Repeat(unit, count)
			budget, _ := NewOperationBudget(DefaultLimits())
			r := parseWithProfile(context.Background(), NewSource([]byte(input)), budget, pandocBlocksProfile)
			if r.Failure != nil || r.Interruption != "" {
				t.Fatalf("unit%q n%d: %+v", unit, count, r)
			}
			nodes := r.Document.Root().Children()
			if len(nodes) != count {
				t.Fatalf("unit%q n%d got%d", unit, count, len(nodes))
			}
			for i, node := range nodes {
				if node.Kind() != Heading {
					t.Fatal("kind")
				}
				assertByteSpan(t, node.Span(), i*len(unit), (i+1)*len(unit), unit)
				a := node.Properties().Attributes
				if strings.Contains(unit, "{#heading}") {
					if a == nil || a.ID != Some("heading") {
						t.Fatal("genuine heading suffix")
					}
				} else if a != nil {
					t.Fatal("inline owner")
				}
				var pending strings.Builder
				for _, part := range node.Content() {
					pending.WriteString(part.Semantic.Value)
				}
				want := strings.TrimSuffix(strings.TrimSuffix(unit[2:], "\n"), " {#heading}")
				if pending.String() != want {
					t.Fatalf("pending %q want%q", pending.String(), want)
				}
			}
			if budget.work > int64(len(input))*200 {
				t.Fatalf("unit%q n%d work%d", unit, count, budget.work)
			}
		}
	}
}

func TestPandocHeadingRegularSourceSetext(t *testing.T) {
	for _, pending := range []string{`[x](a (b)c){#inline}`, `![x](a\)b "title"){#inline}`, `[x](<a)b>){#inline}`, `[x](data:text/plain;base64,YQ==){#inline}`} {
		for _, prefix := range []string{"", "> ", "- "} {
			for _, eol := range []string{"\n", "\r\n"} {
				for _, separate := range []bool{false, true} {
					continuation := prefix
					if prefix == "- " {
						continuation = "  "
					}
					suffix := ""
					if separate {
						suffix = " {#heading}"
					}
					input := prefix + pending + suffix + eol + continuation + "---" + eol
					r := pandocBlocks(t, input)
					nodes := r.Document.Root().Children()
					if prefix == "> " {
						nodes = nodes[0].Children()
					}
					if prefix == "- " {
						nodes = nodes[0].Children()[0].Children()
					}
					if len(nodes) != 1 || nodes[0].Kind() != Heading {
						t.Fatalf("tree %q", input)
					}
					node := nodes[0]
					assertByteSpan(t, node.Span(), len(prefix), len(input), input[len(prefix):])
					a := node.Properties().Attributes
					if !separate && a != nil || separate && (a == nil || a.ID != Some("heading")) {
						t.Fatalf("owner %q", input)
					}
					parts := node.Content()
					if len(parts) != 1 || parts[0].Semantic != Some(pending) {
						t.Fatalf("pending %q", input)
					}
					assertByteSpan(t, parts[0].Source, len(prefix), len(prefix)+len(pending), pending)
				}
			}
		}
	}
}

func TestPandocHeadingRegularSourceAlternativeCommit(t *testing.T) {
	for _, tc := range []struct {
		input string
		id    bool
		full  bool
	}{
		{"# [x](<url){#inline}\nrest>oops\n", true, false},
		{"# [x](<url){#inline}\nrestoops\n", false, false},
		{"# [x](<url){#wrong}\nrest>){#inline}\n", false, true},
		{"# [x](data:text/plain;k=){#inline}\nrest;base64,YQ==oops\n", true, false},
	} {
		r := pandocBlocks(t, tc.input)
		nodes := r.Document.Root().Children()
		wantEnd := strings.IndexByte(tc.input, '\n') + 1
		if tc.full {
			wantEnd = len(tc.input)
		}
		if nodes[0].Kind() != Heading || nodes[0].Span().End() != wantEnd {
			t.Fatalf("extent %q", tc.input)
		}
		a := nodes[0].Properties().Attributes
		if tc.id && (a == nil || a.ID != Some("inline")) || !tc.id && a != nil {
			t.Fatalf("committed alternative owner %q %+v", tc.input, a)
		}
	}
}
