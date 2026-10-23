package parser

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestPandocHeadingInlineAttributeOwnership(t *testing.T) {
	tests := []struct{ text, pending, id string }{
		{"[\\verb§a]b§]{#inline}", "[\\verb§a]b§]{#inline}", ""},
		{"[\\Verb☆a]b☆]{#inline}", "[\\Verb☆a]b☆]{#inline}", ""},
		{"[\\lstinline|a]b|]{#inline}", "[\\lstinline|a]b|]{#inline}", ""},
		{"[\\mintinline{tex}|a]b|]{#inline}", "[\\mintinline{tex}|a]b|]{#inline}", ""},
		{"\\LaTeX{#heading}", "\\LaTeX", "heading"},
		{"\\textbf{x}{#heading}", "\\textbf{x}", "heading"},
		{"\\unknown{x}{#heading}", "\\unknown{x}{#heading}", ""},
		{"$x${#heading}", "$x$", "heading"},
		{"<i>{#heading}", "<i>", "heading"},
		{"[\\begin{math}a]b\\end{math}]{#inline}", "[\\begin{math}a]b\\end{math}]{#inline}", ""},
		{"[\\begin{displaymath}a]b\\end{displaymath}]{#inline}", "[\\begin{displaymath}a]b\\end{displaymath}]{#inline}", ""},
		{"[$a\\$]b$]{#inline}", "[$a\\$]b$]{#inline}", ""},
		{"[\\unknown{a\\}b]c}]{#inline}", "[\\unknown{a\\}b]c}]{#inline}", ""},
		{"[\\begin{unknown}a]b\\end{unknown}]{#heading}", "[\\begin{unknown}a]b\\end{unknown}]", "heading"},
		{"[\\begin{itemize}a]b\\end{itemize}]{#heading}", "[\\begin{itemize}a]b\\end{itemize}]", "heading"},
		{"[\\startfoo{a]b}\\stopfoo]{#heading}", "[\\startfoo{a]b}\\stopfoo]", "heading"},
		{"[\\start[foo]{a]b}\\stop[foo]]{#heading}", "[\\start[foo]{a]b}\\stop[foo]]", "heading"},
		{"[\\LaTeX{]x}]{#heading}", "[\\LaTeX{]x}]", "heading"},
		{"[\\section{]x}]{#heading}", "[\\section{]x}]", "heading"},
		{"[\\textbf]x]{#inline}", "[\\textbf]x]{#inline}", ""},
		{"[\\unknown{a\\}b]c}]{#inline}", "[\\unknown{a\\}b]c}]{#inline}", ""},
		{"[[x](u){k=\"]\"}]{#heading}", "[[x](u){k=\"]\"}]", "heading"},
		{"[![x](u){k=\"]\"}]{#heading}", "[![x](u){k=\"]\"}]", "heading"},
		{"[$a]b$]{#inline}", "[$a]b$]{#inline}", ""},
		{"[$a]b$]{#inline} {#heading}", "[$a]b$]{#inline}", "heading"},
		{"[$]$x]{#inline}", "[$]$x]{#inline}", ""},
		{"[$]$x]{#inline} {#heading}", "[$]$x]{#inline}", "heading"},
		{"[<span title=\"]\">x</span>]{#inline}", "[<span title=\"]\">x</span>]{#inline}", ""},
		{"[<span title=\"]\">x</span>]{#inline} {#heading}", "[<span title=\"]\">x</span>]{#inline}", "heading"},
		{"[<i title=']'>x</i>]{#inline}", "[<i title=']'>x</i>]{#inline}", ""},
		{"[<i title=']'>x</i>]{#inline} {#heading}", "[<i title=']'>x</i>]{#inline}", "heading"},
		{"[<custom-tag k=\"]\">x</custom-tag>]{#inline}", "[<custom-tag k=\"]\">x</custom-tag>]{#inline}", ""},
		{"[<custom-tag k=\"]\">x</custom-tag>]{#inline} {#heading}", "[<custom-tag k=\"]\">x</custom-tag>]{#inline}", "heading"},
		{"[<!-- ] -->x]{#inline}", "[<!-- ] -->x]{#inline}", ""},
		{"[<!-- ] -->x]{#inline} {#heading}", "[<!-- ] -->x]{#inline}", "heading"},
		{"[<style k=\"]\">x</style>]{#inline}", "[<style k=\"]\">x</style>]{#inline}", ""},
		{"[<style k=\"]\">x</style>]{#inline} {#heading}", "[<style k=\"]\">x</style>]{#inline}", "heading"},
		{"[<script type=\"math/tex\" k=\"]\">x</script>]{#inline}", "[<script type=\"math/tex\" k=\"]\">x</script>]{#inline}", ""},
		{"[<script type=\"math/tex\" k=\"]\">x</script>]{#inline} {#heading}", "[<script type=\"math/tex\" k=\"]\">x</script>]{#inline}", "heading"},
		{"[$$a]b$$]{#inline}", "[$$a]b$$]{#inline}", ""},
		{"[$$a]b$$]{#inline} {#heading}", "[$$a]b$$]{#inline}", "heading"},
		{"[$a{b]$c}d$]{#inline}", "[$a{b]$c}d$]{#inline}", ""},
		{"[$a{b]$c}d$]{#inline} {#heading}", "[$a{b]$c}d$]{#inline}", "heading"},
		{"[\\unknown{a]b}]{#inline}", "[\\unknown{a]b}]{#inline}", ""},
		{"[\\unknown{a]b}]{#inline} {#heading}", "[\\unknown{a]b}]{#inline}", "heading"},
		{"[\\unknown[a{]}b]{c]d}]{#inline}", "[\\unknown[a{]}b]{c]d}]{#inline}", ""},
		{"[\\unknown[a{]}b]{c]d}]{#inline} {#heading}", "[\\unknown[a{]}b]{c]d}]{#inline}", "heading"},
		{"[\\textbf{a]b}]{#inline}", "[\\textbf{a]b}]{#inline}", ""},
		{"[\\textbf{a]b}]{#inline} {#heading}", "[\\textbf{a]b}]{#inline}", "heading"},
		{"[\\input{a]b}]{#inline}", "[\\input{a]b}]{#inline}", ""},
		{"[\\input{a]b}]{#inline} {#heading}", "[\\input{a]b}]{#inline}", "heading"},
		{"[\\verb|a]b|]{#inline}", "[\\verb|a]b|]{#inline}", ""},
		{"[\\verb|a]b|]{#inline} {#heading}", "[\\verb|a]b|]{#inline}", "heading"},
		{"<http:a<b>{#inline}", "<http:a<b>{#inline}", ""},
		{"<http:a<b>{#inline} {#heading}", "<http:a<b>{#inline}", "heading"},
		{"<a@b<c>{#inline}", "<a@b<c>{#inline}", ""},
		{"<a@b<c>{#inline} {#heading}", "<a@b<c>{#inline}", "heading"},
		{"<http:a b>{#inline}", "<http:a b>{#inline}", ""},
		{"<http:a b>{#inline} {#heading}", "<http:a b>{#inline}", "heading"},
		{"<a@b c>{#inline}", "<a@b c>{#inline}", ""},
		{"<a@b c>{#inline} {#heading}", "<a@b c>{#inline}", "heading"},
		{"<http:a\u000bb>{#inline}", "<http:a\u000bb>{#inline}", ""},
		{"<http:a\u000bb>{#inline} {#heading}", "<http:a\u000bb>{#inline}", "heading"},
		{"<a@b\fc>{#inline}", "<a@b\fc>{#inline}", ""},
		{"<a@b\fc>{#inline} {#heading}", "<a@b\fc>{#inline}", "heading"},
		{"[$a]b]{#heading}", "[$a]b]", "heading"},
		{"[$ a]b$]{#heading}", "[$ a]b$]", "heading"},
		{"[$a]b $]{#heading}", "[$a]b $]", "heading"},
		{"[$a]b$2]{#heading}", "[$a]b$2]", "heading"},
		{"[<span.title=\"]\">x]{#heading}", "[<span.title=\"]\">x]", "heading"},
		{"[<div title=\"]\">x</div>]{#heading}", "[<div title=\"]\">x</div>]", "heading"},
		{"[<script k=\"]\">x</script>]{#heading}", "[<script k=\"]\">x</script>]", "heading"},
		{"[\\textbf{x}{]z}]{#heading}", "[\\textbf{x}{]z}]", "heading"},
		{"<http: a>{#heading}", "<http: a>", "heading"},
		{"<http:\u000ba>{#heading}", "<http:\u000ba>", "heading"},
		{"<http:\fa>{#heading}", "<http:\fa>", "heading"},
		{"<a@ b>{#heading}", "<a@ b>", "heading"},
		{"<a@\u000bb>{#heading}", "<a@\u000bb>", "heading"},
		{"<http:<a>{#heading}", "<http:<a>", "heading"},
		{"<a@<b>{#heading}", "<a@<b>", "heading"},

		{"`x`{#inline}", "`x`{#inline}", ""},
		{"[x]{#inline}", "[x]{#inline}", ""},
		{"[x](u){#inline}", "[x](u){#inline}", ""},
		{"![x](u){#inline}", "![x](u){#inline}", ""},
		{"[x][ref]{#inline}", "[x][ref]{#inline}", ""},
		{"[x](u(v) \"title\"){#inline}", "[x](u(v) \"title\"){#inline}", ""},
		{"`x`{#inline} {#heading}", "`x`{#inline}", "heading"},
		{"[x]{#inline} {#heading}", "[x]{#inline}", "heading"},
		{"[x](u){#inline} {#heading}", "[x](u){#inline}", "heading"},
		{"`x`{#inline}{#heading}", "`x`{#inline}", "heading"},
		{"`x{#heading}", "`x", "heading"},
		{"[x{#heading}", "[x", "heading"},
		{"[x](u {#heading}", "[x](u", "heading"},
		{"`x`{#broken", "`x`{#broken", ""},
		{"[x]{#broken", "[x]{#broken", ""},
		{"<:>{#heading}", "<:>", "heading"},
		{"<@>{#heading}", "<@>", "heading"},
		{"<https://example.com>{#inline}", "<https://example.com>{#inline}", ""},
		{"<a@example.com>{#inline}", "<a@example.com>{#inline}", ""},
		{"<É@例.example>{#inline}", "<É@例.example>{#inline}", ""},
		{"<http:>{#heading}", "<http:>", "heading"},
		{"<unknown:valid>{#heading}", "<unknown:valid>", "heading"},
		{"<http:_bad>{#heading}", "<http:_bad>", "heading"},
		{"<http:!>{#heading}", "<http:!>", "heading"},
		{"<.a@b>{#heading}", "<.a@b>", "heading"},
		{"<http:!a>{#inline}", "<http:!a>{#inline}", ""},
		{"<http:!!a>{#heading}", "<http:!!a>", "heading"},
		{"<a@b!>{#inline}", "<a@b!>{#inline}", ""},
		{"<a@->{#heading}", "<a@->", "heading"},
		{"<https://example.com>{#inline} {#heading}", "<https://example.com>{#inline}", "heading"},
		{"[@doe]{#inline}", "[@doe]{#inline}", ""},
		{"^[note]{#inline}", "^[note]{#inline}", ""},
		{"[`x`{k=\"]\"}]{#inline}", "[`x`{k=\"]\"}]{#inline}", ""},
	}
	for _, tt := range tests {
		for _, setext := range []bool{false, true} {
			for _, prefix := range []string{"", "> ", "- "} {
				for _, ending := range []string{"\n", "\r\n"} {
					first := tt.text
					contentStart := len(prefix)
					if !setext {
						first = "# " + first
						contentStart += 2
					}
					input := prefix + first + ending
					if setext {
						continuation := prefix
						if prefix == "- " {
							continuation = "  "
						}
						input += continuation + "---" + ending
					}
					r := pandocBlocks(t, input)
					root := r.Document.Root()
					children := root.Children()
					if prefix == "> " {
						if len(children) != 1 || children[0].Kind() != BlockQuote {
							t.Fatal("quote ownership")
						}
						children = children[0].Children()
					}
					if prefix == "- " {
						if len(children) != 1 || children[0].Kind() != List || len(children[0].Children()) != 1 {
							t.Fatal("list ownership")
						}
						item := children[0].Children()[0]
						children = item.Children()
					}
					if len(children) != 1 || children[0].Kind() != Heading {
						t.Fatalf("heading %q: %v", input, pandocKinds(root))
					}
					n := children[0]
					assertByteSpan(t, n.Span(), len(prefix), len(input), input[len(prefix):])
					content := n.Content()
					if len(content) != 1 {
						t.Fatalf("pending source %q: %+v", input, content)
					}
					if content[0].Source.Start() != contentStart || content[0].Source.End() != contentStart+len(tt.pending) {
						t.Fatalf("pending source %q got [%d,%d), want [%d,%d)", input, content[0].Source.Start(), content[0].Source.End(), contentStart, contentStart+len(tt.pending))
					}
					assertByteSpan(t, content[0].Source, contentStart, contentStart+len(tt.pending), tt.pending)
					if content[0].Semantic != Some(tt.pending) || len(n.Children()) != 0 {
						t.Fatal("pending inline payload interpreted or lost")
					}
					attrs := n.Properties().Attributes
					if tt.id == "" {
						if attrs != nil {
							t.Fatalf("inline group stolen by heading: %q %+v", input, attrs)
						}
					} else {
						if attrs == nil || attrs.ID != Some(tt.id) || len(attrs.Occurrences) != 1 {
							t.Fatalf("genuine heading attrs lost %q %+v", input, attrs)
						}
						groupStart := contentStart + len(tt.pending)
						if tt.text[len(tt.pending)] == ' ' {
							groupStart++
						}
						assertValueRange(t, attrs.Raw, groupStart, groupStart+10, "{#heading}", "{#heading}")
					}
				}
			}
		}
	}
}

