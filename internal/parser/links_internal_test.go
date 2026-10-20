package parser

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type linkWorkContext struct {
	context.Context
	checks int
}

func (c *linkWorkContext) Err() error {
	c.checks++
	return nil
}

func TestInlineLinkFailedDestinationWork(t *testing.T) {
	previous := 0
	for _, repetitions := range []int{1000, 2000, 4000, 8000} {
		input := strings.Repeat("[x](", repetitions)
		ctx := &linkWorkContext{Context: context.Background()}
		r := Parse(ctx, NewSource([]byte(input)))
		if r.Status != Complete || r.Failure != nil || r.Interruption != "" || r.Phases.Inlines != PhaseComplete {
			t.Fatal("malformed candidates lost literal completion", r)
		}
		children := r.Document.Root().Children()[0].Children()
		if len(children) != 1 || children[0].Kind() != Text || children[0].Properties().Literal != Some(input) || children[0].Span().Start() != 0 || children[0].Span().End() != len(input) || string(r.Source.Bytes()) != input {
			t.Fatal("malformed candidate output/source changed")
		}
		t.Logf("repetitions=%d bytes=%d checks=%d", repetitions, len(input), ctx.checks)
		// This bounds cumulative executed work as input/output double, not elapsed
		// time or a machine-specific resource budget. Quadratic rescanning grows
		// fourfold and fails; fixed setup and linear/logarithmic lookup fit below 3x.
		if previous != 0 && ctx.checks > previous*3 {
			t.Errorf("overlapping failed destinations rescan suffixes: %d -> %d checks", previous, ctx.checks)
		}
		previous = ctx.checks
	}
}

