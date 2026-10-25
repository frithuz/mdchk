package metadata

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

func decoded(t *testing.T, raw string) (fixture, Result) {
	t.Helper()
	f := bindFixture(t, raw)
	r, err := Decode(context.Background(), f.b)
	if err != nil {
		t.Fatal(err)
	}
	return f, r
}
func fieldValue(t *testing.T, v Value, key string) Value {
	t.Helper()
	for i := 0; i < v.EntryCount(); i++ {
		e, _ := v.KnownEntry(i)
		if e.Key() == key {
			return e.Value()
		}
	}
	t.Fatalf("missing %q in %s (%d)", key, v.Kind(), v.EntryCount())
	return Value{}
}
func sourceText(t *testing.T, b Binding, span parser.Span) string {
	t.Helper()
	text, err := b.Source().Text(span)
	if err != nil {
		t.Fatal(err)
	}
	if span.SourceToken() != b.Source().Token() {
		t.Fatal("foreign source")
	}
	return text
}
func TestDecodeYAMLNativeSource(t *testing.T) {
	raw := "---\nyes: yes\nquoted: \"true\"\nboolean: true\nnothing: null\nlarge: 0x20000000000001\nfloat: 1.25e+2\nnan: .nan\ninf: -.inf\nstr: !!str 42\nlegacy: 0777\nnested: [{z: 1, a: 2}]\n---\nbody\n"
	f, r := decoded(t, raw)
	if !r.Complete() || r.Root().EntryCount() != 11 {
		t.Fatalf("incomplete %#v problems %d entries %d", r, r.ProblemCount(), r.Root().EntryCount())
	}
	for _, test := range []struct {
		key       string
		kind      Kind
		text, raw string
	}{{"yes", String, "yes", "yes"}, {"quoted", String, "true", "\"true\""}, {"large", Integer, "9007199254740993", "0x20000000000001"}, {"float", Float, "125e0", "1.25e+2"}, {"nan", Float, "nan", ".nan"}, {"inf", Float, "-inf", "-.inf"}, {"str", String, "42", "!!str 42"}, {"legacy", Integer, "777", "0777"}} {
		v := fieldValue(t, r.Root(), test.key)
		text, _ := v.Scalar()
		if v.Kind() != test.kind || text != test.text || sourceText(t, f.b, v.Span()) != test.raw {
			t.Errorf("%s got %s %q %q", test.key, v.Kind(), text, sourceText(t, f.b, v.Span()))
		}
	}
	boolean := fieldValue(t, r.Root(), "boolean")
	if value, ok := boolean.Boolean(); !ok || !value {
		t.Fatal("bool")
	}
	if fieldValue(t, r.Root(), "nothing").Kind() != Null {
		t.Fatal("null")
	}
	nested := fieldValue(t, r.Root(), "nested")
	elem, _ := nested.KnownElement(0)
	if elem.Kind() != Mapping || elem.EntryCount() != 2 {
		t.Fatal("nested mapping")
	}
}
func TestDecodeYAMLSourceRangesAndEligibility(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		bom := ""
		if ending == "\r\n" {
			bom = "\xef\xbb\xbf"
		}
		raw := bom + strings.ReplaceAll("---\nz: \"é#\" # inline\n\"\\u0061\": 2\nblock: | # header\n  # payload\n  λ\n---\nbody\n", "\n", ending)
		f, r := decoded(t, raw)
		if !r.Complete() {
			t.Fatalf("%q incomplete %d", ending, r.ProblemCount())
		}
		e, _ := r.Root().KnownEntry(1)
		if e.Key() != "a" || sourceText(t, f.b, e.KeySpan()) != "\"\\u0061\"" {
			t.Fatal("escaped key", e.Key(), sourceText(t, f.b, e.KeySpan()))
		}
		first, _ := r.Root().KnownEntry(0)
		if sourceText(t, f.b, first.Span()) != "z: \"é#\" # inline"+ending {
			t.Fatalf("entry %q", sourceText(t, f.b, first.Span()))
		}
		block := fieldValue(t, r.Root(), "block")
		if sourceText(t, f.b, block.Span()) != "| # header"+ending+"  # payload"+ending+"  λ"+ending {
			t.Fatalf("block %q", sourceText(t, f.b, block.Span()))
		}
		orders, err := r.Ordering(context.Background(), f.b, OrderOptions{})
		if err != nil || len(orders) != 1 || orders[0].MoveEligibility() != IndependentEntries {
			t.Fatalf("eligibility %v %v", orders, err)
		}
		all, _ := f.b.Source().Span(0, len(raw))
		if sourceText(t, f.b, all) != raw {
			t.Fatal("source changed")
		}
	}
	for _, payload := range []string{"# standalone\nz: 1\na: 2\n", "{z: 1, a: 2}\n", "z: &x 1\na: *x\n"} {
		f, r := decoded(t, "---\n"+payload+"---")
		orders, err := r.Ordering(context.Background(), f.b, OrderOptions{})
		if err != nil || len(orders) != 1 || orders[0].MoveEligibility() != ReportOnly {
			t.Fatalf("report-only %q %v %v", payload, orders, err)
		}
	}
}
func TestDecodeYAMLAliasesAndUnavailable(t *testing.T) {
	f, r := decoded(t, "---\na: &x {nested: [7]}\nb: *x\nc: &x true\nd: *x\n---")
	if !r.Complete() {
		t.Fatal("aliases incomplete", r.ProblemCount())
	}
	b := fieldValue(t, r.Root(), "b")
	if sourceText(t, f.b, b.Span()) != "*x" {
		t.Fatal("alias range")
	}
	child, _ := b.KnownEntry(0)
	if child.Span().Present() || sourceText(t, f.b, child.Value().Span()) != "*x" {
		t.Fatal("alias child range")
	}
	if value, ok := fieldValue(t, r.Root(), "d").Boolean(); !ok || !value {
		t.Fatal("rebound anchor")
	}
	for _, payload := range []string{"title: Good\nbad: !custom value\n", "title: A\n\"title\": B\n", "title: [broken\n", "[1,2]\n", "true: x\n", "title: Good\nbad: !!bool nope\n", "title: Good\nbad: &x [*x]\n", "title: Good\nbad: *missing\n", "title: Good\nbad: {<<: {x: 1}}\n"} {
		f, r := decoded(t, "---\n"+payload+"---")
		if r.Complete() || r.ProblemCount() == 0 {
			t.Fatalf("accepted %q", payload)
		}
		if orders, err := r.Ordering(context.Background(), f.b, OrderOptions{}); err != nil || len(orders) != 0 {
			t.Fatalf("incomplete mapping minted ordering %q: %v %v", payload, orders, err)
		}
		title, err := r.Title(context.Background(), f.b)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(payload, "title: Good") && (!title.Known || title.Text != "Good") {
			t.Fatalf("lost independent title %q %#v", payload, title)
		}
		for i := 0; i < r.ProblemCount(); i++ {
			p, _ := r.KnownProblem(i)
			if !p.Location.Present() || p.Location.SourceToken() != f.b.Source().Token() {
				t.Fatal("foreign problem")
			}
		}
	}
	for _, raw := range []string{"body\n", "---\ntitle: Maybe"} {
		f, r := decoded(t, raw)
		if r.Complete() {
			t.Fatal("unavailable complete")
		}
		title, err := r.Title(context.Background(), f.b)
		if err != nil || title.Known {
			t.Fatal("unavailable title", title, err)
		}
		if orders, err := r.Ordering(context.Background(), f.b, OrderOptions{Recursive: true}); err != nil || len(orders) != 0 {
			t.Fatal("unavailable ordering", orders, err)
		}
	}
	for _, raw := range []string{"---\n---", "+++\n+++"} {
		_, r := decoded(t, raw)
		if !r.Complete() || r.Root().Kind() != Mapping || r.Root().EntryCount() != 0 {
			t.Fatal("empty", raw, r.ProblemCount(), r.Root().Kind())
		}
	}
}
func TestDecodeAdmissionAndYAMLDepth(t *testing.T) {
	for _, payload := range []string{"a: [[[1]]]\n", "a:\n  b:\n    c:\n      d: 1\n"} {
		limits := parser.DefaultLimits()
		limits.SyntaxDepth = 4
		budget, _ := parser.NewOperationBudget(limits)
		f := bindBudgetFixture(t, "---\n"+payload+"---", budget, context.Background())
		_, err := Decode(context.Background(), f.b)
		var quota *parser.LimitExceeded
		if !errors.As(err, &quota) || quota.Name != "syntax-depth" {
			t.Fatalf("missing pre-entry depth %q: %v", payload, err)
		}
	}
	limits := parser.DefaultLimits()
	limits.SyntaxDepth = 4
	budget, _ := parser.NewOperationBudget(limits)
	f := bindBudgetFixture(t, "---\na: [[1]]\n---", budget, context.Background())
	r, err := Decode(context.Background(), f.b)
	if err != nil || !r.Complete() {
		t.Fatal("boundary", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Decode(ctx, f.b)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	limits = parser.DefaultLimits()
	limits.DocumentNodes = 100
	budget, _ = parser.NewOperationBudget(limits)
	f = bindBudgetFixture(t, "---\na: 1\n---", budget, context.Background())
	_, err = Decode(context.Background(), f.b)
	var quota *parser.LimitExceeded
	if !errors.As(err, &quota) || quota.Name != "document-nodes" {
		t.Fatal("storage admission", err)
	}
}
func FuzzDecodeSource(f *testing.F) {
	f.Add("---\ntitle: !!str &x 42\ncopy: *x\n---")
	f.Add("---\ntitle: &x !!str 42\ncopy: *x\n---")
	for _, seed := range []string{"---\n{x: !!int # comment\n, other: x}\n---", "---\ntitle: !!str # comment\nother: x\n---", ""} {
		f.Add(seed)
	}
	for _, seed := range []string{"---\n\u00a0\n---", "---\n{title: !!str, 雪: }\n---", "---\n!!str <<: literal\n---", "---\na: &x old\nb: &saved [*x]\nc: &x new\nd: *saved\n---"} {
		f.Add(seed)
	}
	for _, seed := range []string{"---\ntitle: Hello\n---", "+++\na=1\n+++", "---\na: &x [*x]\n---", "---\n\"\\u0061\": x\r\n---"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 4096 {
			t.Skip()
		}
		limits := parser.DefaultLimits()
		budget, _ := parser.NewOperationBudget(limits)
		source := parser.NewSource([]byte(raw))
		options, _ := parser.NewFrontmatterOptions(true, nil)
		parsed := parser.ParseWithOptions(context.Background(), source, budget, parser.ParseOptions{Frontmatter: options})
		if parsed.Document == nil {
			return
		}
		b, err := Bind(context.Background(), context.Background(), budget, parsed.Document)
		if err != nil {
			return
		}
		r, err := Decode(context.Background(), b)
		if err != nil {
			return
		}
		var walk func(Value)
		walk = func(v Value) {
			if span := v.Span(); span.Present() && (span.SourceToken() != source.Token() || span.Start() < b.Header().Content().Start() || span.End() > b.Header().Content().End()) {
				t.Fatal("foreign value")
			}
			for i := 0; i < v.EntryCount(); i++ {
				e, _ := v.KnownEntry(i)
				walk(e.Value())
			}
			for i := 0; i < v.ElementCount(); i++ {
				e, _ := v.KnownElement(i)
				walk(e)
			}
		}
		walk(r.Root())
	})
}

func TestDecodeExactNumericAndImplicitNull(t *testing.T) {
	f, r := decoded(t, "---\nnegative: -0.0\npositive: +0.0\nempty:\nother: !!map {x: !!int 9}\n---")
	if !r.Complete() {
		t.Fatal("incomplete", r.ProblemCount())
	}
	for key, want := range map[string]string{"negative": "-0e0", "positive": "0e0"} {
		v := fieldValue(t, r.Root(), key)
		text, _ := v.Scalar()
		if v.Kind() != Float || text != want {
			t.Fatal(key, text)
		}
	}
	empty := fieldValue(t, r.Root(), "empty")
	if empty.Kind() != Null || sourceText(t, f.b, empty.Span()) != "" || empty.Span().Start() != strings.Index(f.raw, "empty:")+len("empty:") {
		t.Fatal("implicit null range", empty.Span())
	}
}
func TestDecodeCanonicalControlsAndOriginalStop(t *testing.T) {
	for _, opStop := range []bool{false, true} {
		operation, cancel := context.WithCancel(context.Background())
		f := bindBudgetFixture(t, "---\na: 1\n---", mustBudget(t, parser.DefaultLimits()), operation)
		if opStop {
			cancel()
		} else {
			defer cancel()
		}
		caller, stop := context.WithCancel(context.Background())
		stop()
		_, err := Decode(caller, f.b)
		if !errors.Is(err, context.Canceled) {
			t.Fatal("caller/operation lost", err)
		}
	}
	for _, kind := range []string{"work", "global", "time"} {
		limits := parser.DefaultLimits()
		if kind == "time" {
			limits.OperationTime = 10 * time.Millisecond
		}
		budget := mustBudget(t, limits)
		f := bindBudgetFixture(t, "---\na: 1\n---", budget, context.Background())
		var cause error
		switch kind {
		case "work":
			cause = budget.ChargeWork(limits.WorkUnits + 1)
		case "global":
			cause = budget.ChargeNodes(limits.TotalNodes + 1)
		case "time":
			time.Sleep(15 * time.Millisecond)
			cause = budget.ChargeWork(0)
		}
		var quota *parser.LimitExceeded
		if !errors.As(cause, &quota) {
			t.Fatal("fixture", cause)
		}
		want := *quota
		_, err := Decode(context.Background(), f.b)
		if !containsQuota(err, want) {
			t.Fatal("canonical stop lost", cause, err)
		}
	}
	limits := parser.DefaultLimits()
	limits.DocumentNodes = 6
	budget := mustBudget(t, limits)
	options, _ := parser.NewFrontmatterOptions(true, nil)
	parsed := parser.ParseWithOptions(context.Background(), parser.NewSource([]byte("---\nx: 1\n---\nbody *text*\n")), budget, parser.ParseOptions{Frontmatter: options})
	want := parser.LimitExceeded{Name: "document-nodes", Observed: 7, Allowed: 6}
	if parsed.Limit == nil || *parsed.Limit != want {
		t.Fatal("original fixture", parsed.Limit)
	}
	// Public binding refuses new construction at this stop. This private retained
	// identity drives Decode's no-new-work gate against that exact original parse.
	header, _ := parsed.Document.Frontmatter()
	b := Binding{&binding{source: parsed.Document.Source(), header: header, budget: budget, operation: context.Background(), state: Undecoded}}
	parsed.Limit.Observed = 999
	_, err := Decode(context.Background(), b)
	if !containsQuota(err, want) {
		t.Fatal("original quota replaced", err)
	}
	other := parser.ParseWithOptions(context.Background(), parser.NewSource([]byte("---\n---")), budget, parser.ParseOptions{Frontmatter: options})
	if other.Status != parser.Complete {
		t.Fatal("other document inherited local stop")
	}
	originalGlobal := budget.ChargeWork(limits.WorkUnits + 1)
	var global *parser.LimitExceeded
	errors.As(originalGlobal, &global)
	caller, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	operation, stop := context.WithCancel(context.Background())
	stop()
	b.state.operation = operation
	_, err = Decode(caller, b)
	if !containsQuota(err, want) || global == nil || !containsQuota(err, *global) || !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("joined canonical causes lost", err)
	}
}
func mustBudget(t *testing.T, limits parser.Limits) *parser.OperationBudget {
	t.Helper()
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	return budget
}

