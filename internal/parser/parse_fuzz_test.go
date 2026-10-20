package parser_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
)

// FuzzParse exercises the public entry point, including malformed UTF-8 and
// explicit resource interruptions. Seeds use the retained upstream Markdown
// bytes directly; no rendered output or transformed approximation is substituted.
func FuzzParse(f *testing.F) {
	data, err := os.ReadFile("testdata/commonmark/0.31.2/examples.json")
	if err != nil {
		f.Fatal(err)
	}
	var examples []struct {
		ID       int    `json:"example"`
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal(data, &examples); err != nil {
		f.Fatal(err)
	}
	if len(examples) != 652 {
		f.Fatalf("upstream seed count = %d, want 652", len(examples))
	}
	for i, example := range examples {
		if example.ID != i+1 {
			f.Fatalf("upstream example at %d has ID %d", i, example.ID)
		}
		f.Add(example.Markdown)
	}
	for _, input := range []string{
		"", "\xff", "é\x00\r\nβ\rZ", ">\t- ***é &amp; `x`***\r\n>\t  tail",
		strings.Repeat("> ", 300) + "nested\n",
		strings.Repeat("- ", 300) + "nested\n",
		strings.Repeat("***a ", 100) + strings.Repeat(" a***", 100),
		strings.Repeat("[![", 300) + "x" + strings.Repeat("](a))", 300),
		strings.Repeat("[a](", 300) + strings.Repeat("(a", 300),
		strings.Repeat("`", 1000) + "x" + strings.Repeat("`", 999),
		strings.Repeat("`a ``b ```c ", 300),
		"x <!--" + strings.Repeat("--!<", 500),
		"x <a title='" + strings.Repeat("<> \"", 500),
		strings.Repeat("[a]: /url \"title\"\n", 300) + "\n[a] ![a]\n",
		"> [a]: /url \"" + strings.Repeat("\n> continued", 300),
		strings.Repeat("[missing] ", 300),
	} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		// Bound each execution independently of the native fuzzer's time budget.
		// Production resource limits remain active within this retained size.
		if len(input) > 64<<10 {
			t.Skip()
		}
		caller := []byte(input)
		source := parser.NewSource(caller)
		r := parser.Parse(context.Background(), source)
		if string(caller) != input || string(source.Bytes()) != input || string(r.Source.Bytes()) != input {
			t.Fatal("Parse mutated original bytes")
		}
		if len(caller) > 0 {
			caller[0] ^= 0xff
			copy := source.Bytes()
			copy[0] ^= 0xff
		}
		if string(source.Bytes()) != input || !reflect.DeepEqual(r, parser.Parse(context.Background(), source)) {
			t.Fatal("source snapshot is mutable or Parse is nondeterministic")
		}
		if r.Failure != nil {
			if r.Failure.Code != parser.InvalidUTF8 || utf8.ValidString(input) {
				t.Fatalf("unexpected parser failure: %v", r.Failure)
			}
			checkParseFuzzSpan(t, source, r.Failure.Location, 0, len(input))
			return
		}
		if !utf8.ValidString(input) {
			t.Fatal("malformed UTF-8 was accepted")
		}
		if r.Limit != nil {
			if r.Status != parser.Incomplete || r.AST != parser.Partial || r.Interruption != parser.LimitExceededReason ||
				r.Limit.Name == "" || r.Limit.Observed <= r.Limit.Allowed || r.Limit.Allowed <= 0 ||
				len(r.Uncertainty) != 1 || r.Uncertainty[0].Reason != parser.LimitExceededReason ||
				r.Uncertainty[0].Region.Start() != 0 || r.Uncertainty[0].Region.End() != len(input) {
				t.Fatalf("resource exhaustion is not an explicit whole-source interruption: %+v", r)
			}
		} else if r.Status != parser.Complete || r.Interruption != "" || r.AST != parser.Available || len(r.Uncertainty) != 0 {
			t.Fatalf("bounded valid input neither completed nor reported a resource limit: %+v", r)
		}
		for _, u := range r.Uncertainty {
			checkParseFuzzSpan(t, source, u.Region, 0, len(input))
		}
		if r.Document == nil {
			t.Fatal("nonfailure result omitted the document")
		}
		checkParseFuzzDocument(t, source, r.Document)
	})
}