func TestPandocHeadingMultilineInlineOwnership(t *testing.T) {
	for _, prefix := range []string{"", "> "} {
		for _, ending := range []string{"\n", "\r\n"} {
			input := prefix + "# `x`{#inline" + ending + prefix + " .X} {#heading}" + ending
			r := pandocBlocks(t, input)
			children := r.Document.Root().Children()
			if prefix != "" {
				children = children[0].Children()
			}
			n := children[0]
			if n.Kind() != Heading || n.Properties().Attributes.ID != Some("heading") {
				t.Fatal(n.spec)
			}
			expectedRaw := "`x`{#inline" + ending + " .X}"
			var raw strings.Builder
			for _, part := range n.Content() {
				value, _ := r.Source.Text(part.Source)
				raw.WriteString(value)
				if strings.Contains(value, "> ") {
					t.Fatal("prefix gap lost")
				}
			}
			if raw.String() != expectedRaw {
				t.Fatalf("multiline inline source %q want%q", raw.String(), expectedRaw)
			}
			assertByteSpan(t, n.Span(), len(prefix), len(input), input[len(prefix):])
		}
	}
}

func TestPandocResumableAttributeScaling(t *testing.T) {
	for _, n := range []int{32, 64, 128, 256} {
		for _, separator := range []string{" ", "\n"} {
			input := "``` {" + strings.Repeat(".a"+separator, n) + "}\nx\n```\n"
			r := pandocBlocks(t, input)
			node := r.Document.Root().Children()[0]
			a := node.Properties().Attributes
			if node.Kind() != CodeBlock || len(a.Occurrences) != n || len(a.Classes) != n || node.Properties().Literal != Some("x") || node.Properties().PandocFence.Language != Some("a") {
				t.Fatal("finite family semantics")
			}
			assertByteSpan(t, node.Span(), 0, 13+3*n, input)
			for i, o := range a.Occurrences {
				assertValueRange(t, o.Raw, 5+3*i, 7+3*i, ".a", ".a")
				if a.Classes[i] != "a" {
					t.Fatal("classes")
				}
			}
		}
	}
	for _, kind := range []Kind{Heading, Div, CodeBlock} {
		for _, prefix := range []string{"", "> "} {
			group := "{" + strings.Repeat(".a\r\n", 256) + "}"
			lines := []string{group}
			switch kind {
			case Heading:
				lines[0] = "# H " + group
			case Div:
				lines[0] = "::: " + group
				lines = append(lines, "\r\nx\r\n:::")
			case CodeBlock:
				lines[0] = "``` " + group
				lines = append(lines, "\r\nx\r\n```")
			case DocumentKind, Paragraph, ThematicBreak, BlockQuote, List, ListItem, RawBlock, HTMLBlock, InlineHTML, Text, SoftBreak, HardBreak, CodeSpan, Emphasis, Strong, Strike, Link, Image, Table, TableRow, TableCell, DefinitionList, DefinitionItem, DefinitionTerm, DefinitionBody, LineBlock, LineBlockLine:
				t.Fatal("test kind")
			}
			logical := strings.Join(lines, "") + "\r\n"
			input := prefix + strings.ReplaceAll(logical, "\r\n", "\r\n"+prefix)
			if prefix != "" {
				input = strings.TrimSuffix(input, prefix)
			}
			r := pandocBlocks(t, input)
			children := r.Document.Root().Children()
			if prefix != "" {
				children = children[0].Children()
			}
			node := children[0]
			if node.Kind() != kind || len(node.Properties().Attributes.Classes) != 256 {
				t.Fatalf("collector caller %v", pandocKinds(r.Document.Root()))
			}
			for i, o := range node.Properties().Attributes.Occurrences {
				start := 5 + len(prefix) + (4+len(prefix))*i
				assertByteSpan(t, o.Raw.Segments[0].Source, start, start+2, ".a")
				if len(o.Raw.Segments) != 1 || o.Raw.Semantic != Some(".a") {
					t.Fatal("mapped occurrence")
				}
			}
		}
	}
}