func TestDecodeAliasExpansionAndDocumentIndependence(t *testing.T) {
	payload := "a: &a [\"" + strings.Repeat("x", 1000) + "\"]\n"
	limits := parser.DefaultLimits()
	limits.DocumentNodes = 90000
	smallBudget := mustBudget(t, limits)
	small := bindBudgetFixture(t, "---\n"+payload+"---", smallBudget, context.Background())
	smallResult, err := Decode(context.Background(), small.b)
	if err != nil || !smallResult.Complete() {
		t.Fatal("baseline alias definition", err)
	}
	for i := 0; i < 7; i++ {
		name := string(rune('b' + i))
		previous := string(rune('a' + i))
		payload += name + ": &" + name + " [*" + previous + ", *" + previous + "]\n"
	}
	budget := mustBudget(t, limits)
	f := bindBudgetFixture(t, "---\n"+payload+"---", budget, context.Background())
	_, err = Decode(context.Background(), f.b)
	var quota *parser.LimitExceeded
	if !errors.As(err, &quota) || quota.Name != "document-nodes" {
		t.Fatal("unbounded alias projection", err)
	}
	original := *quota
	_, err = Decode(context.Background(), f.b)
	if !containsQuota(err, original) {
		t.Fatal("recomputed sticky alias quota", err)
	}
	other := bindBudgetFixture(t, "---\nx: 2\n---", budget, context.Background())
	result, err := Decode(context.Background(), other.b)
	if err != nil || !result.Complete() {
		t.Fatal("independent document lost allowance", err)
	}
	entry, _ := result.Root().KnownEntry(0)
	cause := budget.ChargeWork(limits.WorkUnits + 1)
	if cause == nil {
		t.Fatal("expected work stop")
	}
	if text, _ := entry.Value().Scalar(); text != "2" {
		t.Fatal("retained immutable facts lost")
	}
	_, err = Decode(context.Background(), other.b)
	if err == nil {
		t.Fatal("new analysis evaded operation stop")
	}
}

