package parser_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

// The retained fixtures are authored from original source spellings, not dumps
// of Parse output. Ranges use LF byte offsets; the test expands them independently
// for CRLF/CR. Content ranges permit finer producer splits for transformations.
type consumerRange struct {
	Start, End int
	Raw        string
}

func (r *consumerRange) UnmarshalJSON(b []byte) error {
	var a []json.RawMessage
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	if len(a) != 3 {
		return fmt.Errorf("range needs start, end, spelling")
	}
	if err := json.Unmarshal(a[0], &r.Start); err != nil {
		return err
	}
	if err := json.Unmarshal(a[1], &r.End); err != nil {
		return err
	}
	return json.Unmarshal(a[2], &r.Raw)
}

type consumerMarker struct {
	Role string
	Span consumerRange
}
type consumerNode struct {
	Kind                                    parser.Kind
	Span                                    consumerRange
	Content                                 []consumerRange
	Markers                                 []consumerMarker
	Children                                []consumerNode
	Literal                                 *string
	Style, Info, Origin, Destination, Title string
	Values                                  map[string][]consumerRange
}
type consumerFixture struct {
	Source     string
	Nodes      []consumerNode
	References []struct {
		Span                      consumerRange
		Label, Destination, Title string
		Values                    map[string][]consumerRange
		Markers                   []consumerMarker
	}
}