func TestPandocResumableQuotedAlternativeControls(t *testing.T) {
	for _, tt := range []struct {
		group string
		want  []AttributeKeyValue
	}{
		{"{k=\"a}\nb\" .X}", []AttributeKeyValue{{"k", "a} b"}}},
		{"{k=abc\\\n .X}", []AttributeKeyValue{{"k", "abc\\"}}},
		{"{k=ab\"cd\n .X}", []AttributeKeyValue{{"k", "ab\"cd"}}},
		{"{k=\"a}\n .X}", []AttributeKeyValue{{"k", "\"a"}}},
	} {
		input := "``` " + tt.group + "\nx\n```\n"
		r := pandocBlocks(t, input)
		node := r.Document.Root().Children()[0]
		if node.Kind() != CodeBlock || !reflect.DeepEqual(node.Properties().Attributes.KeyValues, tt.want) {
			t.Fatalf("quoted control %q %v", input, pandocKinds(r.Document.Root()))
		}
		expectedLiteral := "x"
		if tt.group == "{k=\"a}\n .X}" {
			expectedLiteral = " .X}\nx"
		}
		if node.Properties().Literal != Some(expectedLiteral) {
			t.Fatalf("quoted fallback payload %q", node.Properties().Literal.Value)
		}
	}
	for _, input := range []string{"``` {k=\" leading\"\n .X}\nx\n```\n", "``` {\n.X\n", "::: {#bad\n\ntext\n"} {
		budget, _ := NewOperationBudget(DefaultLimits())
		r := parseWithProfile(context.Background(), NewSource([]byte(input)), budget, pandocBlocksProfile)
		if r.Failure != nil {
			t.Fatal(r.Failure)
		}
		if r.Document.parsedComplete {
			t.Fatal("failed speculation authority")
		}
	}
}

