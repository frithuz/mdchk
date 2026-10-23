package parser

import (
	"context"
	"strings"
	"testing"
)

func TestPandocHeadingSetextCompleteOwners(t *testing.T) {
	for _, pair := range [][2]string{
		{"[x](url {#wrong}", "more){#inline}"},
		{"![x](url {#wrong}", "more){#inline}"},
		{`[x](url "title {#wrong}`, `more"){#inline}`},
		{"[x](<url {#wrong}", "more>){#inline}"},
		{`[x](url\) {#wrong}`, "more){#inline}"},
		{"[x](url (nested {#wrong}", "more)){#inline}"},
		{"[x](data:text/plain;k={#wrong}", "more;base64,YQ==){#inline}"},
		{"[`x {#wrong}", "y`]{#inline}"},
		{"[$x {#wrong}", "y$]{#inline}"},
		{"[<i title='{#wrong}", "x'>y</i>]{#inline}"},
		{`[\foo[{#wrong}`, `opt]]{#inline}`},
	} {
		for _, prefix := range []string{"", "> ", "- "} {
			for _, eol := range []string{"\n", "\r\n"} {
				for _, separate := range []bool{false, true} {
					for _, final := range []bool{false, true} {
						continued := prefix
						if prefix == "- " {
							continued = "  "
						}
						first := prefix + pair[0]
						middle := continued + "---"
						third := continued + pair[1]
						if separate {
							third += " {#heading}"
						}
						input := first + eol + middle + eol + third + eol
						markerStart := len(input) + len(continued)
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
						kind := Paragraph
						if final {
							kind = Heading
						}
						if len(nodes) != 1 || nodes[0].Kind() != kind {
							t.Fatalf("tree %q: %v", input, pandocKinds(r.Document.Root()))
						}
						node := nodes[0]
						assertByteSpan(t, node.Span(), len(prefix), len(input), input[len(prefix):])
						a := node.Properties().Attributes
						if !final && a != nil || final && !separate && a != nil || final && separate && (a == nil || a.ID != Some("heading")) {
							t.Fatalf("owner %q %+v", input, a)
						}
						parts := node.Content()
						if final {
							h := node.Properties().Heading
							if h == nil || h.Level != 2 || h.Style != "setext" {
								t.Fatal("style")
							}
							markers := node.Markers()
							if len(markers) != 1 || markers[0].Role != "underline" {
								t.Fatal("actual marker")
							}
							assertByteSpan(t, markers[0].Source, markerStart, markerStart+3, "---")
							if len(parts) != 5 {
								t.Fatalf("five contributions %q %+v", input, parts)
							}
							offsets := [][2]int{{len(prefix), len(first)}, {len(first), len(first) + len(eol)}, {len(first) + len(eol) + len(continued), len(first) + len(eol) + len(middle)}, {len(first) + len(eol) + len(middle), len(first) + len(eol) + len(middle) + len(eol)}, {len(first) + len(eol) + len(middle) + len(eol) + len(continued), len(first) + len(eol) + len(middle) + len(eol) + len(continued) + len(pair[1])}}
							wants := []string{pair[0], "\n", "---", "\n", pair[1]}
							for i, pos := range offsets {
								assertByteSpan(t, parts[i].Source, pos[0], pos[1], input[pos[0]:pos[1]])
								if parts[i].Semantic != Some(wants[i]) {
									t.Fatal("pending semantic")
								}
							}
						} else {
							// Ordinary paragraph building retains each complete original physical
							// contribution, including CRLF; no speculative heading trimming/ID escapes.
							if len(parts) != 3 {
								t.Fatalf("paragraph contributions %q %+v", input, parts)
							}
							pos := len(prefix)
							for i, raw := range []string{pair[0] + eol, "---" + eol, strings.TrimPrefix(third, continued)} {
								assertByteSpan(t, parts[i].Source, pos, pos+len(raw), raw)
								if parts[i].Semantic != Some(raw) {
									t.Fatal("paragraph source")
								}
								pos += len(raw) + len(continued)
							}
						}
					}
				}
			}
		}
	}
	for _, input := range []string{
		"[x](url {#wrong}\n---\nmore{#inline}\n---\n",
		"[x](url {#wrong}\n---\n\nmore){#inline}\n---\n",
		"[`x {#wrong}\n---\n\ny`]{#inline}\n---\n",
	} {
		r := pandocBlocks(t, input)
		nodes := r.Document.Root().Children()
		if nodes[0].Kind() != Heading || nodes[0].Span().End() != strings.Index(input, "---\n")+4 || nodes[0].Properties().Attributes.ID != Some("wrong") {
			t.Fatalf("failed owner must keep first real underline %q", input)
		}
	}
}

