package parser

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestInlineInterruptionAndEncodingPrecedence(t *testing.T) {
	source := NewSource([]byte("> " + strings.Repeat("plain words ", 800) + "\n"))
	for _, deadline := range []bool{false, true} {
		observed := 0
		for checkpoint := 1; checkpoint < 100; checkpoint++ {
			ctx := &quoteCheckpoint{checkpointContext: checkpointContext{Context: context.Background(), remaining: checkpoint}, deadline: deadline}
			r := Parse(ctx, source)
			if r.Phases.Inlines != Interrupted {
				continue
			}
			observed++
			want := Cancelled
			if deadline {
				want = DeadlineExceeded
			}
			if r.Interruption != want || r.Status != Incomplete || r.AST != Partial || r.Failure != nil || r.Phases.Blocks != PhaseComplete || r.Document.references.LookupAvailability != Unavailable || len(r.Uncertainty) != 1 || r.Uncertainty[0].Reason != want {
				t.Fatal("wrong inline interruption", checkpoint, r)
			}
			Walk(r.Document.Root(), func(n *Node) bool {
				if n.Kind() == Text {
					t.Fatal("unfinished inline tree was published", checkpoint)
				}
				return true
			})
			bad := Parse(ctx, NewSource([]byte("é\xff")))
			if bad.Status != Failed || bad.Failure == nil || bad.Failure.Code != InvalidUTF8 || bad.Interruption != "" || bad.Failure.Location.Start() != 2 {
				t.Fatal("encoding lost precedence", bad)
			}
		}
		if observed < 10 {
			t.Fatal("no bounded scanning checkpoints exercised", observed)
		}
	}
}

func TestInlineInterruptionDoesNotMutateBlockSnapshot(t *testing.T) {
	source := NewSource([]byte(strings.Repeat("x", 4096)))
	r := Parse(context.Background(), source)
	if _, err := parseBlocks(context.Background(), r.Document); err != nil {
		t.Fatal(err)
	}
	before := r.Document.Root()
	ctx := &checkpointContext{Context: context.Background(), remaining: 8}
	state, err := parseInlines(ctx, r.Document, PhaseComplete)
	if err != nil || state != Interrupted || !reflect.DeepEqual(before, r.Document.Root()) {
		t.Fatal("partial publication", state, err)
	}
	state, err = parseInlines(context.Background(), r.Document, PhaseComplete)
	if err != nil || state != PhaseComplete || len(before.spec.Children[0].spec.Children) != 0 || len(r.Document.root.spec.Children[0].spec.Children) != 1 {
		t.Fatal("old immutable block handle changed")
	}
}

func TestCodeSpanInterruptionPreservesBlockTree(t *testing.T) {
	// Sweep deterministic checkpoints through indexing, matching, source mapping
	// and normalization, after block recognition. No partially built inline tree
	// may replace the immutable block snapshot, for either interruption reason.
	for _, input := range []string{
		"before " + strings.Repeat("`x` ", 600),
		"before ` " + strings.Repeat("large payload ", 900) + " ` after",
		"before " + strings.Repeat("`", 2048) + "x",
	} {
		r := Parse(context.Background(), NewSource([]byte(input)))
		if _, err := parseBlocks(context.Background(), r.Document); err != nil {
			t.Fatal(err)
		}
		before := r.Document.Root()
		for _, deadline := range []bool{false, true} {
			if _, err := parseBlocks(context.Background(), r.Document); err != nil {
				t.Fatal(err)
			}
			observed := 0
			for checkpoint := 1; checkpoint < 150; checkpoint++ {
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
					t.Fatal("partial inline publication", state, err)
				}
				bad := Parse(ctx, NewSource([]byte("`é\xff`")))
				if bad.Failure == nil || bad.Failure.Code != InvalidUTF8 || bad.Failure.Location.Start() != 3 || bad.Interruption != "" {
					t.Fatal("encoding lost precedence", bad)
				}
			}
			if observed < 10 {
				t.Fatal("missing bounded code checkpoints", observed)
			}
		}
		state, err := parseInlines(context.Background(), r.Document, PhaseComplete)
		if state != PhaseComplete || err != nil || len(before.Children()[0].Children()) != 0 {
			t.Fatal("old block handle changed", state, err)
		}
	}
}

