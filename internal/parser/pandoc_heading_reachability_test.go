package parser

import (
	"context"
	"strings"
	"testing"
)

func TestPandocHeadingReachableOpaqueCosts(t *testing.T) {
	for _, unit := range []string{`\$a`, `\!a`, `\<i k=`, `\\emph`, `\!abcd`} {
		for _, n := range []int{1024, 2048, 4096, 8192, 16384} {
			if n > 4096 && unit != `\$a` && unit != `\!a` {
				continue
			}
			payload := "[" + strings.Repeat(unit, n) + "]{#inline}"
			input := "# " + payload + "\n"
			budget, _ := NewOperationBudget(DefaultLimits())
			r := parseWithProfile(context.Background(), NewSource([]byte(input)), budget, pandocBlocksProfile)
			if r.Failure != nil || r.Interruption != "" {
				t.Fatalf("unit%q n%d %+v", unit, n, r)
			}
			nodes := r.Document.Root().Children()
			if len(nodes) != 1 || nodes[0].Kind() != Heading || nodes[0].Properties().Attributes != nil {
				t.Fatal("escaped label owner")
			}
			assertByteSpan(t, nodes[0].Span(), 0, len(input), input)
			parts := nodes[0].Content()
			if len(parts) != 1 || parts[0].Semantic != Some(payload) {
				t.Fatal("full pending label")
			}
			assertByteSpan(t, parts[0].Source, 2, len(input)-1, payload)
			if budget.work > int64(len(input))*100+10000 || budget.nodes > 30 {
				t.Fatalf("unreachable suffix work unit%q n%d work%d records%d", unit, n, budget.work, budget.nodes)
			}
		}
	}
}

func TestPandocHeadingEscapedPendingOwnership(t *testing.T) {
	for _, prefix := range []string{"", "> ", "- "} {
		for _, eol := range []string{"\n", "\r\n"} {
			for _, owner := range []string{"html", "math"} {
				for _, mode := range []string{"escaped-future", "escaped-no-future", "actual-owner"} {
					continued := prefix
					if prefix == "- " {
						continued = "  "
					}
					opener, tail := "<i k=", "next>]{#inline}"
					if owner == "math" {
						opener, tail = "$a ", "next$]{#inline}"
					}
					if mode != "actual-owner" {
						opener = "\\" + opener
					}
					if mode == "escaped-no-future" {
						tail = "ordinary"
					}
					first := prefix + "# [" + opener + "{#wrong}"
					input := first + eol + continued + tail + eol
					r := pandocBlocks(t, input)
					nodes := r.Document.Root().Children()
					if prefix == "> " {
						nodes = nodes[0].Children()
					}
					if prefix == "- " {
						nodes = nodes[0].Children()[0].Children()
					}
					if mode == "actual-owner" {
						if len(nodes) != 1 || nodes[0].Kind() != Heading || nodes[0].Properties().Attributes != nil {
							t.Fatal("real unescaped owner")
						}
						assertByteSpan(t, nodes[0].Span(), len(prefix), len(input), input[len(prefix):])
						parts := nodes[0].Content()
						if len(parts) != 3 {
							t.Fatal("real original contributions")
						}
						assertByteSpan(t, parts[0].Source, len(prefix)+2, len(first), "["+opener+"{#wrong}")
						assertByteSpan(t, parts[1].Source, len(first), len(first)+len(eol), eol)
						assertByteSpan(t, parts[2].Source, len(first)+len(eol)+len(continued), len(input)-len(eol), tail)
						if parts[0].Semantic != Some("["+opener+"{#wrong}") || parts[1].Semantic != Some("\n") || parts[2].Semantic != Some(tail) {
							t.Fatal("real pending spelling")
						}
						continue
					}
					if len(nodes) != 2 || nodes[0].Kind() != Heading || nodes[1].Kind() != Paragraph {
						t.Fatalf("separate paragraph %q", input)
					}
					h, p := nodes[0], nodes[1]
					assertByteSpan(t, h.Span(), len(prefix), len(first)+len(eol), input[len(prefix):len(first)+len(eol)])
					a := h.Properties().Attributes
					if a == nil || a.ID != Some("wrong") {
						t.Fatal("genuine first-line ID")
					}
					assertValueRange(t, a.Raw, len(prefix)+3+len(opener), len(first), "{#wrong}", "{#wrong}")
					pending := "[" + strings.TrimRight(opener, " \t")
					parts := h.Content()
					if len(parts) != 1 || parts[0].Semantic != Some(pending) {
						t.Fatal("first-line pending")
					}
					assertByteSpan(t, parts[0].Source, len(prefix)+2, len(prefix)+2+len(pending), pending)
					from := len(first) + len(eol) + len(continued)
					assertByteSpan(t, p.Span(), from, len(input), input[from:])
					parts = p.Content()
					if p.Properties().Attributes != nil || len(parts) != 1 || parts[0].Semantic != Some(tail) {
						t.Fatal("retained paragraph")
					}
					assertByteSpan(t, parts[0].Source, from, len(input)-len(eol), tail)
				}
			}
		}
	}
}