func BenchmarkInlineLinkFailedDestinations(b *testing.B) {
	for _, repetitions := range []int{1000, 2000, 4000, 8000} {
		b.Run(strconv.Itoa(repetitions), func(b *testing.B) {
			input := strings.Repeat("[x](", repetitions)
			source := NewSource([]byte(input))
			b.ReportAllocs()
			b.SetBytes(int64(len(input)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if r := Parse(context.Background(), source); r.Status != Complete {
					b.Fatal("malformed candidates did not remain literal", r)
				}
			}
		})
	}
}

func TestInlineLinkFailedDestinationRecovery(t *testing.T) {
	destination := `a\(b\)` + strings.Repeat("(", 64) + "end" + strings.Repeat(")", 64)
	for _, unit := range []string{"[x](", `[x](\(`} {
		for _, separator := range []string{"", " "} {
			previous := 0
			for _, repetitions := range []int{1000, 2000, 4000, 8000} {
				prefix := strings.Repeat(unit, repetitions) + separator
				input := prefix + "[good](" + destination + ")"
				ctx := &linkWorkContext{Context: context.Background()}
				r := Parse(ctx, NewSource([]byte(input)))
				if r.Status != Complete || r.Failure != nil || r.Interruption != "" {
					t.Fatal("failed lookahead lost later valid link", r)
				}
				children := r.Document.Root().Children()[0].Children()
				if len(children) != 2 || children[0].Kind() != Text || children[1].Kind() != Link {
					t.Fatal("malformed prefix/valid suffix shape", children)
				}
				link := children[1]
				properties := link.Properties().Link
				if children[0].Properties().Literal != Some(strings.ReplaceAll(prefix, `\(`, "(")) || children[0].Span().Start() != 0 || children[0].Span().End() != len(prefix) || link.Span().Start() != len(prefix) || link.Span().End() != len(input) || string(r.Source.Bytes()) != input {
					t.Fatal("recovery lost literal/source bytes")
				}
				if properties.Origin != "inline" || properties.Resolution != Available || properties.Label.Semantic != Some("good") || properties.Destination.Semantic != Some("a(b)"+strings.Repeat("(", 64)+"end"+strings.Repeat(")", 64)) {
					t.Fatal("escaped/deep destination semantics", properties)
				}
				if len(link.Children()) != 1 || link.Children()[0].Properties().Literal != Some("good") || link.Children()[0].Span().Start() != len(prefix)+1 || link.Children()[0].Span().End() != len(prefix)+5 {
					t.Fatal("recovered label source", link.Children())
				}
				var raw strings.Builder
				for _, part := range properties.Destination.Segments {
					text, err := r.Source.Text(part.Source)
					if err != nil {
						t.Fatal(err)
					}
					raw.WriteString(text)
				}
				if raw.String() != destination || properties.Destination.Segments[0].Source.Start() != len(prefix)+7 || properties.Destination.Segments[len(properties.Destination.Segments)-1].Source.End() != len(input)-1 {
					t.Fatal("recovered original destination", raw.String())
				}
				if previous != 0 && ctx.checks > previous*3 {
					t.Fatal("recovery regressed to cumulative rescanning", previous, ctx.checks)
				}
				previous = ctx.checks
			}
		}
	}
}

func TestInlineLinkDestinationIndexInterruption(t *testing.T) {
	input := "](a\\(b\\)(c(d(e))))"
	ctx := &checkpointContext{Context: context.Background(), remaining: 5}
	s := inlineScanner{ctx: ctx, text: input}
	if _, valid := s.inlineLink(0); valid || interruption(ctx) == "" || s.bareLinkEnds != nil {
		t.Fatal("unfinished destination index was retained", s.bareLinkEnds)
	}
	s.ctx = context.Background()
	if syntax, valid := s.inlineLink(0); !valid || syntax.end != len(input) || s.bareLinkEnds == nil {
		t.Fatal("retry did not build the complete index", syntax, valid)
	}
}

func TestInlineLinkInterruptionPreservesPublication(t *testing.T) {
	for _, input := range []string{
		"prefix *[a **b**](<" + strings.Repeat("é", 40) + "> \"title\") [*c*](d)*",
		"> - [é\x00\r\n>   **β**](uri\r\n>   \"title\r\n>   tail\") after",
		"[bad](" + strings.Repeat("(", 40) + "missing) [good](url)",
	} {
		r := Parse(context.Background(), NewSource([]byte(input)))
		if r.Status != Complete {
			t.Fatal("incomplete fixture", r)
		}
		if _, err := parseBlocks(context.Background(), r.Document); err != nil {
			t.Fatal(err)
		}
		before := r.Document.Root()
		for _, deadline := range []bool{false, true} {
			completed, observed := false, 0
			for checkpoint := 1; checkpoint < 5000; checkpoint++ {
				ctx := &quoteCheckpoint{checkpointContext: checkpointContext{Context: context.Background(), remaining: checkpoint}, deadline: deadline}
				state, err := parseInlines(ctx, r.Document, PhaseComplete)
				if err != nil {
					t.Fatal("interruption became model failure", err)
				}
				if state == PhaseComplete {
					completed = true
					break
				}
				observed++
				want := Cancelled
				if deadline {
					want = DeadlineExceeded
				}
				if state != Interrupted || interruption(ctx) != want || !reflect.DeepEqual(before, r.Document.Root()) {
					t.Fatal("unfinished link tree was published", state, ctx)
				}
				bad := Parse(ctx, NewSource([]byte("[é\xff](b)")))
				if bad.Failure == nil || bad.Failure.Code != InvalidUTF8 || bad.Failure.Location.Start() != 3 || bad.Interruption != "" {
					t.Fatal("encoding lost precedence", bad)
				}
			}
			if !completed || observed < 100 {
				t.Fatal("sweep did not cover recognition through construction", completed, observed)
			}
			if _, err := parseBlocks(context.Background(), r.Document); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestInlineLinkScopedAppendAndPartialTab(t *testing.T) {
	// Scoped suffix retirement happens during the scan; new delimiter runs must
	// link to the remaining live prefix without reviving any processed label run.
	input := "*[a *b*](c) [**d**](e)*"
	source := NewSource([]byte(input))
	span, _ := source.Span(0, len(input))
	parts := []Segment{{Source: span, Transformation: Exact, Semantic: Some(input)}}
	s := inlineScanner{ctx: context.Background(), text: input}
	s.opaqueCandidates()
	flat, err := s.scan(parts)
	if err != nil || len(flat) != 5 || flat[1].Kind() != Link || flat[3].Kind() != Link || flat[4].Kind() != Text {
		t.Fatal("scoped scan lost nodes", flat, err)
	}
	nodes, err := s.emphasis(parts, flat)
	if err != nil || len(nodes) != 1 || nodes[0].Kind() != Emphasis {
		t.Fatal("outer delimiter lost after scoped calls", nodes, err)
	}
	children := nodes[0].Children()
	if len(children) != 3 || children[0].Kind() != Link || children[2].Kind() != Link || children[0].Children()[1].Kind() != Emphasis || children[2].Children()[0].Kind() != Strong {
		t.Fatal("label and outer nesting", children)
	}
	// Source-model fixture: block producers usually strip this indentation, but
	// the inline consumer must preserve residual tabs when handed logical Content.
	input = "[a\n\tb](uri)"
	source = NewSource([]byte(input))
	makeSpan := func(a, b int) Span { p, _ := source.Span(a, b); return p }
	parts = []Segment{
		{Source: makeSpan(0, 3), Transformation: Exact, Semantic: Some("[a\n")},
		{Source: makeSpan(3, 4), Transformation: PartialTab, Semantic: Some("  ")},
		{Source: makeSpan(4, len(input)), Transformation: Exact, Semantic: Some("b](uri)")},
	}
	s = inlineScanner{ctx: context.Background(), text: "[a\n  b](uri)"}
	s.opaqueCandidates()
	nodes, err = s.scan(parts)
	if err != nil || len(nodes) != 1 || nodes[0].Kind() != Link || nodes[0].Content()[1] != parts[1] || nodes[0].Properties().Link.Label.Semantic != Some("a\n  b") {
		t.Fatal("partial-tab label source", nodes, err)
	}
}

func TestInlineLinkComponentGrammar(t *testing.T) {
	for _, tt := range []struct {
		input string
		valid bool
	}{
		{"](foo(and(bar(baz))))", true}, {" ](foo)", false},
		{"](<> '')", true}, {"](uri \"\")", true}, {"](uri (a\\(b\\)))", true},
		{"]( \t\r\n <a b>\t 'x\r\ny' \t)", true},
		{"](uri\u00a0\"title\")", true},
		{"](uri\f\"title\")", false}, {"](a\x7fb)", false},
		{"](a\tb)", false}, {"](a\nb)", false}, {"](<a\rb>)", false},
		{"](a (b(c)))", false}, {"](a \"x\"tail)", false},
		{"](a\r\n \r\n\"title\")", false}, {"](a \"x\n \ny\")", false},
		{"](foo(and(bar))", false}, {"](<unterminated)", false},
	} {
		s := inlineScanner{ctx: context.Background(), text: tt.input}
		syntax, valid := s.inlineLink(0)
		if valid != tt.valid || valid && syntax.end != len(tt.input) {
			t.Fatal("raw component grammar", tt.input, syntax, valid)
		}
	}
}
