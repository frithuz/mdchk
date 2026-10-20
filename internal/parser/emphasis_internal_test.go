package parser

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestEmphasisClassificationAndRuleThree(t *testing.T) {
	for _, tt := range []struct {
		marker        byte
		before, after rune
		open, close   bool
	}{
		{'*', ' ', 'a', true, false}, {'*', 'a', ' ', false, true}, {'*', 'a', 'b', true, true},
		{'_', 'a', 'b', false, false}, {'_', '(', 'a', true, false}, {'_', 'a', ')', false, true},
		{'*', 'a', '\u00a0', false, true}, {'*', 'a', '\u0085', true, true},
		{'*', 'a', '€', false, true}, {'*', '€', 'a', true, false}, {'*', 'a', '\f', false, true},
	} {
		open, close := delimiterFlanking(tt.marker, tt.before, tt.after)
		if open != tt.open || close != tt.close {
			t.Fatal(tt, open, close)
		}
	}
	for _, tt := range []struct {
		a, b        int
		dual, match bool
	}{
		{1, 2, true, false}, {2, 1, true, false}, {3, 3, true, true}, {6, 9, true, true}, {2, 2, true, true}, {1, 2, false, true},
	} {
		a := inlineDelimiter{marker: '*', canOpen: true, canClose: tt.dual, original: tt.a, width: tt.a}
		b := inlineDelimiter{marker: '*', canClose: true, original: tt.b, width: tt.b}
		if delimitersMatch(a, b) != tt.match {
			t.Fatal("modulo-three eligibility", tt)
		}
		b.marker = '_'
		if delimitersMatch(a, b) {
			t.Fatal("mixed delimiter match")
		}
	}
}

func TestEmphasisInterruptionPreservesBlockTree(t *testing.T) {
	for _, input := range []string{
		"before " + strings.Repeat("*x* **y** ", 600),
		"before " + strings.Repeat("a**b_c* ", 600),
		"before " + strings.Repeat("*", 8192) + "x",
		"> - *é\x00\r\n>   " + strings.Repeat("payload ", 500) + "β*",
	} {
		r := Parse(context.Background(), NewSource([]byte(input)))
		if r.Status != Complete {
			t.Fatal("fixture incomplete", r)
		}
		for _, deadline := range []bool{false, true} {
			if _, err := parseBlocks(context.Background(), r.Document); err != nil {
				t.Fatal(err)
			}
			before := r.Document.Root()
			observed := 0
			for checkpoint := 1; checkpoint < 250; checkpoint++ {
				ctx := &quoteCheckpoint{checkpointContext: checkpointContext{Context: context.Background(), remaining: checkpoint}, deadline: deadline}
				state, err := parseInlines(ctx, r.Document, PhaseComplete)
				if state != Interrupted {
					break
				}
				observed++
				want := Cancelled
				if deadline {
					want = DeadlineExceeded
				}
				if err != nil || interruption(ctx) != want || !reflect.DeepEqual(before, r.Document.Root()) {
					t.Fatal("unfinished inline publication", state, err)
				}
				bad := Parse(ctx, NewSource([]byte("*é\xff*")))
				if bad.Failure == nil || bad.Failure.Code != InvalidUTF8 || bad.Failure.Location.Start() != 3 || bad.Interruption != "" {
					t.Fatal("encoding precedence", bad)
				}
			}
			if observed < 10 {
				t.Fatal("missing checkpoints", observed)
			}
			if state, err := parseInlines(context.Background(), r.Document, PhaseComplete); state != PhaseComplete || err != nil {
				t.Fatal(state, err)
			}
			if len(before.Children()[0].Children()) != 0 && before.Children()[0].Kind() == Paragraph {
				t.Fatal("borrowed snapshot mutated")
			}
		}
	}
}

func TestEmphasisPairingAndMappingCheckpoints(t *testing.T) {
	// Begin after opaque/flat scanning so the deterministic sweep reaches the new
	// matching and mapping loops, rather than spending every checkpoint in scanning.
	input := "prefix " + strings.Repeat("*x* **y** ", 16) + "suffix"
	source := NewSource([]byte(input))
	span, _ := source.Span(0, len(input))
	parts := []Segment{{Source: span, Transformation: Exact, Semantic: Some(input)}}
	original := inlineScanner{ctx: context.Background(), text: input}
	original.opaqueCandidates()
	flat, err := original.scan(parts)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]*Node(nil), flat...)
	for _, deadline := range []bool{false, true} {
		interrupted, completed := 0, false
		for checkpoint := 1; checkpoint < 3000; checkpoint++ {
			ctx := &quoteCheckpoint{checkpointContext: checkpointContext{Context: context.Background(), remaining: checkpoint}, deadline: deadline}
			scanner := inlineScanner{ctx: ctx, text: input, delimiters: append([]inlineDelimiter(nil), original.delimiters...)}
			nodes, err := scanner.emphasis(parts, flat)
			if err != nil {
				t.Fatal("interruption became model failure", err)
			}
			if !reflect.DeepEqual(flat, before) || parts[0].Semantic != Some(input) {
				t.Fatal("borrowed flat nodes/content changed")
			}
			if interruption(ctx) != "" {
				interrupted++
				continue
			}
			if scanner.pending || len(nodes) != 65 {
				t.Fatal("completed flat mapping", len(nodes), scanner.pending)
			}
			completed = true
			break
		}
		if !completed || interrupted < 100 {
			t.Fatal("pairing/mapping sweep did not span completion", completed, interrupted)
		}
	}
}

func TestEmphasisPartialTabLogicalContent(t *testing.T) {
	source := NewSource([]byte("*a\n\tb*"))
	span := func(a, b int) Span {
		s, err := source.Span(a, b)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	parts := []Segment{
		{Source: span(0, 3), Transformation: Exact, Semantic: Some("*a\n")},
		{Source: span(3, 4), Transformation: PartialTab, Semantic: Some("  ")},
		{Source: span(4, 6), Transformation: Exact, Semantic: Some("b*")},
	}
	s := inlineScanner{ctx: context.Background(), text: "*a\n  b*"}
	s.opaqueCandidates()
	flat, err := s.scan(parts)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := s.emphasis(parts, flat)
	if err != nil || s.pending || len(nodes) != 1 || nodes[0].Kind() != Emphasis {
		t.Fatal(nodes, err)
	}
	payload := nodes[0].Content()
	if len(payload) != 3 || payload[1].Source != span(3, 4) || payload[1].Transformation != PartialTab || payload[1].Semantic != Some("  ") {
		t.Fatal("lost tab provenance", payload)
	}
	if parts[0].Source != span(0, 3) || parts[2].Semantic != Some("b*") {
		t.Fatal("borrowed content changed")
	}
}
