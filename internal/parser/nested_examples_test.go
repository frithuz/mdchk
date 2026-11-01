package parser

import (
	"context"
	"errors"
	"fmt"
	"math/bits"
	"reflect"
	"strings"
	"testing"
	"time"
)

const md070Example = "<!-- mdchk-example -->\n```md\n```go\nx\n```\n<!-- /mdchk-example -->"

func TestNestedExamplesManyIndependentPairsWithinSourceBound(t *testing.T) {
	const count = 6000
	for _, gfm := range []bool{false, true} {
		for _, violating := range []bool{false, true} {
			t.Run(fmt.Sprintf("gfm-%v/violating-%v", gfm, violating), func(t *testing.T) {
				unit := "<!-- mdchk-example -->\n```md\nx\n```\n<!-- /mdchk-example -->\n\n"
				if violating {
					unit = md070Example + "\n\n"
				}
				source := NewSource([]byte(strings.Repeat(unit, count)))
				// Source-derived envelope, not an observed remaining allowance.
				// Each unit has <=66 bytes, <=7 lines and exactly3 opaque original
				// children. Linear buckets: original parse/index12288, model4096,
				// eligibility8192, prepare/payload parse/compare8192 work per pair.
				// These are conservative source bounds for this fixed shallow shape:
				//   - Parse/index:128*bytes +256*lines +256*(nodes+1) +256*count
				//     <=11264*count+512, within the12288 bucket and fixed reserve.
				//     This covers UTF-8/index scans, HTML/fence construction, fixed
				//     dispatch/freezing and GFM filtering; no other syntax is used.
				//   - Model: four line-sized arrays,19 cursor units/line,2*bytes
				//     classification,32 units/node validation, and doubling vectors
				//     fit4096/pair. Reserved capacities sum to<4*final length and
				//     copied full prefixes to<2*final length, apart from constants.
				//   - Eligibility: two form-2 comments/pair, each<=24 logical bytes
				//     and<=2 parts. Lexer scratch reserves<=202W each;2048W/comment
				//     covers its full raw scan, leaving4096/pair for node/candidate
				//     visits and geometric storage within the8192 bucket.
				//   - Payload:<=4 interior lines,<=8 derived bytes,<=4 mappings and
				//     <=32 reserved buffer bytes. Actual target walks cost<2304W:
				//     two root+CodeBlock walks, or one root+Paragraph+Text walk.
				//     The remaining5888 covers short-body parsing, boundaries,
				//     provenance, growth and publication within8192/pair.
				// Binary physical searches cost at most d+3 iterations; opening
				// association at most d+17W. Both tables are ascending for these
				// flat fixtures (block append order and independent pair closure).
				// Typed pdqsort's increasing-hint path takes<=count+11 comparisons
				// per table, charged4W/opening and1W/pair comparison. The larger
				// 256*count*(d+1) bucket covers sorts and all indexed searches;
				// 65536 covers roots, terminal probes and growth rounding.
				// Conversely, the old restart-at-root search visits at least k
				// distinct CodeBlocks on pair k:16*sum(1..count) work just for
				// association. The supplied whole-operation ceiling is below that
				// lower bound. No budget counter or measured residual is read.
				depth := int64(bits.Len(uint(count)))
				workBound := int64(32768*count) + 256*count*(depth+1) + 65536
				oldLookupLowerBound := int64(16 * count * (count + 1) / 2)
				if workBound >= oldLookupLowerBound {
					t.Fatal("scaling ceiling no longer excludes the former repeated scan")
				}
				limits := DefaultLimits()
				limits.WorkUnits = workBound
				// These explicit node/time ceilings isolate the work regression;
				// this is not a claim of whole-document default-profile completion.
				limits.DocumentNodes = 8192*count + 65536
				limits.TotalNodes = limits.DocumentNodes
				limits.OperationTime = 5 * time.Minute
				budget, err := NewOperationBudget(limits)
				if err != nil {
					t.Fatal(err)
				}
				options := ParseOptions{GFM: gfm}
				parsed := ParseWithOptions(context.Background(), source, budget, options)
				if parsed.Status != Complete || parsed.Document == nil {
					t.Fatal(parsed)
				}
				d := parsed.Document
				if len(d.root.spec.Children) != 3*count {
					t.Fatal("actual original owners", len(d.root.spec.Children))
				}
				for i := 0; i < count; i++ {
					n := d.root.spec.Children[3*i+1]
					if n.Kind() != CodeBlock || len(n.spec.Markers) == 0 || n.spec.Markers[0].Source.start != i*len(unit)+23 || n.spec.Markers[0].Source.SourceToken() != source.Token() {
						t.Fatal("actual original opening association", i, n.Kind())
					}
				}
				var index LineIndex
				err = nestedExampleLocked(context.Background(), budget, LineIndex{}, func(w *parserContext, _ LineIndex) error { index = newLineIndex(w, source); return nil })
				if err != nil {
					t.Fatal(err)
				}
				root := d.root
				out, err := d.NestedExamples(context.Background(), budget, index, options)
				want := 0
				if violating {
					want = count
				}
				if err != nil || out.Availability != Available || len(out.Facts) != want || d.root != root || d.commentOperation != budget.identity || d.Source().Token() != source.Token() {
					t.Fatal("complete original-budget inventory", out.Availability, len(out.Facts), err)
				}
				for i, fact := range out.Facts {
					if fact.Kind != "length" || fact.Span.SourceToken() != source.Token() || fact.Span.start != i*len(unit)+23 || fact.Span.end != i*len(unit)+26 {
						t.Fatal(i, fact)
					}
				}
			})
		}
	}
}