// This literal canonical-set digest was derived independently from the adopted
// URI.hs reference, not the Go constant. It checks every pinned identifier and
// detects wrapping splits, duplicates, omissions and invented scheme tokens.
func TestPandocHeadingPinnedSchemeInventory(t *testing.T) {
	names := strings.Fields(pandocHeadingSchemes)
	if len(names) != 268 {
		t.Fatalf("scheme inventory count%d", len(names))
	}
	sort.Strings(names)
	const expected = "7337f2275b3a91b0e99b919787557791059abd387eead191e45dfd081c8b5dd5"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(names, "\n")))); got != expected {
		t.Fatalf("pinned scheme set digest%s", got)
	}
	for _, scheme := range names {
		input := "# <" + scheme + ":a>{#inline}\n"
		n := pandocBlocks(t, input).Document.Root().Children()[0]
		if n.Properties().Attributes != nil {
			t.Fatalf("valid pinned scheme%s lost inline ownership", scheme)
		}
		content := n.Content()
		if len(content) != 1 {
			t.Fatal("pending URI content segments")
		}
		assertByteSpan(t, content[0].Source, 2, len(input)-1, input[2:len(input)-1])
	}
	for _, scheme := range []string{"browser-extension", "connectabledevices", "infopath", "ms-", "ms-settings-", "secondary-screen-controller", "settings-bluetooth", "settings-power", "whiteboard"} {
		input := "# <" + scheme + ":a>{#heading}\n"
		n := pandocBlocks(t, input).Document.Root().Children()[0]
		if n.Properties().Attributes == nil || n.Properties().Attributes.ID != Some("heading") {
			t.Fatalf("invented scheme%s hid heading attributes", scheme)
		}
	}
}