func TestDecodeOriginalUnicodeTagsAndBodyUncertainty(t *testing.T) {
	f, r := decoded(t, "\ufeff---\r\nmap: {é: 1, \"\\u0061\": !<tag:yaml.org,2002:str>\t\"雪\"}\r\n---")
	if !r.Complete() {
		t.Fatal("long standard tag incomplete", r.ProblemCount())
	}
	m := fieldValue(t, r.Root(), "map")
	e, _ := m.KnownEntry(1)
	if e.Key() != "a" || sourceText(t, f.b, e.KeySpan()) != "\"\\u0061\"" || sourceText(t, f.b, e.Value().Span()) != "!<tag:yaml.org,2002:str>\t\"雪\"" {
		t.Fatal("original Unicode/tag positions", sourceText(t, f.b, e.KeySpan()), sourceText(t, f.b, e.Value().Span()))
	}
	limits := parser.DefaultLimits()
	limits.SyntaxDepth = 4
	budget := mustBudget(t, limits)
	raw := "---\ntitle: Reliable\n---\n" + strings.Repeat("> ", 10) + "deep\n"
	source := parser.NewSource([]byte(raw))
	options, _ := parser.NewFrontmatterOptions(true, nil)
	parsed := parser.ParseWithOptions(context.Background(), source, budget, parser.ParseOptions{Frontmatter: options})
	if parsed.Status == parser.Complete {
		t.Fatal("body fixture should be uncertain")
	}
	b, err := Bind(context.Background(), context.Background(), budget, parsed.Document)
	if err != nil {
		t.Fatal(err)
	}
	r, err = Decode(context.Background(), b)
	if err != nil || !r.Complete() {
		t.Fatal("reliable header lost to unrelated body depth", err)
	}
	title, err := r.Title(context.Background(), b)
	if err != nil || title.Text != "Reliable" {
		t.Fatal("title", title, err)
	}
}

