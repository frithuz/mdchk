package parser

import (
	"context"
	"testing"
)

func TestInlinePhaseRequiresFinishedReferenceHandoff(t *testing.T) {
	r := Parse(context.Background(), NewSource(nil))
	if got := parseInlines(context.Background(), r.Document, Pending); got != NotStarted {
		t.Fatal("inline phase consumed unfinished block work")
	}
	r.Document.references.LookupAvailability = Unavailable
	if got := parseInlines(context.Background(), r.Document, PhaseComplete); got != NotStarted {
		t.Fatal("inline phase consumed unavailable references")
	}
	r.Document.references.LookupAvailability = Available
	if got := parseInlines(context.Background(), r.Document, PhaseComplete); got != PhaseComplete {
		t.Fatal("complete empty handoff did not permit inline phase")
	}
}

// Deterministic operation-budget stand-in: cancel at a selected checkpoint,
// without racing a goroutine against the parser or depending on wall-clock time.
type checkpointContext struct {
	context.Context
	remaining int
}

func TestHTMLInterruptionDiscardsOnlyOpenBlock(t *testing.T) {
	for _, opening := range []string{"<!--", "<pre>", "<div>", "<custom>"} {
		source := NewSource([]byte("***\n<!-- closed -->\n" + opening + "\nopen\n"))
		// Interrupt before the last payload line, or at EOF finalization.
		for _, checkpoint := range []int{5, 6} {
			r := Parse(context.Background(), source)
			ctx := &checkpointContext{Context: context.Background(), remaining: checkpoint}
			state, err := parseBlockLifecycle(ctx, r.Document, nil)
			children := r.Document.Root().Children()
			if err != nil || state != Interrupted || r.Document.inlineFree || r.Document.References().LookupAvailability != Unavailable {
				t.Fatal("interrupted HTML handed off complete work", state, err)
			}
			if len(children) != 2 || children[0].Kind() != ThematicBreak || children[1].Kind() != HTMLBlock || children[1].Span().Start() != 4 || children[1].Span().End() != 20 || children[1].Properties().HTML.BlockForm != Some(2) {
				t.Fatal("lost closed prefix or published interrupted HTML", children)
			}
		}
	}
	r := Parse(context.Background(), NewSource([]byte("<!-- closed -->\n")))
	document, err := NewDocument(r.Document.Root(), r.Document.References())
	if err != nil {
		t.Fatal(err)
	}
	if parseInlines(context.Background(), document, PhaseComplete) != Pending {
		t.Fatal("constructed HTML tree claimed private recognition proof")
	}
}

func (c *checkpointContext) Err() error {
	c.remaining--
	if c.remaining <= 0 {
		return context.Canceled
	}
	return nil
}

func TestFencedInterruptionDiscardsOnlyOpenFence(t *testing.T) {
	source := NewSource([]byte("***\n```\nfirst\n```\n~~~\nopen\n"))
	// The last checkpoint interrupts at EOF, after the final payload line but
	// before normal unclosed-fence finalization. No sleeps or goroutine race.
	for _, checkpoint := range []int{6, 7, 8} {
		r := Parse(context.Background(), source)
		ctx := &checkpointContext{Context: context.Background(), remaining: checkpoint}
		state, err := parseBlockLifecycle(ctx, r.Document, nil)
		children := r.Document.Root().Children()
		if err != nil || state != Interrupted || r.Document.inlineFree || r.Document.References().LookupAvailability != Unavailable {
			t.Fatal("interrupted fence handed off complete work", state, err)
		}
		if len(children) != 2 || children[0].Kind() != ThematicBreak || children[1].Kind() != CodeBlock || children[1].Span().Start() != 4 || children[1].Span().End() != 18 || children[1].Properties().Literal != Some("first\n") || children[1].Properties().Code.Style != "fenced" {
			t.Fatal("lost closed prefix or published interrupted open fence")
		}
	}
	r := Parse(context.Background(), NewSource([]byte("```\ncode\n```\n")))
	document, err := NewDocument(r.Document.Root(), r.Document.References())
	if err != nil {
		t.Fatal(err)
	}
	if parseInlines(context.Background(), document, PhaseComplete) != Pending {
		t.Fatal("constructed fenced tree claimed private recognition proof")
	}
}