func TestNestedExamplesIndexedOriginalOwnerPrecedence(t *testing.T) {
	d, b, index := md014Parse(t, md070Example+"\n\n"+md070Example)
	pre, err := d.NestedExamples(context.Background(), b, index, ParseOptions{})
	if err != nil || len(pre.Facts) != 2 {
		t.Fatal("original positive", pre, err)
	}
	var m nestedExampleModel
	err = nestedExampleLocked(context.Background(), b, index, func(w *parserContext, i LineIndex) error { var e error; m, e = d.nestedExampleModel(w, i); return e })
	if err != nil {
		t.Fatal(err)
	}
	owners := md070Nodes(d, CodeBlock)
	if len(m.openings) != 2 || len(owners) != 2 {
		t.Fatal("actual opening table", len(m.openings), len(owners))
	}
	for i, owner := range owners {
		err = nestedExampleLocked(context.Background(), b, index, func(w *parserContext, _ LineIndex) error {
			got, e := m.originalOpening(w, owner.spec.Markers[0].Source)
			if got != owner {
				t.Fatal("indexed original association", i)
			}
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Equal-offset candidates retain traversal precedence, including the former
	// envelope eligibility check. These are private lookup units borrowing an
	// actual original owner; they do not confer producer parse authority.
	actual := owners[0]
	span := actual.spec.Markers[0].Source
	for _, matchFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("first-envelope-matches-%v", matchFirst), func(t *testing.T) {
			first := *actual
			if !matchFirst {
				first.spec.Envelope.end = span.end - 1
			}
			table := nestedExampleModel{openings: []nestedExampleOpening{{offset: span.start, order: 0, node: &first}, {offset: span.start, order: 1, node: actual}}}
			err := nestedExampleLocked(context.Background(), b, index, func(w *parserContext, _ LineIndex) error {
				got, e := table.originalOpening(w, span)
				want := actual
				if matchFirst {
					want = &first
				}
				if got != want {
					t.Fatal("equal-offset precedence")
				}
				return e
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func md070ActualPayload(t *testing.T) (nestedExamplePayload, *Document, *OperationBudget) {
	t.Helper()
	d, b, index := md014Parse(t, md070Example)
	pre, err := d.NestedExamples(context.Background(), b, index, ParseOptions{})
	if err != nil || pre.Availability != Available || len(pre.Facts) != 1 || pre.Facts[0].Span.start != 23 {
		t.Fatal("production positive", pre, err)
	}
	var model nestedExampleModel
	err = nestedExampleLocked(context.Background(), b, index, func(w *parserContext, i LineIndex) error {
		var e error
		model, e = d.nestedExampleModel(w, i)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	ok, err := model.eligibility(context.Background(), b, index)
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	var payload nestedExamplePayload
	err = nestedExampleLocked(context.Background(), b, index, func(w *parserContext, i LineIndex) error {
		pairs, available, e := model.pairs(w, i)
		if e != nil {
			return e
		}
		if !available || len(pairs) != 1 {
			t.Fatal(pairs, available)
		}
		payload, available, e = model.prepare(w, i, pairs[0])
		if !available {
			t.Fatal("payload unavailable")
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	p := ParseWithOptions(context.Background(), payload.source, b, ParseOptions{})
	if p.Status != Complete {
		t.Fatal(p)
	}
	bad, available, err := payload.compare(context.Background(), b, p.Document)
	if err != nil || !bad || !available {
		t.Fatal("actual mapped payload positive", bad, available, err)
	}
	if payload.opening.SourceToken() != index.source.Token() || payload.source.Token() == index.source.Token() || len(payload.mapping) == 0 || payload.mapping[0].segment.Source.SourceToken() != index.source.Token() {
		t.Fatal("original/derived provenance")
	}
	return payload, p.Document, b
}

func TestNestedExamplesMappedMarkerStaticPositiveReservations(t *testing.T) {
	payload, d, originalBudget := md070ActualPayload(t)
	blocks := md070Nodes(d, CodeBlock)
	if len(blocks) != 1 || len(blocks[0].spec.Markers) != 1 {
		t.Fatal("actual unclosed inner opener", blocks)
	}
	marker := blocks[0].spec.Markers[0].Source
	if marker.start != 0 || marker.end != 3 || marker.SourceToken() != payload.source.Token() {
		t.Fatal(marker)
	}
	// The complete production path above used one original cumulative allowance.
	// This isolated cost unit borrows its actual immutable mapping and marker;
	// it does not invoke a producer or confer original parse/budget authority.
	payload.mapping = payload.mapping[:1]
	known, err := payload.mappedMarker(context.Background(), originalBudget, marker)
	if err != nil || !known {
		t.Fatal("single-contribution positive", known, err)
	}
	for _, delta := range []int64{-1, 0} {
		t.Run(fmt.Sprintf("delta-%d", delta), func(t *testing.T) {
			b, _ := NewOperationBudget(DefaultLimits())
			if err := b.ChargeWork(b.Limits().WorkUnits - 45 - delta); err != nil {
				t.Fatal(err)
			}
			known, err := payload.mappedMarker(context.Background(), b, marker)
			if delta == 0 {
				if err != nil || !known {
					t.Fatal(known, err)
				}
				return
			}
			for range 2 {
				var stop *LimitExceeded
				if !errors.As(err, &stop) || known {
					t.Fatal(known, err)
				}
				known, err = payload.mappedMarker(context.Background(), b, marker)
			}
		})
	}
}

func TestNestedExamplesMappedMarkerProvenanceFaults(t *testing.T) {
	for _, fault := range []string{"no-mapping", "source-absent", "semantic-absent", "mapping-unavailable", "foreign-original", "foreign-derived", "wrong-bytes", "range", "partial-tab"} {
		t.Run(fault, func(t *testing.T) {
			payload, d, b := md070ActualPayload(t)
			blocks := md070Nodes(d, CodeBlock)
			if len(blocks) != 1 {
				t.Fatal(blocks)
			}
			marker := blocks[0].spec.Markers[0].Source
			known, err := payload.mappedMarker(context.Background(), b, marker)
			if err != nil || !known {
				t.Fatal("positive before mutation", known, err)
			}
			switch fault {
			case "no-mapping":
				payload.mapping = nil
			case "source-absent":
				payload.mapping[0].segment.Source = Span{}
			case "semantic-absent":
				payload.mapping[0].segment.Semantic = Optional[string]{}
			case "mapping-unavailable":
				payload.mapping[0].segment.Transformation = MappingUnavailable
			case "foreign-original":
				payload.mapping[0].segment.Source.source = NewSource([]byte(md070Example))
			case "foreign-derived":
				marker.source = NewSource([]byte(payload.source.content))
			case "wrong-bytes":
				payload.mapping[0].segment.Source.start++
				payload.mapping[0].segment.Source.end++
			case "range":
				payload.mapping[0].segment.Source.end = len(md070Example) + 1
			case "partial-tab":
				payload.mapping[0].segment.Transformation = PartialTab
			}
			known, err = payload.mappedMarker(context.Background(), b, marker)
			if known {
				t.Fatal("invalid mapping accepted")
			}
			switch fault {
			case "no-mapping", "source-absent", "semantic-absent", "mapping-unavailable":
				if err != nil {
					t.Fatal(err)
				}
			default:
				if !errors.Is(err, ErrModel) {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestNestedExamplesStaticEmptyOriginalReservations(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, delta := range []int64{-1, 0} {
			t.Run(fmt.Sprintf("gfm-%v/delta-%d", gfm, delta), func(t *testing.T) {
				s := NewSource(nil)
				b, _ := NewOperationBudget(DefaultLimits())
				options := ParseOptions{GFM: gfm}
				p := ParseWithOptions(context.Background(), s, b, options)
				if p.Status != Complete {
					t.Fatal(p)
				}
				setup := int64(25)
				if gfm {
					setup = 30
				}
				// Independently frozen: original empty parse 25/30W, producer 3W.
				if err := b.ChargeWork(b.Limits().WorkUnits - setup - 3 - delta); err != nil {
					t.Fatal(err)
				}
				got, err := p.Document.NestedExamples(context.Background(), b, NewLineIndex(s), options)
				if delta == 0 {
					if err != nil || got.Availability != Available || len(got.Facts) != 0 {
						t.Fatal(got, err)
					}
					return
				}
				for range 2 {
					var stop *LimitExceeded
					if !errors.As(err, &stop) || got.Facts != nil {
						t.Fatal(got, err)
					}
					got, err = p.Document.NestedExamples(context.Background(), b, NewLineIndex(s), options)
				}
			})
		}
	}
}

func md070Nodes(d *Document, kind Kind) []*Node {
	var out []*Node
	stack := []*Node{d.root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Kind() == kind {
			out = append(out, n)
		}
		for i := len(n.spec.Children) - 1; i >= 0; i-- {
			stack = append(stack, n.spec.Children[i])
		}
	}
	return out
}

func TestNestedExamplesLateOriginalFactsAndDetached(t *testing.T) {
	for _, fault := range []string{"properties", "info", "info-semantic", "fence-absent", "width-absent", "markers-absent", "marker-absent", "wrong-opening-role", "foreign-marker", "bad-fence", "bad-width", "bad-style", "bad-info", "payload-irrelevant", "unchanged"} {
		t.Run(fault, func(t *testing.T) {
			d, b, index := md014Parse(t, md070Example+"\n\n"+md070Example)
			pre, err := d.NestedExamples(context.Background(), b, index, ParseOptions{})
			if err != nil || pre.Availability != Available || len(pre.Facts) != 2 || pre.Facts[0].Span.start != 23 || pre.Facts[1].Span.start != 89 {
				t.Fatal("positive before fault", pre, err)
			}
			pre.Facts[0] = NestedExampleFact{}
			blocks := md070Nodes(d, CodeBlock)
			if len(blocks) != 2 || blocks[1].spec.Markers[0].Source.start != 89 {
				t.Fatal("actual second opening", blocks)
			}
			n := blocks[1]
			switch fault {
			case "properties":
				n.spec.Properties.Code = nil
			case "info":
				n.spec.Properties.Code.Info = nil
			case "info-semantic":
				n.spec.Properties.Code.Info.Semantic = Optional[string]{}
			case "fence-absent":
				n.spec.Properties.Code.Fence = Optional[byte]{}
			case "width-absent":
				n.spec.Properties.Code.Width = Optional[int]{}
			case "markers-absent":
				n.spec.Markers = nil
			case "marker-absent":
				n.spec.Markers[0].Source = Span{}
			case "wrong-opening-role":
				n.spec.Markers[0].Role = "closing"
			case "foreign-marker":
				foreign := NewSource([]byte(md070Example + "\n\n" + md070Example))
				n.spec.Markers[0].Source.source = foreign
			case "bad-fence":
				n.spec.Properties.Code.Fence = Some(byte('!'))
			case "bad-width":
				n.spec.Properties.Code.Width = Some(2)
			case "bad-style":
				n.spec.Properties.Code.Style = "other"
			case "bad-info":
				n.spec.Properties.Code.Info.Semantic = Some("go")
			case "payload-irrelevant":
				n.spec.Content = nil
				n.spec.Properties.Literal = Optional[string]{}
			}
			got, err := d.NestedExamples(context.Background(), b, index, ParseOptions{})
			switch fault {
			case "payload-irrelevant", "unchanged":
				if err != nil || got.Availability != Available || len(got.Facts) != 2 || got.Facts[0].Span.start != 23 {
					t.Fatal(got, err)
				}
			case "foreign-marker", "bad-fence", "bad-width", "bad-style", "bad-info":
				if !errors.Is(err, ErrModel) || got.Facts != nil {
					t.Fatal(got, err)
				}
			default:
				if err != nil || got.Availability != Unavailable || got.Facts != nil {
					t.Fatal(got, err)
				}
			}
		})
	}
}

func TestNestedExamplesCommentRawAndSuffixFaults(t *testing.T) {
	for _, fault := range []string{"opening", "closing", "raw-before", "raw-after", "suffix-prefix", "other-container-prefix"} {
		t.Run(fault, func(t *testing.T) {
			source := md070Example
			if fault == "raw-before" {
				source = "<div>\nx\n</div>\n\n" + source
			}
			if fault == "raw-after" {
				source += "\n\n<div>\nx\n</div>"
			}
			if fault == "suffix-prefix" {
				source = "> " + strings.ReplaceAll(source, "\n", "\n> ") + "\n> ordinary"
			}
			if fault == "other-container-prefix" {
				source += "\n\n> ordinary"
			}
			d, b, index := md014Parse(t, source)
			pre, err := d.NestedExamples(context.Background(), b, index, ParseOptions{})
			if err != nil || pre.Availability != Available || len(pre.Facts) != 1 {
				t.Fatal("positive", pre, err)
			}
			if fault == "opening" || fault == "closing" {
				nodes := md070Nodes(d, HTMLBlock)
				var opener *Node
				for _, n := range nodes {
					if n.commentOpen.present && strings.HasPrefix(source[n.commentOpen.start:], nestedExampleOpen) {
						opener = n
						break
					}
				}
				if opener == nil || !opener.commentClose.present {
					t.Fatal("opening comment absent")
				}
				if fault == "opening" {
					opener.commentOpen = Span{}
				} else {
					opener.commentClose = Span{}
				}
			}
			if strings.HasPrefix(fault, "raw-") {
				var html *Node
				for _, n := range md070Nodes(d, HTMLBlock) {
					if n.spec.Properties.HTML.BlockForm.Value != 2 {
						html = n
						break
					}
				}
				if html == nil || len(html.spec.Content) == 0 {
					t.Fatal("raw owner absent")
				}
				work := &parserContext{Context: context.Background(), budget: &parseBudget{operation: b}}
				_, _, ok, e := conflictHTMLRaw(work, index.source, html, conflictRawState{})
				if e != nil || !ok {
					t.Fatal("raw positive", e, ok)
				}
				html.spec.Content[0].Transformation = MappingUnavailable
				_, _, ok, e = conflictHTMLRaw(work, index.source, html, conflictRawState{})
				if e != nil || ok {
					t.Fatal("raw fault unreached", e, ok)
				}
			}
			if strings.HasSuffix(fault, "prefix") {
				quotes := md070Nodes(d, BlockQuote)
				if len(quotes) != 1 || len(quotes[0].spec.Markers) == 0 {
					t.Fatal("quote absent")
				}
				q := quotes[0]
				last := len(q.spec.Markers) - 1
				if q.spec.Markers[last].Role != "quote-prefix" {
					t.Fatal(q.spec.Markers)
				}
				q.spec.Markers = q.spec.Markers[:last]
			}
			got, err := d.NestedExamples(context.Background(), b, index, ParseOptions{})
			if err != nil {
				t.Fatal(got, err)
			}
			if fault == "raw-after" || fault == "other-container-prefix" {
				if got.Availability != Available || len(got.Facts) != 1 {
					t.Fatal(got)
				}
			} else if got.Availability != Unavailable || got.Facts != nil {
				t.Fatal(got)
			}
		})
	}
}

func TestNestedExamplesDerivedRawUnknownAndSourceMap(t *testing.T) {
	source := "<!-- mdchk-example -->\n```md\n<div>\nx\n</div>\n\n```go\nx\n```\n<!-- /mdchk-example -->"
	d, b, index := md014Parse(t, source)
	pre, err := d.NestedExamples(context.Background(), b, index, ParseOptions{})
	if err != nil || len(pre.Facts) != 1 {
		t.Fatal("positive", pre, err)
	}
	var model nestedExampleModel
	err = nestedExampleLocked(context.Background(), b, index, func(w *parserContext, i LineIndex) error {
		var e error
		model, e = d.nestedExampleModel(w, i)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	ok, err := model.eligibility(context.Background(), b, index)
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	var payload nestedExamplePayload
	err = nestedExampleLocked(context.Background(), b, index, func(w *parserContext, i LineIndex) error {
		pairs, available, e := model.pairs(w, i)
		if e != nil {
			return e
		}
		if !available || len(pairs) != 1 {
			t.Fatal(pairs, available)
		}
		payload, available, e = model.prepare(w, i, pairs[0])
		if !available {
			t.Fatal("payload unavailable")
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed := ParseWithOptions(context.Background(), payload.source, b, ParseOptions{})
	if parsed.Status != Complete {
		t.Fatal(parsed)
	}
	bad, available, err := payload.compare(context.Background(), b, parsed.Document)
	if err != nil || !available || !bad {
		t.Fatal("derived positive", bad, available, err)
	}
	html := md070Nodes(parsed.Document, HTMLBlock)
	if len(html) != 1 || len(html[0].spec.Content) == 0 {
		t.Fatal("derived raw owner")
	}
	html[0].spec.Content[0].Transformation = MappingUnavailable
	bad, available, err = payload.compare(context.Background(), b, parsed.Document)
	if err != nil || available || bad {
		t.Fatal("derived unknown must not publish maximum", bad, available, err)
	}
	// Original source and tree semantics remain independent of derived mutation.
	again, err := d.NestedExamples(context.Background(), b, index, ParseOptions{})
	if err != nil || !reflect.DeepEqual(pre, again) || index.source.content != source {
		t.Fatal(again, err)
	}
	for _, part := range payload.mapping {
		if part.segment.Source.SourceToken() != index.source.Token() {
			t.Fatal("derived provenance leaked", part)
		}
	}
}

func TestNestedExamplesOriginalPrivateAuthority(t *testing.T) {
	d, b, index := md014Parse(t, "```sh\n$ pwd\n```\n")
	public, _ := NewDocument(d.Root(), d.References())
	foreign, _ := NewOperationBudget(DefaultLimits())
	for _, tc := range []struct {
		d *Document
		b *OperationBudget
		i LineIndex
		o ParseOptions
	}{
		{nil, b, index, ParseOptions{}}, {public, b, index, ParseOptions{}}, {d, nil, index, ParseOptions{}}, {d, foreign, index, ParseOptions{}}, {d, b, NewLineIndex(NewSource([]byte("```sh\n$ pwd\n```\n"))), ParseOptions{}}, {d, b, index, ParseOptions{GFM: true}},
	} {
		got, err := tc.d.NestedExamples(context.Background(), tc.b, tc.i, tc.o)
		if !errors.Is(err, ErrModel) || got.Facts != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Deliberately absent caller.
	if got, err := d.NestedExamples(nil, b, index, ParseOptions{}); !errors.Is(err, ErrModel) || got.Facts != nil {
		t.Fatal(got, err)
	}
	for _, profile := range []parserProfile{tablesOnlyProfile, tasksOnlyProfile, strikeOnlyProfile, bareOnlyProfile, filterOnlyProfile} {
		for _, fullFlags := range []bool{false, true} {
			t.Run(fmt.Sprintf("profile-%d/full-flags-%v", profile, fullFlags), func(t *testing.T) {
				s := NewSource([]byte("```sh\n$ pwd\n```\n"))
				budget, _ := NewOperationBudget(DefaultLimits())
				p := parseWithProfile(context.Background(), s, budget, profile)
				if p.Status != Complete {
					t.Fatal(p)
				}
				if fullFlags {
					p.Document.parsedOptions.GFM = true
				}
				got, err := p.Document.NestedExamples(context.Background(), budget, NewLineIndex(s), p.Document.parsedOptions)
				if !errors.Is(err, ErrModel) || got.Facts != nil {
					t.Fatal(got, err)
				}
			})
		}
	}
	d.parsedComplete = false
	if got, err := d.NestedExamples(context.Background(), b, index, ParseOptions{}); !errors.Is(err, ErrModel) || got.Facts != nil {
		t.Fatal(got, err)
	}
	source := NewSource([]byte("```sh\n$ pwd\n```\n"))
	budget, _ := NewOperationBudget(DefaultLimits())
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	partial := ParseWithOptions(cancelled, source, budget, ParseOptions{})
	if partial.Status == Complete {
		t.Fatal("genuine interrupted parse completed")
	}
	if got, err := partial.Document.NestedExamples(context.Background(), budget, NewLineIndex(source), ParseOptions{}); !errors.Is(err, ErrModel) || got.Facts != nil {
		t.Fatal(got, err)
	}
}