func TestDecodeBeforeInputAdmission(t *testing.T) {
	limits := parser.DefaultLimits()
	limits.DocumentNodes = 100
	budget := mustBudget(t, limits)
	f := bindBudgetFixture(t, "---\na: 1\n---", budget, context.Background())
	text, err := admitDecode(context.Background(), f.b)
	var quota *parser.LimitExceeded
	if text != "" || !errors.As(err, &quota) || quota.Name != "document-nodes" {
		t.Fatal("native input admitted before storage reservation", text, err)
	}
	limits = parser.DefaultLimits()
	budget = mustBudget(t, limits)
	f = bindBudgetFixture(t, "---\na: 1\n---", budget, context.Background())
	if err := budget.ChargeWork(limits.WorkUnits - 1000); err != nil {
		t.Fatal(err)
	}
	text, err = admitDecode(context.Background(), f.b)
	if err != nil {
		t.Fatal("small admission fixture should fit", err)
	}
	if text != "a: 1\n" {
		t.Fatal(text)
	}
	if err := reserveNumeric(context.Background(), f.b, strings.Repeat("1", 100)); err == nil {
		t.Fatal("big integer conversion admitted before quadratic work reservation")
	}
}

func TestDecodeYAMLExactCorePrecision(t *testing.T) {
	_, r := decoded(t, "---\nodd: .iNf\nbig: 184467440737095516160\nfraction: 0.12345678901234567890123456789\nlegacy: 1_000\n---")
	if !r.Complete() {
		t.Fatal("precision incomplete", r.ProblemCount())
	}
	for _, tc := range []struct {
		key  string
		kind Kind
		text string
	}{{"odd", String, ".iNf"}, {"big", Integer, "184467440737095516160"}, {"fraction", Float, "12345678901234567890123456789e-29"}, {"legacy", String, "1_000"}} {
		v := fieldValue(t, r.Root(), tc.key)
		text, _ := v.Scalar()
		if v.Kind() != tc.kind || text != tc.text {
			t.Fatal(tc.key, v.Kind(), text)
		}
	}
}