func TestPandocHeadingFailedLabelOuterOpaqueFallback(t *testing.T) {
	for _, pair := range [][2]string{{"<i k=", "next>"}, {"$a ", "next$"}} {
		for _, escaped := range []bool{false, true} {
			opener := pair[0]
			if escaped {
				opener = "\\" + opener
			}
			first := "# [" + opener + "{#wrong}"
			input := first + "\n" + pair[1] + "\n"
			r := pandocBlocks(t, input)
			nodes := r.Document.Root().Children()
			if escaped {
				if len(nodes) != 2 || nodes[0].Span().End() != len(first)+1 || nodes[0].Properties().Attributes == nil || nodes[0].Properties().Attributes.ID != Some("wrong") || nodes[1].Kind() != Paragraph {
					t.Fatal("escaped failed-label fallback")
				}
			} else {
				if len(nodes) != 1 || nodes[0].Kind() != Heading || nodes[0].Properties().Attributes != nil {
					t.Fatal("outer reachable opaque must still own newline after label failure")
				}
				assertByteSpan(t, nodes[0].Span(), 0, len(input), input)
			}
		}
	}
}

func TestPandocHeadingLabelCodeAttributeContinuation(t *testing.T) {
	for _, pair := range [][2]string{{"[`a`{k=\"x", "y\"}]{#inline}"}, {"[`a`{#one", ".two}]{#inline}"}, {"[`a`{#one", "- .two}]{#inline}"}} {
		for _, prefix := range []string{"", "> ", "- "} {
			for _, eol := range []string{"\n", "\r\n"} {
				for _, separate := range []bool{false, true} {
					continued := prefix
					if prefix == "- " {
						continued = "  "
					}
					suffix := ""
					if separate {
						suffix = " {#heading}"
					}
					first := prefix + "# " + pair[0]
					second := continued + pair[1]
					input := first + eol + second + suffix + eol
					r := pandocBlocks(t, input)
					nodes := r.Document.Root().Children()
					if prefix == "> " {
						nodes = nodes[0].Children()
					}
					if prefix == "- " {
						nodes = nodes[0].Children()[0].Children()
					}
					if len(nodes) != 1 || nodes[0].Kind() != Heading {
						t.Fatal("complete code attribute owner")
					}
					n := nodes[0]
					assertByteSpan(t, n.Span(), len(prefix), len(input), input[len(prefix):])
					a := n.Properties().Attributes
					if !separate && a != nil || separate && (a == nil || a.ID != Some("heading")) {
						t.Fatal("code attrs stole outer group")
					}
					parts := n.Content()
					if len(parts) != 3 {
						t.Fatal("code attr contributions")
					}
					assertByteSpan(t, parts[0].Source, len(prefix)+2, len(first), pair[0])
					assertByteSpan(t, parts[1].Source, len(first), len(first)+len(eol), eol)
					assertByteSpan(t, parts[2].Source, len(first)+len(eol)+len(continued), len(first)+len(eol)+len(continued)+len(pair[1]), pair[1])
					if parts[0].Semantic != Some(pair[0]) || parts[1].Semantic != Some("\n") || parts[2].Semantic != Some(pair[1]) {
						t.Fatal("code attr pending")
					}
				}
			}
		}
	}
	for _, input := range []string{"# [`a`{k= {#heading}\nordinary\n", "# [`a`{#one {#heading}\nordinary\n"} {
		r := pandocBlocks(t, input)
		nodes := r.Document.Root().Children()
		if len(nodes) != 2 || nodes[0].Span().End() != strings.IndexByte(input, '\n')+1 || nodes[1].Kind() != Paragraph {
			t.Fatal("malformed optional attrs must retain fallback")
		}
	}
}

