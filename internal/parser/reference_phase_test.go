package parser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestReferencePhaseAndSemanticAvailability(t *testing.T) {
	for _, tt := range []struct {
		input           string
		blocks, inlines PhaseState
		status          Status
		semantic        bool
	}{
		{"[a]: /url\n", PhaseComplete, PhaseComplete, Complete, true},
		{"text\n\n[a]: /url\n", PhaseComplete, PhaseComplete, Complete, true},
		{"[a]: /url\\*\n", PhaseComplete, PhaseComplete, Complete, true},
		{"[a]: /url \"&amp;\"\n", PhaseComplete, PhaseComplete, Complete, true},
	} {
		r := Parse(context.Background(), NewSource([]byte(tt.input)))
		refs := r.Document.References()
		if r.Failure != nil || r.Phases != (PhaseProgress{Blocks: tt.blocks, Inlines: tt.inlines}) || r.Status != tt.status || refs.Availability != Available || refs.LookupAvailability != Available || len(refs.Definitions) != 1 || refs.Definitions[0].spec.Destination.Semantic.Present != tt.semantic {
			t.Fatalf("%q: %+v %+v", tt.input, r, refs)
		}
	}
	// Decoded title data is known and distinct from an empty destination.
	r := Parse(context.Background(), NewSource([]byte("[a]: <> \"&amp;\"\n")))
	definition := r.Document.References().Definitions[0].Properties()
	if definition.Destination.Semantic != Some("") || definition.Title == nil || definition.Title.Semantic != Some("&") || definition.Title.Segments[0].Transformation != Entity {
		t.Fatal(definition)
	}
}

func TestReferenceInterruptionCannotPublishGlobalRegistry(t *testing.T) {
	for _, input := range []string{
		"***\n> [a]: /url \"first\n> second\n> third\"\n#\n",
		"***\n- [a]: /url\n  [b]: /next\n",
		"***\n[a]: /url\n===\n",
		"***\n[a]: /url\n#\n",
		"***\n> [a]: /url \"first\n" + strings.Repeat("outside line\n", 12) + "\"\n",
		"***\n- > [a\n  b]: /url\n",
	} {
		source := NewSource([]byte(input))
		baseline := Parse(context.Background(), source)
		if _, err := parseBlocks(context.Background(), baseline.Document); err != nil {
			t.Fatal(err)
		}
		completeChildren := baseline.Document.Root().Children()
		interrupted := 0
		for _, deadline := range []bool{false, true} {
			// Deterministic checkpoints sweep line processing, component scanning,
			// container closure, final registry publication and phase boundaries.
			for checkpoint := 1; checkpoint < 100; checkpoint++ {
				ctx := &quoteCheckpoint{checkpointContext: checkpointContext{Context: context.Background(), remaining: checkpoint}, deadline: deadline}
				r := Parse(ctx, source)
				if r.Interruption == "" {
					continue
				}
				interrupted++
				want := Cancelled
				if deadline {
					want = DeadlineExceeded
				}
				if r.Interruption != want || r.Failure != nil || r.Status != Incomplete || r.Document.references.Availability != Unavailable || r.Document.references.LookupAvailability != Unavailable || len(r.Document.references.Definitions) != 0 || r.Document.inlineFree {
					t.Fatal("interruption published registry", checkpoint, r)
				}
				children := r.Document.Root().Children()
				if len(children) > len(completeChildren) {
					t.Fatal("interruption invented blocks")
				}
				for i, child := range children {
					if !reflect.DeepEqual(inlineBlockShape(child), inlineBlockShape(completeChildren[i])) {
						t.Fatal("interruption published unfinished structure", checkpoint, child)
					}
				}
				bad := Parse(ctx, NewSource([]byte("[a]: \xff")))
				if bad.Failure == nil || bad.Failure.Code != InvalidUTF8 || bad.Interruption != "" {
					t.Fatal("encoding lost precedence")
				}
			}
		}
		if interrupted < 10 {
			t.Fatal("insufficient checkpoint coverage", interrupted)
		}
	}
}

func TestFullCaseFoldAgainstPinnedUnicode(t *testing.T) {
	data, err := os.ReadFile("testdata/unicode/17.0.0/CaseFolding.txt")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != "ff8d8fefbf123574205085d6714c36149eb946d717a0c585c27f0f4ef58c4183" {
		t.Fatal("pinned folding data drift")
	}
	checked := 0
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(strings.SplitN(line, "#", 2)[0], ";")
		if len(fields) < 3 || strings.TrimSpace(fields[1]) != "C" && strings.TrimSpace(fields[1]) != "F" {
			continue
		}
		r, err := strconv.ParseInt(strings.TrimSpace(fields[0]), 16, 32)
		if err != nil {
			t.Fatal(err)
		}
		var want strings.Builder
		for _, c := range strings.Fields(fields[2]) {
			v, err := strconv.ParseInt(c, 16, 32)
			if err != nil {
				t.Fatal(err)
			}
			want.WriteRune(rune(v))
		}
		got := normalizeReferenceLabel(string(rune(r)))
		if got != want.String() || normalizeReferenceLabel(got) != got {
			t.Fatalf("U+%04X: %q want %q", r, got, want.String())
		}
		checked++
	}
	if checked != len(fullCaseFold) || checked < 1500 {
		t.Fatal("incomplete default full folding", checked)
	}
	if normalizeReferenceLabel("Iİı") != "ii\u0307ı" {
		t.Fatal("Turkic special mapping replaced default full folding")
	}
}

func FuzzReferenceRegistry(f *testing.F) {
	for _, s := range []string{"[a]: /u\n", "> [ẞ]: <>\n[SS]: /v\n", "[a]: /u \"bad\n", "[a]: (a(b)) 't'\n", "[a\\]]: /u\r\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 8192 {
			t.Skip()
		}
		r := Parse(context.Background(), NewSource([]byte(input)))
		if string(r.Source.Bytes()) != input {
			t.Fatal("source modified")
		}
		if r.Failure != nil {
			if r.Failure.Code != InvalidUTF8 {
				t.Fatal("invalid model", r.Failure)
			}
			return
		}
		refs := r.Document.References()
		first := map[string]int{}
		previous := -1
		for i, d := range refs.Definitions {
			p := d.Properties()
			if p.Envelope.Start() < previous {
				t.Fatal("source order")
			}
			previous = p.Envelope.Start()
			if _, err := r.Source.Text(p.Envelope); err != nil {
				t.Fatal("foreign envelope")
			}
			if !p.NormalizedLabel.Present {
				t.Fatal("missing matching identity")
			}
			if _, ok := first[p.NormalizedLabel.Value]; !ok {
				first[p.NormalizedLabel.Value] = i
			}
		}
		if len(first) != len(refs.Lookup) {
			t.Fatal("missing first selections")
		}
		for key, i := range first {
			if got, ok := refs.Lookup[key]; !ok || got != i {
				t.Fatal("wrong first selection")
			}
		}
	})
}

// Inline processing changes only leaf children; block interruption comparisons
// retain all independently expected block facts while ignoring that later phase.
func inlineBlockShape(n *Node) *Node {
	copy := *n
	copy.spec.Children = nil
	if n.Kind() != Paragraph && n.Kind() != Heading {
		for _, child := range n.spec.Children {
			copy.spec.Children = append(copy.spec.Children, inlineBlockShape(child))
		}
	}
	return &copy
}