func TestDecodeTOMLRejectsTableAdjacencyBeforeValue(t *testing.T) {
	limits := parser.DefaultLimits()
	limits.SyntaxDepth = 4
	budget := mustBudget(t, limits)
	f := bindBudgetFixture(t, "+++\n[a] x=[[[?\n+++", budget, context.Background())
	r, err := Decode(context.Background(), f.b)
	if err != nil || r.Complete() || r.ProblemCount() == 0 {
		t.Fatal("same-line table suffix reached a recursive value", r.Complete(), err)
	}
	p, _ := r.KnownProblem(0)
	if p.Code != Malformed || sourceText(t, f.b, p.Location) != "x" {
		t.Fatal("native table finishLine boundary", p.Code, sourceText(t, f.b, p.Location))
	}
}

func TestDecodeUnsupportedKeysDenyAffectedOrdering(t *testing.T) {
	f, r := decoded(t, "---\ntitle: Good\nnested: {true: value, z: 1, a: 2}\n---")
	nested := fieldValue(t, r.Root(), "nested")
	if r.Complete() || r.Root().Complete() || nested.Complete() {
		t.Fatal("omitted unsupported key minted complete mapping")
	}
	orders, err := r.Ordering(context.Background(), f.b, OrderOptions{Recursive: true})
	if err != nil || len(orders) != 0 {
		t.Fatal("omitted unsupported key minted sorted authority", orders, err)
	}
	title, err := r.Title(context.Background(), f.b)
	if err != nil || !title.Known || title.Text != "Good" {
		t.Fatal("independent title lost", title, err)
	}
}

func TestDecodeMalformedBlockDoesNotRetainGuessedFacts(t *testing.T) {
	f, r := decoded(t, "---\ntitle: Maybe\nnested:\n  z: |++\n    text\n  a: 1\n---")
	if r.Complete() || r.Root().Kind() != Unknown || r.ProblemCount() == 0 {
		t.Fatal("malformed lexical block retained guessed mapping")
	}
	title, err := r.Title(context.Background(), f.b)
	if err != nil || title.Known {
		t.Fatal("malformed block minted title authority", title, err)
	}
	orders, err := r.Ordering(context.Background(), f.b, OrderOptions{Recursive: true})
	if err != nil || len(orders) != 0 {
		t.Fatal("malformed block minted ordering", orders, err)
	}
}