func TestBreakInterruptionPreservesBlockTree(t *testing.T) {
	for _, input := range []string{
		strings.Repeat("a  \r\nb\\\nc \rd\n", 200),
		"before" + strings.Repeat(" ", 8192) + "\nafter",
	} {
		r := Parse(context.Background(), NewSource([]byte(input)))
		if _, err := parseBlocks(context.Background(), r.Document); err != nil {
			t.Fatal(err)
		}
		before := r.Document.Root()
		for _, deadline := range []bool{false, true} {
			if _, err := parseBlocks(context.Background(), r.Document); err != nil {
				t.Fatal(err)
			}
			observed := 0
			for checkpoint := 1; checkpoint < 200; checkpoint++ {
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
					t.Fatal("partial break publication", state, err)
				}
				bad := Parse(ctx, NewSource([]byte("a  \n\xff")))
				if bad.Failure == nil || bad.Failure.Code != InvalidUTF8 || bad.Failure.Location.Start() != 4 || bad.Interruption != "" {
					t.Fatal("break scanning changed encoding precedence", bad)
				}
			}
			if observed < 10 {
				t.Fatal("missing break/mapping/whitespace checkpoints", observed)
			}
		}
		state, err := parseInlines(context.Background(), r.Document, PhaseComplete)
		if err != nil || state != PhaseComplete || len(before.Children()[0].Children()) != 0 {
			t.Fatal("break scan changed borrowed block tree", state, err)
		}
	}
}

func TestCodeSpanPartialTabLogicalContent(t *testing.T) {
	// Exercise the established logical-segment contract directly. Paragraph
	// production currently strips leading indentation; this is source-model
	// coverage, not a claim of new block/indentation recognition.
	source := NewSource([]byte("`a\n\tb`"))
	span := func(a, b int) Span {
		s, err := source.Span(a, b)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	parts := []Segment{
		{Source: span(0, 3), Transformation: Exact, Semantic: Some("`a\n")},
		{Source: span(3, 4), Transformation: PartialTab, Semantic: Some("  ")},
		{Source: span(4, 6), Transformation: Exact, Semantic: Some("b`")},
	}
	scanner := inlineScanner{ctx: context.Background(), text: "`a\n  b`"}
	scanner.opaqueCandidates()
	nodes, err := scanner.scan(parts)
	if err != nil || len(nodes) != 1 || nodes[0].Properties().Literal != Some("a   b") {
		t.Fatal(nodes, err)
	}
	payload := nodes[0].Content()
	if len(payload) != 3 || payload[1].Source != span(3, 4) || payload[1].Transformation != PartialTab || payload[1].Semantic != Some("  ") {
		t.Fatal("lost residual tab", payload)
	}
	if parts[0].Semantic != Some("`a\n") || parts[2].Source != span(4, 6) {
		t.Fatal("borrowed segments mutated")
	}
}

func TestCodeSpanNormalizationCheckpointsAcrossCRLF(t *testing.T) {
	// CRLF pairs straddle offsets divisible by 256. A modulo-only checkpoint
	// can skip every later check when the loop advances past both ending bytes.
	text := strings.Repeat("x", 255) + "\r\n" + strings.Repeat(strings.Repeat("x", 254)+"\r\n", 30)
	source := NewSource([]byte(text))
	span, err := source.Span(0, len(text))
	if err != nil {
		t.Fatal(err)
	}
	ctx := &checkpointContext{Context: context.Background(), remaining: 4}
	value := normalizeCodeSpan(ctx, []Segment{{Source: span, Transformation: Exact, Semantic: Some(text)}})
	if value.Semantic.Present || ctx.remaining > 0 {
		t.Fatal("missed normalization interruption", ctx.remaining)
	}
}

func TestAutolinkInterruptionPreservesBlockTree(t *testing.T) {
	for _, input := range []string{
		"before <ab:" + strings.Repeat("x", 10000) + "> after",
		"before <" + strings.Repeat("x", 10000) + "@b> after",
		"before <a@" + strings.Repeat("x.", 5000) + "b> after",
		"before " + strings.Repeat("<ab:é\x00><a@b> ", 200),
		"before " + strings.Repeat("<invalid", 1000),
	} {
		r := Parse(context.Background(), NewSource([]byte(input)))
		if _, err := parseBlocks(context.Background(), r.Document); err != nil {
			t.Fatal(err)
		}
		before := r.Document.Root()
		for _, deadline := range []bool{false, true} {
			if _, err := parseBlocks(context.Background(), r.Document); err != nil {
				t.Fatal(err)
			}
			observed := 0
			for checkpoint := 1; checkpoint < 200; checkpoint++ {
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
					t.Fatal("partial autolink publication", state, err)
				}
				bad := Parse(ctx, NewSource([]byte("<ab:é\xff>")))
				if bad.Failure == nil || bad.Failure.Code != InvalidUTF8 || bad.Failure.Location.Start() != 6 || bad.Interruption != "" {
					t.Fatal("autolink encoding precedence", bad)
				}
			}
			if observed < 10 {
				t.Fatal("missing autolink checkpoints", observed)
			}
		}
		state, err := parseInlines(context.Background(), r.Document, PhaseComplete)
		if err != nil || state != PhaseComplete || len(before.Children()[0].Children()) != 0 {
			t.Fatal("autolink changed borrowed block snapshot", state, err)
		}
	}
}