func TestPandocHeadingFailedDelimiterScaling(t *testing.T) {
	for _, char := range []string{"a", "[", "`", "<", "<a", "<http:a"} {
		for _, size := range []int{1024, 2048, 4096, 8192, 16384, 32768} {
			input := "# " + strings.Repeat(char, size) + "\n"
			budget, _ := NewOperationBudget(DefaultLimits())
			r := parseWithProfile(context.Background(), NewSource([]byte(input)), budget, pandocBlocksProfile)
			if r.Failure != nil || r.Interruption != "" || r.Status != Incomplete || r.AST != Partial {
				t.Fatalf("%q/%d: %+v", char, size, r)
			}
			children := r.Document.Root().Children()
			if len(children) != 1 || children[0].Kind() != Heading || children[0].Properties().Attributes != nil || len(children[0].Children()) != 0 {
				t.Fatalf("%q/%d ownership", char, size)
			}
			node := children[0]
			assertByteSpan(t, node.Span(), 0, len(input), input)
			content := node.Content()
			if len(content) != 1 || content[0].Semantic != Some(strings.Repeat(char, size)) {
				t.Fatalf("%q/%d content", char, size)
			}
			assertByteSpan(t, content[0].Source, 2, 2+len(char)*size, strings.Repeat(char, size))
			// A finite practical bound, not a statement about arbitrary grammar/input.
			if budget.work > int64(size*len(char))*100+10000 {
				t.Fatalf("%q/%d repeated visits: %d", char, size, budget.work)
			}
		}
	}
	// Failed outer probes must not suppress real later suffixes. Reuse all exact
	// envelope/prefix/CRLF/private-authority assertions in the ownership matrix.
}