func TestSourceConsumerMixedDocuments(t *testing.T) {
	for _, name := range []string{"mixed", "references"} {
		data, err := os.ReadFile("testdata/source_consumers/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var fixture consumerFixture
		if err = json.Unmarshal(data, &fixture); err != nil {
			t.Fatal(err)
		}
		for _, ending := range []string{"\n", "\r\n", "\r"} {
			for _, terminal := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%q/final=%t", name, ending, terminal), func(t *testing.T) {
					input := strings.ReplaceAll(fixture.Source, "\n", ending)
					if terminal {
						input += ending
					}
					caller := []byte(input)
					source := parser.NewSource(caller)
					result := parser.Parse(context.Background(), source)
					checkStagedTextOutcome(t, result, true)
					if string(caller) != input || string(source.Bytes()) != input || string(result.Source.Bytes()) != input {
						t.Fatal("Parse changed original bytes")
					}
					// Source acquired its own bytes before the caller reuses its buffer.
					caller[0] = '!'
					copy := source.Bytes()
					copy[0] = '!'
					offset := func(i int) int { return i + strings.Count(fixture.Source[:i], "\n")*(len(ending)-1) }
					bounds := func(r consumerRange) byteRange { return byteRange{offset(r.Start), offset(r.End)} }
					checkSpan := func(got parser.Span, w consumerRange) {
						t.Helper()
						want := bounds(w)
						raw, err := source.Text(got)
						if err != nil || got.Start() != want.start || got.End() != want.end || raw != strings.ReplaceAll(w.Raw, "\n", ending) {
							t.Fatalf("span [%d,%d) %q: want [%d,%d) %q (%v)", got.Start(), got.End(), raw, want.start, want.end, w.Raw, err)
						}
					}
					checkOwned := func(s, owner parser.Span) {
						t.Helper()
						if _, err := source.Text(s); err != nil || s.Start() < owner.Start() || s.End() > owner.End() {
							t.Fatalf("foreign/outside owner span %+v within %+v: %v", s, owner, err)
						}
					}
					var segments func([]parser.Segment, parser.Span)
					segments = func(parts []parser.Segment, owner parser.Span) {
						t.Helper()
						previous := owner.Start()
						for _, part := range parts {
							if !part.Source.Present() {
								if part.Transformation != parser.Synthetic {
									t.Fatal("absent nonsynthetic provenance", part)
								}
								continue
							}
							checkOwned(part.Source, owner)
							if part.Source.Start() < previous {
								t.Fatal("unordered contributions")
							}
							previous = part.Source.Start()
							raw, _ := source.Text(part.Source)
							if part.Transformation == parser.Exact && part.Semantic.Present && part.Semantic.Value != raw {
								t.Fatal("exact spelling changed", part)
							}
							if part.Transformation == parser.PartialTab && (raw != "\t" || !part.Semantic.Present || len(part.Semantic.Value) == 0 || strings.Trim(part.Semantic.Value, " ") != "") {
								t.Fatal("invalid residual tab", part)
							}
						}
					}

					markers := func(got []parser.Marker, want []consumerMarker, owner parser.Span) {
						t.Helper()
						if len(got) != len(want) {
							t.Fatalf("marker count %d want %d at [%d,%d)", len(got), len(want), owner.Start(), owner.End())
						}
						for i, m := range got {
							checkOwned(m.Source, owner)
							if m.Role != want[i].Role {
								t.Fatalf("marker %s want %s", m.Role, want[i].Role)
							}
							checkSpan(m.Source, want[i].Span)
						}
					}
					checkPayload := func(actual []parser.Segment, want []consumerRange) {
						t.Helper()
						p := 0
						at := 0
						for _, r := range want {
							br := bounds(r)
							if input[br.start:br.end] != strings.ReplaceAll(r.Raw, "\n", ending) {
								t.Fatal("fixture content spelling disagrees with byte offsets", r)
							}
							at = br.start
							if br.start == br.end {
								if p >= len(actual) || actual[p].Source.Start() != at || actual[p].Source.End() != at {
									t.Fatal("missing known-empty source contribution", r)
								}
								p++
							}
							for p < len(actual) && at < br.end {
								part := actual[p]
								if part.Source.Start() != at || part.Source.End() > br.end {
									t.Fatalf("%s content %+v want %+v", "payload", actual, want)
								}
								at = part.Source.End()
								p++
							}
							if at != br.end {
								t.Fatalf("missing content %q", r.Raw)
							}
						}
						if p != len(actual) {
							t.Fatalf("extra %s content %+v, want %+v", "payload", actual, want)
						}
					}

					value := func(v parser.Value, owner parser.Span, want []consumerRange) {
						t.Helper()
						segments(v.Segments, owner)
						if want != nil {
							var backed []parser.Segment
							for _, part := range v.Segments {
								if part.Source.Present() {
									backed = append(backed, part)
								}
							}
							checkPayload(backed, want)
						}
					}

					var visit func(*parser.Node, consumerNode, parser.Span, bool)
					visit = func(n *parser.Node, w consumerNode, parent parser.Span, last bool) {
						t.Helper()
						if n.Kind() != w.Kind {
							t.Fatalf("kind %s want %s at %q", n.Kind(), w.Kind, w.Span.Raw)
						}
						checkOwned(n.Span(), parent)
						envelope := w.Span
						if terminal && last && n.Kind() == parser.Paragraph && envelope.End == len(fixture.Source) {
							envelope.End++
							envelope.Raw += "\n" /* offset handled below for virtual LF */
						}
						if envelope.End > len(fixture.Source) {
							base := bounds(w.Span)
							if n.Span().Start() != base.start || n.Span().End() != base.end+len(ending) {
								t.Fatal("terminal envelope", n.Span())
							}
						} else {
							checkSpan(n.Span(), envelope)
						}
						markers(n.Markers(), w.Markers, n.Span())
						segments(n.Content(), n.Span())
						actual := n.Content()
						checkPayload(actual, w.Content)
						props := n.Properties()
						if w.Literal != nil && props.Literal != parser.Some(*w.Literal) {
							t.Fatalf("literal %+v want %q", props.Literal, *w.Literal)
						}
						if w.Literal == nil && props.Literal.Present {
							t.Fatal("invented literal", props.Literal)
						}
						if props.Code != nil {
							if props.Code.Style != w.Style {
								t.Fatal("code style", props.Code)
							}
							if props.Code.Info != nil {
								value(*props.Code.Info, n.Span(), w.Values["info"])
								if props.Code.Info.Semantic != parser.Some(w.Info) {
									t.Fatal("fence info", props.Code.Info)
								}
							}
						}
						if props.Heading != nil && props.Heading.Style != w.Style {
							t.Fatal("heading style")
						}
						if props.List != nil && props.List.Prefix != nil {
							value(*props.List.Prefix, n.Span(), w.Values["prefix"])
						}
						if props.Link != nil {
							link := props.Link
							value(link.Label, n.Span(), w.Values["label"])
							value(link.Destination, n.Span(), w.Values["destination"])
							if link.ReferenceLabel != nil {
								value(*link.ReferenceLabel, n.Span(), w.Values["reference-label"])
							}
							if link.Title != nil {
								value(*link.Title, n.Span(), w.Values["title"])
							}
							if link.Origin != w.Origin || link.Destination.Semantic != parser.Some(w.Destination) || link.Resolution != parser.Available {
								t.Fatalf("link properties %+v want %+v", link, w)
							}
							if w.Title != "" && (link.Title == nil || link.Title.Semantic != parser.Some(w.Title)) {
								t.Fatal("effective title")
							}
							checkLinkProvenance(t, source, n, result.Document.Root())
						}
						if n.Kind() == parser.CodeSpan {
							var ranges []byteRange
							for _, r := range w.Content {
								ranges = append(ranges, bounds(r))
							}
							checkInlineNodes(t, source, []*parser.Node{n}, []inlineWant{codeWant(n.Span().Start(), bounds(w.Markers[0].Span).end, bounds(w.Markers[1].Span).start, n.Span().End(), *w.Literal, ranges...)})
						}
						if n.Kind() == parser.Text {
							var ranges []byteRange
							for _, r := range w.Content {
								ranges = append(ranges, bounds(r))
							}
							assertDecodedParts(t, source, n.Content(), ranges, entityOracle(t))
						}
						children := n.Children()
						if len(children) != len(w.Children) {
							t.Fatalf("%s child count %d want %d", n.Kind(), len(children), len(w.Children))
						}
						for i, c := range children {
							visit(c, w.Children[i], n.Span(), last && i == len(children)-1)
						}
						// Mutate every detached mutable layer that exposes source contributions.
						for i := range actual {
							actual[i].Semantic = parser.Some("mutated")
						}
						if m := n.Markers(); len(m) > 0 {
							m[0].Role = "mutated"
						}
						if props.Code != nil && props.Code.Info != nil {
							props.Code.Info.Segments = nil
						}
						if props.List != nil && props.List.Prefix != nil {
							props.List.Prefix.Segments = nil
						}
						if props.Link != nil {
							props.Link.Label.Segments = nil
							props.Link.Destination.Segments = nil
							if props.Link.ReferenceLabel != nil {
								props.Link.ReferenceLabel.Segments = nil
							}
							if props.Link.Title != nil {
								props.Link.Title.Segments = nil
							}
						}
						*n = parser.Node{}
					}
					root := result.Document.Root()
					if root.Kind() != parser.DocumentKind || root.Span().Start() != 0 || root.Span().End() != len(input) || len(root.Children()) != len(fixture.Nodes) || len(root.Content()) != 0 || len(root.Markers()) != 0 || !reflect.DeepEqual(root.Properties(), parser.Properties{}) {
						t.Fatal("document envelope/hierarchy", root)
					}

					if name == "mixed" {
						// These are diagnostic scalar columns of actual syntax contributions, after
						// multibyte/combining characters and stripped structural prefixes.
						index := parser.NewLineIndex(source)
						checkPosition := func(span parser.Span, start, end parser.Position) {
							t.Helper()
							for _, point := range []struct {
								offset int
								want   parser.Position
							}{{span.Start(), start}, {span.End(), end}} {
								got, err := index.Position(point.offset)
								if err != nil || got != point.want {
									t.Fatalf("syntax position %d: %+v want %+v (%v)", point.offset, got, point.want, err)
								}
								back, err := index.Offset(point.want)
								if err != nil || back != point.offset {
									t.Fatal("syntax reverse position", back, err)
								}
							}
						}
						escaped := false
						tail := false
						partial := false
						parser.Walk(root, func(n *parser.Node) bool {
							for _, part := range n.Content() {
								raw, _ := source.Text(part.Source)
								switch {
								case n.Kind() == parser.Text && part.Transformation == parser.Escape:
									checkPosition(part.Source, parser.Position{Line: 1, Column: 7}, parser.Position{Line: 1, Column: 9})
									escaped = true
								case n.Kind() == parser.CodeSpan && raw == "tail":
									checkPosition(part.Source, parser.Position{Line: 5, Column: 5}, parser.Position{Line: 5, Column: 9})
									tail = true
								case n.Kind() == parser.CodeBlock && part.Transformation == parser.PartialTab:
									checkPosition(part.Source, parser.Position{Line: 11, Column: 3}, parser.Position{Line: 11, Column: 4})
									partial = true
								}
							}
							return true
						})
						if !escaped || !tail || !partial {
							t.Fatal("syntax-coordinate witnesses missing")
						}
					}

					for i, n := range root.Children() {
						visit(n, fixture.Nodes[i], root.Span(), i == len(fixture.Nodes)-1)
					}
					refs := result.Document.References()
					if len(refs.Definitions) != len(fixture.References) {
						t.Fatal("reference count", refs)
					}
					for i, ref := range refs.Definitions {
						p := ref.Properties()
						w := fixture.References[i]
						if terminal && w.Span.End == len(fixture.Source) {
							br := bounds(w.Span)
							if p.Envelope.Start() != br.start || p.Envelope.End() != br.end+len(ending) {
								t.Fatal("reference terminal envelope")
							}
						} else {
							checkSpan(p.Envelope, w.Span)
						}
						markers(p.Markers, w.Markers, p.Envelope)
						value(p.Label, p.Envelope, w.Values["label"])
						value(p.Destination, p.Envelope, w.Values["destination"])
						if p.Title != nil {
							value(*p.Title, p.Envelope, w.Values["title"])
						}
						if p.NormalizedLabel != parser.Some("id") {
							t.Fatal("reference normalization", p.NormalizedLabel)
						}
						if p.Label.Semantic != parser.Some(w.Label) || p.Destination.Semantic != parser.Some(w.Destination) || p.Title == nil || p.Title.Semantic != parser.Some(w.Title) {
							t.Fatal("reference values", p, w)
						}
						p.Label.Segments = nil
						p.Destination.Segments = nil
						p.Title.Segments = nil
						*ref = parser.ReferenceDefinition{}
					}
					if name == "references" {
						if !reflect.DeepEqual(refs.Lookup, map[string]int{"id": 0}) {
							t.Fatal("duplicate first selection", refs.Lookup)
						}
					} else if len(refs.Lookup) != 0 {
						t.Fatal("opaque fake definitions leaked")
					}
					clear(refs.Lookup)
					if !reflect.DeepEqual(result, parser.Parse(context.Background(), source)) || string(source.Bytes()) != input {
						t.Fatal("detached mutation/reparse changed model or source")
					}
				})
			}
		}
	}
}

// Empty and definition-only documents establish the same affirmative consumer
// handoff even when there are no AST children; references remain independent.
func TestSourceConsumerCompletionControls(t *testing.T) {
	for _, input := range []string{"", " \t\r\n", `[id]: /target "title"`} {
		t.Run(fmt.Sprintf("%q", input), func(t *testing.T) {
			r := parser.Parse(context.Background(), parser.NewSource([]byte(input)))
			checkStagedTextOutcome(t, r, true)
			if len(r.Document.Root().Children()) != 0 || r.Document.Root().Span().Start() != 0 || r.Document.Root().Span().End() != len(input) {
				t.Fatal("empty tree envelope")
			}
			refs := r.Document.References()
			want := 0
			if strings.HasPrefix(input, "[id]") {
				want = 1
			}
			if len(refs.Definitions) != want || len(refs.Lookup) != want {
				t.Fatal("reference inventory", refs)
			}
		})
	}
}
