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