func TestPandocHeadingMultilineOpaqueOwnership(t *testing.T) {
	for _, pair := range [][2]string{{`\foo[{#wrong}`, "opt]"}, {`\foo[{#wrong}`, "opt] {#heading}"}, {"[<i title='{#wrong}", "x'>y</i>]{#inline}"}, {"[$x {#wrong}", "y$]{#inline}"}, {"[`x {#wrong}", "y`]{#inline}"}, {`[\unknown{x {#wrong}`, `y}]{#inline}`}} {
		for _, prefix := range []string{"", "> ", "- "} {
			for _, ending := range []string{"\n", "\r\n"} {
				for _, separate := range []bool{false, true} {
					if separate && strings.HasPrefix(pair[0], `\foo[`) {
						continue
					}
					continuation := prefix
					if prefix == "- " {
						continuation = "  "
					}
					suffix := ""
					if separate {
						suffix = " {#heading}"
					}
					first := prefix + "# " + pair[0]
					second := continuation + pair[1] + suffix
					input := first + ending + second + ending
					r := pandocBlocks(t, input)
					children := r.Document.Root().Children()
					if prefix == "> " {
						children = children[0].Children()
					}
					if prefix == "- " {
						children = children[0].Children()[0].Children()
					}
					if len(children) != 1 || children[0].Kind() != Heading {
						t.Fatalf("tree %q: %v", input, pandocKinds(r.Document.Root()))
					}
					node := children[0]
					assertByteSpan(t, node.Span(), len(prefix), len(input), input[len(prefix):])
					attrs := node.Properties().Attributes
					if !separate && attrs != nil || separate && (attrs == nil || attrs.ID != Some("heading")) {
						t.Fatalf("attrs %q %+v", input, attrs)
					}
					content := node.Content()
					if len(content) != 3 {
						t.Fatalf("segments %q: %+v", input, content)
					}
					assertByteSpan(t, content[0].Source, len(prefix)+2, len(first), pair[0])
					assertByteSpan(t, content[1].Source, len(first), len(first)+len(ending), ending)
					secondStart := len(first) + len(ending) + len(continuation)
					assertByteSpan(t, content[2].Source, secondStart, secondStart+len(pair[1]), pair[1])
					if content[0].Semantic != Some(pair[0]) || content[1].Semantic != Some("\n") || content[2].Semantic != Some(pair[1]) || len(node.Children()) != 0 {
						t.Fatal("pending bytes changed")
					}
				}
			}
		}
	}
	for _, input := range []string{"# [<i title='{#heading}\nordinary\n", "# [$x {#heading}\nordinary\n"} {
		r := pandocBlocks(t, input)
		children := r.Document.Root().Children()
		if len(children) != 2 || children[0].Kind() != Heading || children[0].Properties().Attributes.ID != Some("heading") || children[1].Kind() != Paragraph {
			t.Fatalf("malformed rollback %q: %v", input, pandocKinds(r.Document.Root()))
		}
	}
}

func TestPandocHeadingDocumentScaling(t *testing.T) {
	tests := []struct {
		input, pending string
		multi          bool
	}{
		{"# [\n", "[", false},
		{"# <a\n", "<a", false},
		{"# <http:a\n", "<http:a", false},
		{"# `a\nb`{#inline}\n", "`a\nb`{#inline}", true},
		{"# [<i title='{#wrong}\nx'>y</i>]{#inline}\n", "[<i title='{#wrong}\nx'>y</i>]{#inline}", true},
	}
	for _, tt := range tests {
		for _, count := range []int{128, 512, 1024} {
			input := strings.Repeat(tt.input, count)
			budget, _ := NewOperationBudget(DefaultLimits())
			r := parseWithProfile(context.Background(), NewSource([]byte(input)), budget, pandocBlocksProfile)
			if r.Failure != nil || r.Interruption != "" || r.Status != Incomplete || r.AST != Partial {
				t.Fatalf("%q/%d: %+v", tt.input, count, r)
			}
			nodes := r.Document.Root().Children()
			if len(nodes) != count {
				t.Fatalf("%q/%d: %d headings", tt.input, count, len(nodes))
			}
			for i, n := range nodes {
				if n.Kind() != Heading || n.Properties().Attributes != nil || len(n.Children()) != 0 {
					t.Fatalf("%q/%d node %d", tt.input, count, i)
				}
				base := i * len(tt.input)
				assertByteSpan(t, n.Span(), base, base+len(tt.input), tt.input)
				content := n.Content()
				var got strings.Builder
				for _, part := range content {
					got.WriteString(part.Semantic.Value)
				}
				if got.String() != tt.pending {
					t.Fatalf("%q/%d pending %q", tt.input, count, got.String())
				}
				if !tt.multi {
					if len(content) != 1 {
						t.Fatal("single line ranges")
					}
					assertByteSpan(t, content[0].Source, base+2, base+len(tt.input)-1, tt.pending)
				} else {
					split := strings.IndexByte(tt.input, '\n')
					if len(content) != 3 {
						t.Fatal("multiline ranges")
					}
					assertByteSpan(t, content[0].Source, base+2, base+split, tt.input[2:split])
					assertByteSpan(t, content[1].Source, base+split, base+split+1, "\n")
					assertByteSpan(t, content[2].Source, base+split+1, base+len(tt.input)-1, tt.input[split+1:len(tt.input)-1])
				}
			}
			if budget.nodes > int64(count)*120+100 || budget.work > int64(len(input))*200+10000 {
				t.Fatalf("%q/%d repeated suffix construction: work%d records%d", tt.input, count, budget.work, budget.nodes)
			}
		}
	}
}

