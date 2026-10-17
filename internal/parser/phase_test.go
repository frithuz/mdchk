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

func TestInlineStubRefusesNonblankHandoff(t *testing.T) {
	for _, input := range []string{"hello\n", "# heading\n", "***\nhello\n", "***\n# heading\n"} {
		r := Parse(context.Background(), NewSource([]byte(input)))
		r.Document.references = ReferenceCollection{Availability: Available, LookupAvailability: Available}
		if got := parseInlines(context.Background(), r.Document, PhaseComplete); got != Pending {
			t.Fatalf("inline stub completed %q without a provider: %s", input, got)
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
