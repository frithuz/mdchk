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
	for _, input := range []string{"hello\n", "# heading\n"} {
		r := Parse(context.Background(), NewSource([]byte(input)))
		r.Document.references = ReferenceCollection{Availability: Available, LookupAvailability: Available}
		if got := parseInlines(context.Background(), r.Document, PhaseComplete); got != Pending {
			t.Fatalf("inline stub completed %q without a provider: %s", input, got)
		}
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