func TestPandocHeadingRecursiveDocumentScaling(t *testing.T) {
	for _, lines := range [][]string{{"# ["}, {"# <a"}, {"# <http:a"}, {"# `a", "b`{#inline}"}, {"# [<i title='{#wrong}", "x'>y</i>]{#inline}"}} {
		for _, prefix := range []string{"> ", "- "} {
			for _, ending := range []string{"\n", "\r\n"} {
				continuation := prefix
				if prefix == "- " {
					continuation = "  "
				}
				unit := prefix + lines[0] + ending
				if len(lines) == 2 {
					unit += continuation + lines[1] + ending
				}
				for _, count := range []int{128, 512, 1024} {
					input := strings.Repeat(unit, count)
					budget, _ := NewOperationBudget(DefaultLimits())
					r := parseWithProfile(context.Background(), NewSource([]byte(input)), budget, pandocBlocksProfile)
					if r.Failure != nil || r.Interruption != "" || r.Status != Incomplete || r.AST != Partial {
						t.Fatalf("%q/%d: %+v", unit, count, r)
					}
					var headings []*Node
					var walk func(*Node)
					walk = func(n *Node) {
						if n.Kind() == Heading {
							headings = append(headings, n)
						}
						for _, child := range n.Children() {
							walk(child)
						}
					}
					walk(r.Document.Root())
					if len(headings) != count {
						t.Fatalf("%q/%d: %d headings", unit, count, len(headings))
					}
					for i, n := range headings {
						base := i * len(unit)
						assertByteSpan(t, n.Span(), base+len(prefix), base+len(unit), unit[len(prefix):])
						if n.Properties().Attributes != nil || len(n.Children()) != 0 {
							t.Fatal("inline facts published")
						}
						content := n.Content()
						want := 1
						if len(lines) == 2 {
							want = 3
						}
						if len(content) != want {
							t.Fatalf("ranges %q: %+v", unit, content)
						}
						firstEnd := base + len(prefix) + len(lines[0])
						assertByteSpan(t, content[0].Source, base+len(prefix)+2, firstEnd, lines[0][2:])
						if content[0].Semantic != Some(lines[0][2:]) {
							t.Fatal("first pending spelling")
						}
						if len(lines) == 2 {
							assertByteSpan(t, content[1].Source, firstEnd, firstEnd+len(ending), ending)
							next := firstEnd + len(ending) + len(continuation)
							assertByteSpan(t, content[2].Source, next, next+len(lines[1]), lines[1])
							if content[1].Semantic != Some("\n") || content[2].Semantic != Some(lines[1]) {
								t.Fatal("multiline pending spelling")
							}
						}
					}
					if budget.nodes > int64(count)*160+100 || budget.work > int64(len(input))*300+20000 {
						t.Fatalf("%q/%d repeated replay: work%d records%d", unit, count, budget.work, budget.nodes)
					}
				}
			}
		}
	}
}

func TestPandocHeadingFailedOpaqueGroupScaling(t *testing.T) {
	for _, token := range []string{"${", `\foo{`} {
		for _, size := range []int{1024, 2048, 4096, 8192} {
			for _, attrs := range []bool{false, true} {
				pending := strings.Repeat(token, size) + ">"
				input := "# " + pending
				if attrs {
					input += "{#heading}"
				}
				input += "\n"
				budget, _ := NewOperationBudget(DefaultLimits())
				r := parseWithProfile(context.Background(), NewSource([]byte(input)), budget, pandocBlocksProfile)
				if r.Failure != nil || r.Interruption != "" || r.Status != Incomplete || r.AST != Partial {
					t.Fatalf("%q/%d: %+v", token, size, r)
				}
				nodes := r.Document.Root().Children()
				if len(nodes) != 1 || nodes[0].Kind() != Heading || len(nodes[0].Children()) != 0 {
					t.Fatal("fallback ownership")
				}
				node := nodes[0]
				assertByteSpan(t, node.Span(), 0, len(input), input)
				content := node.Content()
				if len(content) != 1 || content[0].Semantic != Some(pending) {
					t.Fatal("pending fallback bytes")
				}
				assertByteSpan(t, content[0].Source, 2, 2+len(pending), pending)
				actual := node.Properties().Attributes
				if !attrs && actual != nil || attrs && (actual == nil || actual.ID != Some("heading")) {
					t.Fatal("genuine suffix lost")
				}
				if budget.work > int64(len(input))*1000+10000 {
					t.Fatalf("%q/%d overlapping failed groups: %d", token, size, budget.work)
				}
			}
		}
	}
}