func TestPandocHeadingEscapedOpaqueSetextAndProfiles(t *testing.T) {
	for _, prefix := range []string{"", "> ", "- "} {
		for _, eol := range []string{"\n", "\r\n"} {
			continued := prefix
			if prefix == "- " {
				continued = "  "
			}
			for _, unit := range []string{`\$a`, `\<i k=`, `\\emph`} {
				for _, atx := range []bool{false, true} {
					for _, separate := range []bool{false, true} {
						payload := "[" + strings.Repeat(unit, 128) + "]{#inline}"
						leader := ""
						if atx {
							leader = "# "
						}
						suffix := ""
						if separate {
							suffix = " {#heading}"
						}
						input := prefix + leader + payload + suffix + eol
						if !atx {
							input += continued + "---" + eol
						}
						r := pandocBlocks(t, input)
						nodes := r.Document.Root().Children()
						if prefix == "> " {
							nodes = nodes[0].Children()
						}
						if prefix == "- " {
							nodes = nodes[0].Children()[0].Children()
						}
						if len(nodes) != 1 || nodes[0].Kind() != Heading {
							t.Fatal("profile escaped owner")
						}
						n := nodes[0]
						assertByteSpan(t, n.Span(), len(prefix), len(input), input[len(prefix):])
						a := n.Properties().Attributes
						if !separate && a != nil || separate && (a == nil || a.ID != Some("heading")) {
							t.Fatal("profile genuine group")
						}
						parts := n.Content()
						if len(parts) != 1 || parts[0].Semantic != Some(payload) {
							t.Fatal("profile source")
						}
						from := len(prefix) + len(leader)
						assertByteSpan(t, parts[0].Source, from, from+len(payload), payload)
					}
				}
			}
			for _, pair := range [][2]string{{"<i k='", "next'>]{#inline}"}, {"$a ", "next$]{#inline}"}, {"`a`{k=\"x", "y\"}]{#inline}"}, {"`a`{#one", ".two}]{#inline}"}} {
				for _, escaped := range []bool{false, true} {
					for _, final := range []bool{false, true} {
						if escaped && strings.HasPrefix(pair[0], "`a`") {
							continue
						}
						opener := pair[0]
						if escaped {
							opener = "\\" + opener
						}
						first := prefix + "[" + opener + "{#wrong}"
						tail := pair[1]
						// Code-attribute controls carry the brace group in their literal template.
						if strings.HasPrefix(pair[0], "`a`") {
							first = prefix + "[" + opener
						}
						middle := continued + "---"
						third := continued + tail
						input := first + eol + middle + eol + third + eol
						if final {
							input += continued + "---" + eol
						}
						r := pandocBlocks(t, input)
						nodes := r.Document.Root().Children()
						if prefix == "> " {
							nodes = nodes[0].Children()
						}
						if prefix == "- " {
							nodes = nodes[0].Children()[0].Children()
						}
						if escaped {
							if len(nodes) != 2 || nodes[0].Kind() != Heading || nodes[0].Properties().Attributes == nil || nodes[0].Properties().Attributes.ID != Some("wrong") {
								t.Fatal("escaped Setext original owner")
							}
							firstEnd := len(first) + len(eol) + len(middle) + len(eol)
							assertByteSpan(t, nodes[0].Span(), len(prefix), firstEnd, input[len(prefix):firstEnd])
							if final && nodes[1].Kind() != Heading || !final && nodes[1].Kind() != Paragraph {
								t.Fatal("escaped Setext later block")
							}
						} else {
							kind := Paragraph
							if final {
								kind = Heading
							}
							if len(nodes) != 1 || nodes[0].Kind() != kind || nodes[0].Properties().Attributes != nil {
								t.Fatalf("real Setext opaque/code-attribute owner %q: %v", input, pandocKinds(r.Document.Root()))
							}
							assertByteSpan(t, nodes[0].Span(), len(prefix), len(input), input[len(prefix):])
							if final {
								marker := len(first) + len(eol) + len(middle) + len(eol) + len(third) + len(eol) + len(continued)
								assertByteSpan(t, nodes[0].Markers()[0].Source, marker, marker+3, "---")
							}
						}
					}
				}
			}
		}
	}
}

func TestPandocHeadingCodeAttributePreferredAlternative(t *testing.T) {
	for _, input := range []string{
		"# [`a`{k=\"x}]{#inline}\ny\"}]{#inline}\n",
		"# [`a`{k=\"x}]{#inline}\ny\"}]{#inline} {#heading}\n",
	} {
		r := pandocBlocks(t, input)
		nodes := r.Document.Root().Children()
		if len(nodes) != 1 || nodes[0].Kind() != Heading {
			t.Fatal("preferred quoted code attr whole owner")
		}
		n := nodes[0]
		assertByteSpan(t, n.Span(), 0, len(input), input)
		a := n.Properties().Attributes
		separate := strings.Contains(input, " {#heading}")
		if !separate && a != nil || separate && (a == nil || a.ID != Some("heading")) {
			t.Fatal("preferred code attr group ownership")
		}
		var pending strings.Builder
		for _, part := range n.Content() {
			pending.WriteString(part.Semantic.Value)
		}
		want := strings.TrimSuffix(strings.TrimSuffix(input[2:], "\n"), " {#heading}")
		if pending.String() != want {
			t.Fatal("preferred full pending")
		}
	}
	input := "# [`a`{k=\"x}]{#inline}\ny\"} {#heading}\n"
	r := pandocBlocks(t, input)
	nodes := r.Document.Root().Children()
	if len(nodes) != 1 || nodes[0].Kind() != Heading || nodes[0].Span().End() != len(input) || nodes[0].Properties().Attributes == nil || nodes[0].Properties().Attributes.ID != Some("heading") {
		t.Fatal("preferred code attribute success with failed outer label")
	}

	// Without a later closing quote, the selected quoted alternative fails and
	// the genuine first-line unquoted fallback remains a complete span owner.
	input = "# [`a`{k=\"x}]{#inline}\nordinary\n"
	r = pandocBlocks(t, input)
	nodes = r.Document.Root().Children()
	if len(nodes) != 2 || nodes[0].Kind() != Heading || nodes[0].Properties().Attributes != nil || nodes[0].Span().End() != strings.IndexByte(input, '\n')+1 || nodes[1].Kind() != Paragraph {
		t.Fatal("missing preferred closer fallback")
	}
}
