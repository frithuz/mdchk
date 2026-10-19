package parser

import (
	"context"
	"reflect"
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