func TestIndentedInterruptionDiscardsOpenCodeAndBlanks(t *testing.T) {
	source := NewSource([]byte("    first\n***\n    second\n    third\n\n    fourth\n"))
	for _, checkpoint := range []int{5, 6, 7} {
		r := Parse(context.Background(), source)
		ctx := &checkpointContext{Context: context.Background(), remaining: checkpoint}
		state, err := parseBlockLifecycle(ctx, r.Document, nil)
		children := r.Document.Root().Children()
		if err != nil || state != Interrupted || r.Document.inlineFree || r.Document.References().LookupAvailability != Unavailable {
			t.Fatal("interrupted code handed off complete work", state, err)
		}
		if len(children) != 2 || children[0].Kind() != CodeBlock || children[0].Span().Start() != 0 || children[0].Span().End() != 10 || children[0].Properties().Literal != Some("first\n") || children[1].Kind() != ThematicBreak {
			t.Fatal("lost closed prefix or published interrupted code/blanks")
		}
	}
	// A constructed code tree and supplied references still cannot manufacture
	// the private proof of recognition used by the inline-free handoff.
	r := Parse(context.Background(), NewSource([]byte("    code\n")))
	document, err := NewDocument(r.Document.Root(), r.Document.References())
	if err != nil {
		t.Fatal(err)
	}
	if parseInlines(context.Background(), document, PhaseComplete) != Pending {
		t.Fatal("constructed CodeBlock tree claimed recognition")
	}
}

func TestInlineStubRefusesNonemptyInlineHandoff(t *testing.T) {
	for _, input := range []string{"hello\n", "# heading\n", "***\nhello\n", "***\n# heading\n", "heading\n=\n"} {
		r := Parse(context.Background(), NewSource([]byte(input)))
		r.Document.references = ReferenceCollection{Availability: Available, LookupAvailability: Available}
		if got := parseInlines(context.Background(), r.Document, PhaseComplete); got != Pending {
			t.Fatalf("inline stub completed %q without a provider: %s", input, got)
		}
	}
}

func TestSetextInterruptionKeepsOnlyPromotedHeadings(t *testing.T) {
	for _, cancelOn := range []byte{'T', 'o'} {
		r := Parse(context.Background(), NewSource([]byte("Title\n---\nopen\nnext\n")))
		ctx, cancel := context.WithCancel(context.Background())
		state, err := parseBlockLifecycle(ctx, r.Document, []blockStartHook{func(line blockLine, open bool) bool {
			if line.first == cancelOn {
				cancel()
			}
			return false
		}})
		cancel()
		children := r.Document.Root().Children()
		if err != nil || state != Interrupted || r.Document.inlineFree || r.Document.References().LookupAvailability != Unavailable {
			t.Fatal("interrupted Setext work claimed completeness", state, err)
		}
		if cancelOn == 'T' {
			if len(children) != 0 {
				t.Fatal("interrupted paragraph was promoted")
			}
		} else if len(children) != 1 || children[0].Kind() != Heading || children[0].Properties().Heading.Style != "setext" || children[0].Span().Start() != 0 || children[0].Span().End() != 10 {
			t.Fatal("interruption lost promoted heading or published unfinished paragraph")
		}
	}
}