func TestPandocHeadingSharedContinuationScaling(t *testing.T) {
	for _, prefix := range []string{"", "> ", "- "} {
		for _, eol := range []string{"\n", "\r\n"} {
			for _, count := range []int{16, 32, 64, 128, 256, 512} {
				for _, closer := range []bool{false, true} {
					continued := prefix
					if prefix == "- " {
						continued = "  "
					}
					unit := continued + "# [x](" + eol
					input := prefix + "# [x](" + eol + strings.Repeat(unit, count-1)
					if closer {
						input += continued + "){#inline} {#heading}" + eol
					}
					budget, _ := NewOperationBudget(DefaultLimits())
					r := parseWithProfile(context.Background(), NewSource([]byte(input)), budget, pandocBlocksProfile)
					if r.Failure != nil || r.Interruption != "" {
						t.Fatalf("prefix%q eol%q n%d: %+v", prefix, eol, count, r)
					}
					nodes := r.Document.Root().Children()
					if prefix == "> " {
						nodes = nodes[0].Children()
					}
					if prefix == "- " {
						nodes = nodes[0].Children()[0].Children()
					}
					if len(nodes) != count {
						t.Fatalf("headings%d want%d", len(nodes), count)
					}
					for i, node := range nodes {
						from := i*len(unit) + len(continued)
						if i == 0 {
							from = len(prefix)
						}
						end := (i + 1) * len(unit)
						if i == count-1 && closer {
							end = len(input)
						}
						if node.Kind() != Heading {
							t.Fatal("kind")
						}
						assertByteSpan(t, node.Span(), from, end, input[from:end])
						attrs := node.Properties().Attributes
						if i == count-1 && closer {
							if attrs == nil || attrs.ID != Some("heading") {
								t.Fatal("last genuine group")
							}
						} else if attrs != nil {
							t.Fatal("false attrs")
						}
						parts := node.Content()
						if i == count-1 && closer {
							if len(parts) != 3 || parts[0].Semantic != Some("[x](") || parts[1].Semantic != Some("\n") || parts[2].Semantic != Some("){#inline}") {
								t.Fatal("last pending")
							}
							assertByteSpan(t, parts[0].Source, from+2, from+6, "[x](")
							assertByteSpan(t, parts[1].Source, from+6, from+6+len(eol), eol)
							lastStart := count*len(unit) + len(continued)
							assertByteSpan(t, parts[2].Source, lastStart, lastStart+10, "){#inline}")
						} else {
							if len(parts) != 1 || parts[0].Semantic != Some("[x](") {
								t.Fatal("failed pending")
							}
							assertByteSpan(t, parts[0].Source, from+2, from+6, "[x](")
						}
					}
					if budget.work > int64(len(input))*200+10000 || budget.nodes > int64(count)*100+1000 {
						t.Fatalf("shared replay work%d nodes%d n%d", budget.work, budget.nodes, count)
					}
				}
			}
		}
	}
}

func TestPandocHeadingReplayCodeAdmission(t *testing.T) {
	for _, input := range []string{"- # `a {#heading}\n  - b`{#inline}\n", "# `a {#heading}\n\nb`{#inline}\n"} {
		r := pandocBlocks(t, input)
		nodes := r.Document.Root().Children()
		if strings.HasPrefix(input, "- ") {
			nodes = nodes[0].Children()[0].Children()
		}
		if nodes[0].Kind() != Heading || nodes[0].Properties().Attributes == nil || nodes[0].Properties().Attributes.ID != Some("heading") {
			t.Fatalf("code continuation admission %q", input)
		}
		want := strings.IndexByte(input, '\n') + 1
		if nodes[0].Span().End() != want {
			t.Fatal("code crossed reader barrier")
		}
	}
}