func TestPandocHeadingConTeXtCompletionGuards(t *testing.T) {
	for _, tt := range []struct {
		name  string
		valid bool
	}{{"foo1 bar", true}, {"foo\tbar", true}, {"É", true}, {"foo٣", false}, {"foo\nbar", false}, {"foo\rbar", false}} {
		text := `\start[` + tt.name + `]x\stop[` + tt.name + `]`
		end := pandocHeadingConTeXtEnd(context.Background(), text, 0)
		if (end == len(text)) != tt.valid {
			t.Fatalf("completion %q end%d", tt.name, end)
		}
	}
	for _, prefix := range []string{"", "> ", "- "} {
		for _, ending := range []string{"\n", "\r\n"} {
			continuation := prefix
			if prefix == "- " {
				continuation = "  "
			}
			lines := []string{`[\start[foo`, `bar]x\stop[foo`, `bar]]{#inline}`}
			first := prefix + "# " + lines[0]
			input := first + ending + continuation + lines[1] + ending + continuation + lines[2] + ending
			r := pandocBlocks(t, input)
			nodes := r.Document.Root().Children()
			if prefix == "> " {
				nodes = nodes[0].Children()
			}
			if prefix == "- " {
				nodes = nodes[0].Children()[0].Children()
			}
			if len(nodes) != 1 || nodes[0].Kind() != Heading || nodes[0].Properties().Attributes != nil || len(nodes[0].Children()) != 0 {
				t.Fatalf("owner %q: %v", input, pandocKinds(r.Document.Root()))
			}
			n := nodes[0]
			assertByteSpan(t, n.Span(), len(prefix), len(input), input[len(prefix):])
			content := n.Content()
			if len(content) != 5 {
				t.Fatalf("ranges %q: %+v", input, content)
			}
			offset := len(prefix) + 2
			for i, line := range lines {
				if i > 0 {
					offset += len(continuation)
				}
				assertByteSpan(t, content[2*i].Source, offset, offset+len(line), line)
				if content[2*i].Semantic != Some(line) {
					t.Fatal("pending completion source")
				}
				offset += len(line)
				if i < 2 {
					assertByteSpan(t, content[2*i+1].Source, offset, offset+len(ending), ending)
					if content[2*i+1].Semantic != Some("\n") {
						t.Fatal("atomic ending")
					}
					offset += len(ending)
				}
			}
		}
	}
}

func TestPandocHeadingConTeXtFailureScaling(t *testing.T) {
	for _, token := range []string{`\startfoo`, `\start[foo]`} {
		for _, size := range []int{1024, 4096} {
			for _, suffix := range []string{">", ">{#heading}"} {
				pending := strings.Repeat(token, size) + ">"
				input := "# " + strings.Repeat(token, size) + suffix + "\n"
				budget, _ := NewOperationBudget(DefaultLimits())
				r := parseWithProfile(context.Background(), NewSource([]byte(input)), budget, pandocBlocksProfile)
				if r.Failure != nil || r.Interruption != "" || r.Status != Incomplete || r.AST != Partial {
					t.Fatalf("%q/%d %+v", token, size, r)
				}
				nodes := r.Document.Root().Children()
				if len(nodes) != 1 || nodes[0].Kind() != Heading {
					t.Fatal("fallback heading")
				}
				n := nodes[0]
				assertByteSpan(t, n.Span(), 0, len(input), input)
				parts := n.Content()
				if len(parts) != 1 || parts[0].Semantic != Some(pending) {
					t.Fatal("pending fallback")
				}
				assertByteSpan(t, parts[0].Source, 2, 2+len(pending), pending)
				a := n.Properties().Attributes
				if suffix == ">" && a != nil || suffix != ">" && (a == nil || a.ID != Some("heading")) {
					t.Fatal("later genuine group")
				}
				if budget.work > int64(len(input))*1000+10000 {
					t.Fatalf("repeated guard work%d bytes%d", budget.work, len(input))
				}
			}
		}
	}
}

func TestPandocHeadingConTeXtEndpointIndex(t *testing.T) {
	opens := []struct{ raw, name string }{{`\starta`, "a"}, {`\startab`, "ab"}, {`\start[a1]`, "[a1]"}}
	var slow func(string, int) int
	slow = func(text string, start int) int {
		name := ""
		after := start
		// Complete literal openers have a nonletter sentinel, independent of the
		// indexed production completion parser and its reverse successor algorithm.
		for _, o := range opens {
			if strings.HasPrefix(text[start:], o.raw+"!") {
				name = o.name
				after = start + len(o.raw)
				break
			}
		}
		if after == start {
			return start
		}
		for i := after; i < len(text); {
			closer := `\stop` + name
			if strings.HasPrefix(text[i:], closer) {
				return i + len(closer)
			}
			if nested := slow(text, i); nested > i {
				i = nested
			} else {
				i++
			}
		}
		return start
	}
	tokens := []string{`\starta!`, `\startab!`, `\start[a1]!`, `\stopa!`, `\stopab!`, `\stop[a1]!`, "x!"}
	var check func(string, int)
	check = func(text string, remaining int) {
		b := pandocHeadingBoundaries{ctx: context.Background(), text: text}
		for at := 0; at < len(text); at++ {
			if text[at] == '\\' {
				if got, want := b.contextEnd(at), slow(text, at); got != want {
					t.Fatalf("%q at%d end%d want%d", text, at, got, want)
				}
			}
		}
		if remaining > 0 {
			for _, token := range tokens {
				check(text+token, remaining-1)
			}
		}
	}
	check("", 4)
}