func TestInlineStubRequiresProvenInlineFreeSource(t *testing.T) {
	source := NewSource([]byte("***\n"))
	envelope, _ := source.Span(0, source.Len())
	marker, _ := source.Span(0, 3)
	breakNode, err := NewNode(NodeSpec{Kind: ThematicBreak, Envelope: envelope, Markers: []Marker{{Role: "thematic-marker", Source: marker}}})
	if err != nil {
		t.Fatal(err)
	}
	// Neither source-only nor constructed thematic children prove recognition.
	for _, children := range [][]*Node{nil, {breakNode}} {
		root, err := NewNode(NodeSpec{Kind: DocumentKind, Envelope: envelope, Children: children})
		if err != nil {
			t.Fatal(err)
		}
		document, err := NewDocument(root, ReferenceCollection{Availability: Available, LookupAvailability: Available})
		if err != nil {
			t.Fatal(err)
		}
		if got := parseInlines(context.Background(), document, PhaseComplete); got != Pending {
			t.Fatal("unproven handoff completed", got)
		}
	}
	r := Parse(context.Background(), source)
	if !r.Document.inlineFree || parseInlines(context.Background(), r.Document, PhaseComplete) != PhaseComplete {
		t.Fatal("proven thematic-only handoff did not complete")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if state, err := parseBlockLifecycle(ctx, r.Document, nil); state != Interrupted || err != nil || r.Document.inlineFree || r.Document.references.LookupAvailability != Unavailable {
		t.Fatal("interrupted re-entry retained the previous complete handoff")
	}
}

func TestBlockStartHooksPrecedeContinuation(t *testing.T) {
	r := Parse(context.Background(), NewSource([]byte("closed\n\nopen\nSTOP\n")))
	var openAtCall []bool
	state, err := parseBlockLifecycle(context.Background(), r.Document, []blockStartHook{
		func(line blockLine, open bool) bool {
			openAtCall = append(openAtCall, open)
			return line.first == 'S'
		},
		func(line blockLine, open bool) bool {
			if line.first == 'S' {
				t.Fatal("later hook ran after an earlier deferral")
			}
			return false
		},
	})
	if err != nil || state != Pending || len(openAtCall) != 3 || openAtCall[0] || openAtCall[1] || !openAtCall[2] {
		t.Fatalf("wrong open/close/continuation hook sequence: %v, %s, %v", openAtCall, state, err)
	}
	children := r.Document.Root().Children()
	if len(children) != 1 || children[0].Span().Start() != 0 || children[0].Span().End() != 7 {
		t.Fatal("deferred start retained its potentially transformed open paragraph")
	}
}

func TestBlockLifecycleInterruptionRetainsOnlyClosedPrefix(t *testing.T) {
	r := Parse(context.Background(), NewSource([]byte("closed\n\nopen\nnext\n")))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state, err := parseBlockLifecycle(ctx, r.Document, []blockStartHook{func(line blockLine, open bool) bool {
		if line.first == 'o' {
			cancel()
		}
		return false
	}})
	if err != nil || state != Interrupted || len(r.Document.Root().Children()) != 1 || r.Document.References().LookupAvailability != Unavailable {
		t.Fatalf("interrupted lifecycle published unfinished work: %s, %v", state, err)
	}
}

func TestThematicInterruptionRetainsClosedBreak(t *testing.T) {
	r := Parse(context.Background(), NewSource([]byte("***\nopen\nnext\n")))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state, err := parseBlockLifecycle(ctx, r.Document, []blockStartHook{func(line blockLine, open bool) bool {
		if line.first == 'o' {
			cancel()
		}
		return false
	}})
	children := r.Document.Root().Children()
	if err != nil || state != Interrupted || len(children) != 1 || children[0].Kind() != ThematicBreak || children[0].Span().Start() != 0 || children[0].Span().End() != 4 || r.Document.inlineFree || r.Document.References().LookupAvailability != Unavailable {
		t.Fatal("interruption published unfinished or complete source", state, err)
	}
}

func TestATXCompletionRequiresRecognizedInlineFreeSource(t *testing.T) {
	source := NewSource([]byte("#\n"))
	envelope, _ := source.Span(0, 2)
	opening, _ := source.Span(0, 1)
	heading, err := NewNode(NodeSpec{Kind: Heading, Envelope: envelope,
		Markers:    []Marker{{Role: "opening", Source: opening}},
		Properties: Properties{Heading: &HeadingProperties{Level: 1, Style: "atx"}}})
	if err != nil {
		t.Fatal(err)
	}
	root, err := NewNode(NodeSpec{Kind: DocumentKind, Envelope: envelope, Children: []*Node{heading}})
	if err != nil {
		t.Fatal(err)
	}
	document, err := NewDocument(root, ReferenceCollection{Availability: Available, LookupAvailability: Available})
	if err != nil {
		t.Fatal(err)
	}
	if parseInlines(context.Background(), document, PhaseComplete) != Pending {
		t.Fatal("constructed empty heading fabricated recognition proof")
	}
	r := Parse(context.Background(), source)
	if !r.Document.inlineFree || parseInlines(context.Background(), r.Document, PhaseComplete) != PhaseComplete {
		t.Fatal("actually recognized empty heading did not complete")
	}
	// Cancel deterministically in the next ordinary line, before the following
	// line's checkpoint. Retain the closed heading but discard unfinished text.
	r = Parse(context.Background(), NewSource([]byte("#\nopen\nnext\n")))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state, err := parseBlockLifecycle(ctx, r.Document, []blockStartHook{func(line blockLine, open bool) bool {
		if line.first == 'o' {
			cancel()
		}
		return false
	}})
	children := r.Document.Root().Children()
	if err != nil || state != Interrupted || len(children) != 1 || children[0].Kind() != Heading || children[0].Span().Start() != 0 || children[0].Span().End() != 2 || r.Document.inlineFree || r.Document.References().LookupAvailability != Unavailable {
		t.Fatal("interruption lost closed heading or published a complete handoff", state, err)
	}
}