func TestInlineHTMLInterruptionPreservesBlockTree(t *testing.T) {
	for _, input := range []string{
		"before <!--" + strings.Repeat("x", 10000) + "--> after",
		"before " + strings.Repeat("<?bad <!--open <![CDATA[", 400),
		"before <a " + strings.Repeat("attribute='long value' ", 400) + "> after",
		"> - before <!--é\x00\n>   " + strings.Repeat("x", 10000) + " --> after",
	} {
		r := Parse(context.Background(), NewSource([]byte(input)))
		if r.Status != Complete {
			t.Fatal(r)
		}
		for _, deadline := range []bool{false, true} {
			if _, err := parseBlocks(context.Background(), r.Document); err != nil {
				t.Fatal(err)
			}
			before := r.Document.Root()
			observed := 0
			for checkpoint := 1; checkpoint < 200; checkpoint++ {
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
					t.Fatal("partial HTML publication", state, err)
				}
				bad := Parse(ctx, NewSource([]byte("x <!--\xff-->")))
				if bad.Failure == nil || bad.Failure.Code != InvalidUTF8 || bad.Failure.Location.Start() != 6 || bad.Interruption != "" {
					t.Fatal("HTML encoding precedence", bad)
				}
			}
			if observed < 10 {
				t.Fatal("missing HTML indexing/recognition/mapping checkpoints", observed)
			}
			if state, err := parseInlines(context.Background(), r.Document, PhaseComplete); err != nil || state != PhaseComplete || before.Children()[0].Kind() == Paragraph && len(before.Children()[0].Children()) != 0 {
				t.Fatal("borrowed HTML block tree changed", state, err)
			}
		}
	}
}

func TestInlineHTMLLogicalWhitespaceGrammar(t *testing.T) {
	// Exercise productions directly where block-first parsing can cut the
	// paragraph before a newline followed by '>' or a blank physical line.
	for _, tt := range []struct {
		raw   string
		valid bool
	}{
		{"</a\n>", true}, {"</a\r\n>", true}, {"<a\r b\n=\r\n'v'\n/>", true},
		{"<a\n\nb>", false}, {"</a\n\n>", false}, {"<a x\r\n\r\n=v>", false},
		{"<a x=\n\nv>", false}, {"<a\f>", false}, {"<a x=unquoted\fvalue>", true},
	} {
		s := inlineScanner{ctx: context.Background(), text: tt.raw}
		end, _ := s.inlineHTMLEnd(0)
		if tt.valid && end != len(tt.raw) || !tt.valid && end != 0 {
			t.Fatal("Raw HTML whitespace production", strconv.Quote(tt.raw), end, tt.valid)
		}
	}
}