func checkParseFuzzSpan(t *testing.T, source parser.Source, span parser.Span, start, end int) {
	t.Helper()
	if !span.Present() || span.Start() < start || span.End() < span.Start() || span.End() > end {
		t.Fatalf("invalid span [%d,%d), owner [%d,%d)", span.Start(), span.End(), start, end)
	}
	if _, err := source.Text(span); err != nil {
		t.Fatalf("foreign or altered source span: %v", err)
	}
}

func checkParseFuzzDocument(t *testing.T, source parser.Source, doc *parser.Document) {
	t.Helper()
	segments := func(parts []parser.Segment, owner parser.Span) {
		t.Helper()
		previous := owner.Start()
		for _, part := range parts {
			if !part.Source.Present() {
				if part.Transformation != parser.Synthetic && part.Transformation != parser.MappingUnavailable {
					t.Fatal("missing source for a source-backed contribution")
				}
				continue
			}
			checkParseFuzzSpan(t, source, part.Source, owner.Start(), owner.End())
			if part.Source.Start() < previous {
				t.Fatal("unordered source contributions")
			}
			previous = part.Source.Start()
			if part.Transformation == parser.Exact && part.Semantic.Present {
				raw, _ := source.Text(part.Source)
				if raw != part.Semantic.Value {
					t.Fatal("exact contribution changed original bytes")
				}
			}
		}
	}
	markers := func(markers []parser.Marker, owner parser.Span) {
		t.Helper()
		previous := owner.Start()
		for _, m := range markers {
			checkParseFuzzSpan(t, source, m.Source, owner.Start(), owner.End())
			if m.Role == "" || m.Source.Start() < previous {
				t.Fatal("empty marker role or unordered markers")
			}
			previous = m.Source.Start()
		}
	}
	root := doc.Root()
	if root == nil || root.Span().Start() != 0 || root.Span().End() != source.Len() {
		t.Fatal("missing whole-source root")
	}
	parser.Walk(root, func(n *parser.Node) bool {
		owner := n.Span()
		checkParseFuzzSpan(t, source, owner, 0, source.Len())
		markers(n.Markers(), owner)
		segments(n.Content(), owner)
		previous := owner.Start()
		for _, child := range n.Children() {
			checkParseFuzzSpan(t, source, child.Span(), owner.Start(), owner.End())
			if child.Span().Start() < previous {
				t.Fatal("unordered children")
			}
			previous = child.Span().Start()
		}
		p := n.Properties()
		var values []*parser.Value
		if p.List != nil {
			values = append(values, p.List.Prefix)
		}
		if p.Code != nil {
			values = append(values, p.Code.Info)
		}
		if p.Link != nil {
			values = append(values, &p.Link.Label, p.Link.ReferenceLabel, &p.Link.Destination, p.Link.Title)
		}
		for _, v := range values {
			if v != nil {
				segments(v.Segments, owner)
			}
		}
		return true
	})
	refs := doc.References()
	previous := 0
	for _, ref := range refs.Definitions {
		if ref == nil {
			t.Fatal("nil reference definition")
		}
		p := ref.Properties()
		checkParseFuzzSpan(t, source, p.Envelope, previous, source.Len())
		previous = p.Envelope.Start()
		markers(p.Markers, p.Envelope)
		segments(p.Label.Segments, p.Envelope)
		segments(p.Destination.Segments, p.Envelope)
		if p.Title != nil {
			segments(p.Title.Segments, p.Envelope)
		}
	}
	for _, i := range refs.Lookup {
		if i < 0 || i >= len(refs.Definitions) {
			t.Fatal("reference lookup index outside inventory")
		}
	}
}

// fuzzLimitOutcome verifies explicit exhaustion before provenance-only oracles
// skip unavailable syntax. FuzzParse independently validates every partial span.
func fuzzLimitOutcome(t *testing.T, r parser.ParseResult) bool {
	t.Helper()
	if r.Limit == nil {
		return false
	}
	if r.Status != parser.Incomplete || r.AST != parser.Partial || r.Interruption != parser.LimitExceededReason || r.Failure != nil || r.Limit.Observed <= r.Limit.Allowed || r.Limit.Allowed <= 0 || len(r.Uncertainty) != 1 || r.Uncertainty[0].Reason != parser.LimitExceededReason || r.Uncertainty[0].Region.Start() != 0 || r.Uncertainty[0].Region.End() != r.Source.Len() {
		t.Fatal("inexplicit fuzz limit outcome", r)
	}
	return true
}