func TestPandocHeadingSharedSuccessfulOwnerChain(t *testing.T) {
	for _, count := range []int{128, 256, 512} {
		for _, prefix := range []string{"", "> ", "- "} {
			for _, eol := range []string{"\n", "\r\n"} {
				for _, separate := range []bool{false, true} {
					pending := strings.TrimSuffix(strings.Repeat("[x](u\nv){#inline} ", count), " ")
					lines := strings.Split(pending, "\n")
					continued := prefix
					if prefix == "- " {
						continued = "  "
					}
					var input strings.Builder
					type sourceFact struct {
						from, to      int
						raw, semantic string
					}
					var facts []sourceFact
					for i, text := range lines {
						if i == 0 {
							input.WriteString(prefix + "# ")
						} else {
							input.WriteString(continued)
						}
						at := input.Len()
						input.WriteString(text)
						facts = append(facts, sourceFact{at, input.Len(), text, text})
						if i == len(lines)-1 && separate {
							input.WriteString(" {#heading}")
						}
						at = input.Len()
						input.WriteString(eol)
						if i < len(lines)-1 {
							facts = append(facts, sourceFact{at, input.Len(), eol, "\n"})
						}
					}
					original := input.String()
					budget, _ := NewOperationBudget(DefaultLimits())
					r := parseWithProfile(context.Background(), NewSource([]byte(original)), budget, pandocBlocksProfile)
					if r.Failure != nil || r.Interruption != "" {
						t.Fatalf("n%d: %+v", count, r)
					}
					nodes := r.Document.Root().Children()
					if prefix == "> " {
						nodes = nodes[0].Children()
					}
					if prefix == "- " {
						nodes = nodes[0].Children()[0].Children()
					}
					if len(nodes) != 1 || nodes[0].Kind() != Heading {
						t.Fatal("chain tree")
					}
					node := nodes[0]
					assertByteSpan(t, node.Span(), len(prefix), len(original), original[len(prefix):])
					a := node.Properties().Attributes
					if !separate && a != nil || separate && (a == nil || a.ID != Some("heading")) {
						t.Fatal("chain attribute owner")
					}
					parts := node.Content()
					if len(parts) != len(facts) {
						t.Fatal("chain contribution count")
					}
					for i, f := range facts {
						assertByteSpan(t, parts[i].Source, f.from, f.to, f.raw)
						if parts[i].Semantic != Some(f.semantic) {
							t.Fatal("chain pending")
						}
					}
					if budget.work > int64(len(original))*200+10000 || budget.nodes > int64(count)*80+1000 {
						t.Fatalf("repeated success-prefix copies n%d work%d nodes%d", count, budget.work, budget.nodes)
					}
				}
			}
		}
	}
}

func TestPandocHeadingSharedReplayOwnerContexts(t *testing.T) {
	// A source's raw angle spelling is already consumed by the link. Its HTML-
	// shaped bytes cannot shield a later label from that label's own raw prescan.
	input := "# [x](u\nv){#inline} [a](<i title='>){#inline} [b]{#inline}\nz'>\n"
	r := pandocBlocks(t, input)
	nodes := r.Document.Root().Children()
	end := strings.Index(input, "\nz'>") + 1
	if len(nodes) != 2 || nodes[0].Kind() != Heading || nodes[0].Span().End() != end || nodes[0].Properties().Attributes != nil || nodes[1].Kind() != Paragraph {
		t.Fatal("independent label start after consumed angle source")
	}
	for _, opening := range []string{"`a", "[`a"} {
		input = "- # [x](u\n  v){#inline} " + opening + " {#heading}\n  - b`]{#inline}\n"
		r = pandocBlocks(t, input)
		nodes = r.Document.Root().Children()[0].Children()[0].Children()
		firstEnd := strings.Index(input, "\n  - b") + 1
		if nodes[0].Kind() != Heading || nodes[0].Span().End() != firstEnd || nodes[0].Properties().Attributes == nil || nodes[0].Properties().Attributes.ID != Some("heading") {
			t.Fatalf("later code/list-start admission %q", input)
		}
	}
}

func TestPandocHeadingSetextActualMarkerAndAttributeOwner(t *testing.T) {
	input := "[x](u\n---\nv){#inline} {#heading}\n\n===\nfollowing\n"
	r := pandocBlocks(t, input)
	nodes := r.Document.Root().Children()
	if len(nodes) != 2 || nodes[0].Kind() != Heading || nodes[0].Properties().Heading.Level != 1 || nodes[0].Properties().Attributes.ID != Some("heading") || nodes[1].Kind() != Paragraph {
		t.Fatal("actual underline level after consumed eligibility")
	}
	at := strings.Index(input, "===")
	assertByteSpan(t, nodes[0].Span(), 0, at+4, input[:at+4])
	assertByteSpan(t, nodes[0].Markers()[0].Source, at, at+3, "===")
	for _, final := range []bool{false, true} {
		input = "H {k=\"v\n---\nx\" #heading}\n"
		if final {
			input += "---\n"
		}
		r = pandocBlocks(t, input)
		nodes = r.Document.Root().Children()
		kind := Paragraph
		if final {
			kind = Heading
		}
		if len(nodes) != 1 || nodes[0].Kind() != kind || nodes[0].Span().End() != len(input) {
			t.Fatal("whole attribute-owned underline try")
		}
		if final {
			a := nodes[0].Properties().Attributes
			if a == nil || a.ID != Some("heading") || len(a.KeyValues) != 1 || a.KeyValues[0].Value != "v --- x" {
				t.Fatalf("complete attribute owner %+v", a)
			}
		} else if nodes[0].Properties().Attributes != nil {
			t.Fatal("provisional heading attr escaped rollback")
		}
	}
}
