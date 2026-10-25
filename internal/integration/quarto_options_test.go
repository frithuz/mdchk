package integration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

func optionsFixture(t *testing.T, raw string) (*Scope, Document, parser.Value) {
	t.Helper()
	s, doc, op := contractScope(t, raw, Settings{QuartoEnabled: true})
	parsed := parser.ParseWithOptions(context.Background(), doc.Source(), op.Budget(), parser.ParseOptions{})
	if parsed.Document == nil {
		t.Fatal(parsed)
	}
	var body parser.Value
	var walk func(*parser.Node)
	walk = func(n *parser.Node) {
		if n.Kind() == parser.CodeBlock {
			p := n.Properties()
			body = parser.Value{Segments: n.Content(), Semantic: p.Literal}
			return
		}
		for _, child := range n.Children() {
			walk(child)
		}
	}
	walk(parsed.Document.Root())
	if !body.Semantic.Present {
		t.Fatal("missing fixture fence")
	}
	return s, doc, body
}
func TestQuartoOptionSafeYAMLAndInitialBoundary(t *testing.T) {
	cases := []struct {
		name, options string
		kind          LabelKind
		value         string
		issues        bool
		declared      int
	}{
		{"none", "print(1)\n#| label: late\n", LabelAbsent, "", false, 0},
		{"blank-first", "\n#| label: late\n", LabelAbsent, "", false, 0},
		{"plain", "#| label: simple\nprint(1)\n#| label: late\n", LabelString, "simple", false, 1},
		{"quoted-number", "#| label: '001'\n", LabelString, "001", false, 1},
		{"empty", "#| label: ''\n", LabelString, "", false, 1},
		{"unicode-escape", "#| label: \"雪\\u2003図\"\n", LabelString, "雪\u2003図", false, 1},
		{"number", "#| label: 1\n", LabelNonString, "", false, 0},
		{"exponent", "#| label: 1e3\n", LabelNonString, "", false, 0},
		{"null", "#| label: null\n", LabelNonString, "", false, 0},
		{"bool", "#| label: false\n", LabelNonString, "", false, 0},
		{"literal", "#| label: |\n#|   first\n#|   second\n", LabelString, "first\nsecond\n", false, 1},
		{"folded", "#| label: >-\n#|   first\n#|   second\n", LabelString, "first second", false, 1},
		{"empty-block", "#| label: |-\n", LabelString, "", false, 1},
		{"alias", "#| base: &label 'old label'\n#| label: *label\n", LabelString, "old label", false, 1},
		{"nested-safe", "#| unrelated: {x: [1, true, null], y: {nested: value}}\n#| label: okay\n", LabelString, "okay", false, 1},
		{"duplicates", "#| label: 'old label'\n#| label: new\n", LabelAmbiguous, "", true, 2},
		{"unrelated-duplicates", "#| echo: true\n#| echo: false\n#| label: okay\n", LabelUnknown, "", true, 1},
		{"nested-duplicates", "#| foo: {x: 1, x: 2}\n#| label: okay\n", LabelUnknown, "", true, 1},
		{"core-string", "#| label: !!str 123\n", LabelString, "123", false, 1},
		{"core-bool", "#| label: !!bool false\n", LabelNonString, "", false, 0},
		{"tag", "#| label: !execute something\n", LabelUnknown, "", true, 0},
		{"merge", "#| base: &a {x: 1}\n#| <<: *a\n#| label: okay\n", LabelUnknown, "", true, 1},
		{"cycle", "#| value: &a [*a]\n#| label: okay\n", LabelUnknown, "", true, 1},
		{"unresolved", "#| label: *missing\n", LabelUnknown, "", true, 0},
		{"sequence-root", "#| - label\n", LabelUnknown, "", true, 0},
		{"malformed", "#| label: [unclosed\n", LabelUnknown, "", true, 0},
		{"multidoc", "#| ---\n#| label: x\n#| ---\n#| label: y\n", LabelUnknown, "", true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, doc, body := optionsFixture(t, "```{r}\n"+tc.options+"```\n")
			got, err := decodeQuartoOptions(context.Background(), s, doc, body)
			if err != nil || got.label.Kind() != tc.kind || got.label.Text() != tc.value || (len(got.issues) > 0) != tc.issues || len(got.declared) != tc.declared {
				t.Fatalf("got kind=%s value=%q declarations=%d issues=%v err=%v", got.label.Kind(), got.label.Text(), len(got.declared), got.issues, err)
			}
		})
	}
}
func TestQuartoOptionOriginalContainerCRLFLocations(t *testing.T) {
	raw := "雪\r\n\r\n> - ```{python}\r\n>   #| base: &x 'é 図'\r\n>   #| label: *x\r\n>   print(1)\r\n>   ```\r\n"
	s, doc, body := optionsFixture(t, raw)
	got, err := decodeQuartoOptions(context.Background(), s, doc, body)
	if err != nil || got.label.Kind() != LabelString || got.label.Text() != "é 図" {
		t.Fatal(got, err)
	}
	span := got.label.Location().Span()
	start := strings.Index(raw, "*x")
	if span.Start() != start || span.End() != start+2 {
		t.Fatalf("alias span [%d,%d), want [%d,%d)", span.Start(), span.End(), start, start+2)
	}
	spelling, _ := doc.Source().Text(span)
	if spelling != "*x" {
		t.Fatal(spelling)
	}
	raw = "> ```{r}\r\n> #| label: |\r\n> #|   雪\r\n> #|   図\r\n> ```\r\n"
	s, doc, body = optionsFixture(t, raw)
	got, err = decodeQuartoOptions(context.Background(), s, doc, body)
	if err != nil || got.label.Text() != "雪\n図\n" {
		t.Fatal(got, err)
	}
	spelling, _ = doc.Source().Text(got.label.Location().Span())
	if spelling != "|\r\n> #|   雪\r\n> #|   図\r\n" {
		t.Fatalf("wrong original block spelling %q", spelling)
	}
}
func TestQuartoOptionActualStopAndForeignBody(t *testing.T) {
	s, doc, body := optionsFixture(t, "```{r}\n#| label: okay\n```\n")
	foreign := parser.NewSource(doc.Source().Bytes())
	copyBody := body
	copyBody.Segments = append([]parser.Segment(nil), body.Segments...)
	for i := range copyBody.Segments {
		span := copyBody.Segments[i].Source
		copyBody.Segments[i].Source, _ = foreign.Span(span.Start(), span.End())
	}
	if _, err := decodeQuartoOptions(context.Background(), s, doc, copyBody); !errors.Is(err, ErrContract) {
		t.Fatal("foreign source accepted", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := decodeQuartoOptions(cancelled, s, doc, body)
	if !errors.Is(err, context.Canceled) || out.label.Kind() == LabelString {
		t.Fatal("cancelled options became complete", out, err)
	}
	if err := s.operation.Budget().ChargeWork(s.operation.Budget().Limits().WorkUnits + 1); err == nil {
		t.Fatal("quota not exhausted")
	}
	out, err = decodeQuartoOptions(context.Background(), s, doc, body)
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || out.label.Kind() == LabelString {
		t.Fatal("quota became complete", out, err)
	}
}

func TestQuartoOptionForeignSegmentUsesIdentityBeforeBodyWork(t *testing.T) {
	source := parser.NewSource([]byte("{r}" + strings.Repeat("x", 256<<10)))
	limits := parser.DefaultLimits()
	limits.WorkUnits = int64(source.Len()) + 512
	budget, _ := parser.NewOperationBudget(limits)
	root, _ := resource.NewRoot(resource.SuppliedRoot)
	policy, _ := resource.NewSettings(parser.ParseOptions{}, "index.md", "github", "option-owner")
	op, err := resource.NewOperation(context.Background(), budget, root, policy, []resource.Target{{ID: "a.md", Source: source}})
	if err != nil {
		t.Fatal(err)
	}
	view, _ := op.NewView(resource.Original, nil)
	scope, err := NewScope(context.Background(), op, view, Settings{QuartoEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := scope.Document(context.Background(), "a.md")
	if err != nil {
		t.Fatal(err)
	}
	foreign := parser.NewSource(source.Bytes())
	span, _ := foreign.Span(0, foreign.Len())
	foreignText, _ := foreign.Text(span)
	body := parser.Value{Segments: []parser.Segment{{Source: span, Transformation: parser.Exact, Semantic: parser.Some(foreignText)}}, Semantic: parser.Some(foreignText)}
	for range 8 {
		if _, err := decodeQuartoOptions(context.Background(), scope, doc, body); !errors.Is(err, ErrContract) {
			t.Fatal("foreign segment did proportional work or borrowed authority", err)
		}
	}
	headerSpan, _ := source.Span(0, 3)
	location, _ := NewLocation(source, headerSpan)
	decision, err := scope.QuartoHeader(context.Background(), doc, location, false)
	if err != nil || decision.Class() != SupportedCandidate {
		t.Fatal("foreign option checks exhausted owner allowance", decision, err)
	}
}
func TestQuartoOptionStructuredDepthLimit(t *testing.T) {
	raw := "```{r}\n#| x: " + strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65) + "\n```\n"
	s, doc, body := optionsFixture(t, raw)
	_, err := decodeQuartoOptions(context.Background(), s, doc, body)
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || limit.Name != "structured-input-depth" || limit.Allowed != 64 {
		t.Fatal("deep YAML not controlled", err)
	}
}

func TestQuartoOptionPreflightsLexerStorageBeforeMalformedInput(t *testing.T) {
	// No parser-owned cell authority is asserted here: this tests the private
	// decoder's admission boundary with a source-coherent logical body fixture.
	raw := "#| label: [" + strings.Repeat("x", 128) + "\n"
	source := parser.NewSource([]byte(raw))
	limits := parser.DefaultLimits()
	limits.DocumentNodes, limits.TotalNodes = 32, 32
	budget, _ := parser.NewOperationBudget(limits)
	root, _ := resource.NewRoot(resource.SuppliedRoot)
	policy, _ := resource.NewSettings(parser.ParseOptions{}, "index.md", "github", "option-preflight")
	op, err := resource.NewOperation(context.Background(), budget, root, policy, []resource.Target{{ID: "a.md", Source: source}})
	if err != nil {
		t.Fatal(err)
	}
	view, _ := op.NewView(resource.Original, nil)
	scope, err := NewScope(context.Background(), op, view, Settings{QuartoEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	document, err := scope.Document(context.Background(), "a.md")
	if err != nil {
		t.Fatal(err)
	}
	span, _ := source.Span(0, source.Len())
	body := parser.Value{Segments: []parser.Segment{{Source: span, Transformation: parser.Exact, Semantic: parser.Some(raw)}}, Semantic: parser.Some(raw)}
	out, err := decodeQuartoOptions(context.Background(), scope, document, body)
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || limit.Name != "total-nodes" || limit.Allowed != 32 || len(out.issues) != 0 {
		t.Fatal("lexer allocation/malformed uncertainty happened before node admission", out, err)
	}
}

func TestQuartoOptionTagProbeOnlyChangesConfirmedSeparators(t *testing.T) {
	for _, test := range []struct{ name, original, want string }{
		{"quoted", "label: '!!str\tpayload'\n", "label: '!!str\tpayload'\n"},
		{"plain", "other: prefix !!str\tpayload\nlabel: !!str\tokay\n", "other: prefix !!str\tpayload\nlabel: !!str okay\n"},
		{"block", "other: |-\n  !!str\tpayload\nlabel: !!str\tokay\n", "other: |-\n  !!str\tpayload\nlabel: !!str okay\n"},
		{"comment", "other: !!int\t0x10 # !!str\tpayload\nlabel: okay\n", "other: !!int 0x10 # !!str\tpayload\nlabel: okay\n"},
		{"flow", "{other: !!int 0x10, spare: !!str\t'!!int\tpayload', label: !!str \"雪\t!!str\tpayload\\u2003図\"}\n", "{other: !!int 0x10, spare: !!str '!!int\tpayload', label: !!str \"雪\t!!str\tpayload\\u2003図\"  }\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			scope, _, _ := contractScope(t, "probe", Settings{QuartoEnabled: true})
			d := quartoOptionDecoder{scope: scope, ctx: context.Background(), text: test.original, starts: []int{0}}
			for i, c := range test.original {
				if c == '\n' {
					d.starts = append(d.starts, i+1)
				}
			}
			got, err := d.tagSeparatorInput()
			if err != nil || got != test.want {
				t.Fatalf("probe changed payload or missed separator: got %q want %q err=%v", got, test.want, err)
			}
		})
	}
}

func TestQuartoOptionQuotedTabPaddingCounts(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want int
	}{
		{"\"a\tb\"", 1}, {"\"a\t\"", 0}, {"\"a\t  \"", 1}, {"\"a\t\tb\"", 2},
		{"\"a\\\tb\"", 0}, {"\"a\\tb\"", 0}, {"\"a\t\n b\"", 0}, {"\"a\t \n b\"", 0},
		{"\"a\n  \tb\"", 0}, {"\"a\n x\tb\"", 1}, {"\"a\\\n \tb\"", 0}, {"\"a\\u2003\tb\"", 1},
	} {
		scope, _, _ := contractScope(t, "count", Settings{QuartoEnabled: true})
		d := quartoOptionDecoder{scope: scope, ctx: context.Background(), text: test.raw}
		if got := d.quotedTabPadding(0, len(test.raw)); got != test.want {
			t.Fatalf("quoted cursor count %q: got %d want %d", test.raw, got, test.want)
		}
	}
}
