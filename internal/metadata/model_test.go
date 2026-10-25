package metadata

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

type fixture struct {
	t   *testing.T
	raw string
	b   Binding
}

func bindFixture(t *testing.T, raw string) fixture {
	t.Helper()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	return bindBudgetFixture(t, raw, budget, context.Background())
}
func bindBudgetFixture(t *testing.T, raw string, budget *parser.OperationBudget, operation context.Context) fixture {
	t.Helper()
	source := parser.NewSource([]byte(raw))
	options, _ := parser.NewFrontmatterOptions(true, nil)
	parsed := parser.ParseWithOptions(context.Background(), source, budget, parser.ParseOptions{Frontmatter: options})
	if parsed.Document == nil {
		t.Fatal(parsed)
	}
	b, err := Bind(context.Background(), operation, budget, parsed.Document)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{t, raw, b}
}
func (f fixture) span(text string) parser.Span {
	f.t.Helper()
	start := strings.Index(f.raw, text)
	if start < 0 {
		f.t.Fatal("fixture text absent", text)
	}
	span, err := f.b.Source().Span(start, start+len(text))
	if err != nil {
		f.t.Fatal(err)
	}
	return span
}
func (f fixture) scalar(text string, kind Kind, semantic string) Value {
	f.t.Helper()
	v, err := scalar(context.Background(), f.b, f.span(text), kind, semantic, semantic == "true")
	if err != nil {
		f.t.Fatal(err)
	}
	return v
}
func (f fixture) entry(key, keyText, whole string, v Value) Entry {
	f.t.Helper()
	span := f.span(whole)
	offset := strings.Index(whole, keyText)
	if offset < 0 {
		f.t.Fatal(keyText, whole)
	}
	keySpan, _ := f.b.Source().Span(span.Start()+offset, span.Start()+offset+len(keyText))
	e, err := entry(context.Background(), f.b, key, keySpan, span, v)
	if err != nil {
		f.t.Fatal(err)
	}
	return e
}
func (f fixture) mapping(span parser.Span, entries []Entry, restrictions MoveRestrictions) Value {
	f.t.Helper()
	v, err := collection(context.Background(), f.b, span, Mapping, entries, nil, restrictions)
	if err != nil {
		f.t.Fatal(err)
	}
	return v
}
func (f fixture) result(entries []Entry, restrictions MoveRestrictions) Result {
	f.t.Helper()
	root := f.mapping(f.b.Header().Content(), entries, restrictions)
	r, err := result(context.Background(), f.b, root, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}
func indices(o Order) []int {
	out := make([]int, o.Count())
	for i := range out {
		out[i], _ = o.KnownIndex(i)
	}
	return out
}

func TestBindingRecognizedSourceAndOperation(t *testing.T) {
	for _, raw := range []string{"---\n---\n", "\xef\xbb\xbf+++\r\n+++\r\nbody\n", "---\nmalformed: [\n---\nbody\n"} {
		f := bindFixture(t, raw)
		if f.b.State() != Undecoded {
			t.Fatal(f.b.State())
		}
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		if f.b.Header().OwnsOperation(budget) {
			t.Fatal("foreign budget owns header")
		}
		if !f.b.Header().OwnsOperation(f.b.state.budget) {
			t.Fatal("origin operation lost")
		}
		// Recognition itself never claims the opaque malformed bytes were decoded.
		if (Result{}).Complete() {
			t.Fatal("zero metadata complete")
		}
	}
	absent := bindFixture(t, "# body\n")
	if absent.b.State() != Absent {
		t.Fatal(absent.b.State())
	}
	unclosed := bindFixture(t, "---\nz: 1\n")
	if unclosed.b.State() != Unterminated {
		t.Fatal(unclosed.b.State())
	}
	if _, err := collection(context.Background(), unclosed.b, unclosed.b.Header().Content(), Mapping, nil, nil, 0); !errors.Is(err, ErrContract) {
		t.Fatal("unterminated minted complete mapping", err)
	}
	empty := bindFixture(t, "---\n---\n")
	if !empty.result(nil, 0).Complete() {
		t.Fatal("complete empty header cannot be empty map")
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	source := parser.NewSource([]byte("---\n---\n"))
	options, _ := parser.NewFrontmatterOptions(true, nil)
	parsed := parser.ParseWithOptions(context.Background(), source, budget, parser.ParseOptions{Frontmatter: options})
	foreign, _ := parser.NewOperationBudget(parser.DefaultLimits())
	if _, err := Bind(context.Background(), context.Background(), foreign, parsed.Document); !errors.Is(err, ErrContract) {
		t.Fatal("foreign operation binding", err)
	}
}

func TestModelOriginalSpansKindsAndFrozenCollections(t *testing.T) {
	f := bindFixture(t, "\xef\xbb\xbf---\r\nz: 9007199254740993 # exact\r\na: [true, 1979-05-27]\r\n---\r\nbody\n")
	integer := f.scalar("9007199254740993", Integer, "9007199254740993")
	boolean := f.scalar("true", Boolean, "true")
	date := f.scalar("1979-05-27", LocalDate, "1979-05-27")
	elements := []Value{boolean, date}
	seq, err := collection(context.Background(), f.b, f.span("[true, 1979-05-27]"), Sequence, nil, elements, 0)
	if err != nil {
		t.Fatal(err)
	}
	elements[0] = Value{}
	entries := []Entry{f.entry("z", "z", "z: 9007199254740993 # exact\r\n", integer), f.entry("a", "a", "a: [true, 1979-05-27]\r\n", seq)}
	r := f.result(entries, 0)
	entries[0] = Entry{}
	e, ok := r.Root().KnownEntry(0)
	if !ok || e.Key() != "z" {
		t.Fatal("mutable map snapshot")
	}
	text, _ := f.b.Source().Text(e.Span())
	if text != "z: 9007199254740993 # exact\r\n" {
		t.Fatal(text)
	}
	if e.KeySpan().Start() != 8 {
		t.Fatal("BOM/CRLF byte offset", e.KeySpan().Start())
	}
	n, ok := e.Value().Scalar()
	if !ok || n != "9007199254740993" || e.Value().Kind() != Integer {
		t.Fatal("integer coerced", n)
	}
	a, _ := r.Root().KnownEntry(1)
	first, _ := a.Value().KnownElement(0)
	if v, ok := first.Boolean(); !v || !ok {
		t.Fatal("boolean not native")
	}
	second, _ := a.Value().KnownElement(1)
	if second.Kind() != LocalDate {
		t.Fatal("date coerced")
	}

	foreign := parser.NewSource(f.b.Source().Bytes())
	same, _ := foreign.Span(integer.Span().Start(), integer.Span().End())
	if _, err := scalar(context.Background(), f.b, same, Integer, "1", false); !errors.Is(err, ErrContract) {
		t.Fatal("equal-byte foreign source accepted")
	}
	body := f.span("body")
	if _, err := scalar(context.Background(), f.b, body, String, "body", false); !errors.Is(err, ErrContract) {
		t.Fatal("body accepted as metadata")
	}
	if _, err := scalar(context.Background(), f.b, f.b.Header().Opener(), String, "---", false); !errors.Is(err, ErrContract) {
		t.Fatal("opener accepted as metadata")
	}
	other := bindFixture(t, f.raw)
	if _, err := entry(context.Background(), other.b, "z", other.span("z"), other.span("z: 9007199254740993 # exact\r\n"), integer); !errors.Is(err, ErrContract) {
		t.Fatal("foreign model owner accepted", err)
	}
}

func TestDuplicateKeysAndIncompleteConclusions(t *testing.T) {
	f := bindFixture(t, "---\n\"a\": 1\na: 2\n---\n")
	entries := []Entry{f.entry("a", "\"a\"", "\"a\": 1\n", f.scalar("1", Integer, "1")), f.entry("a", "a", "a: 2\n", f.scalar("2", Integer, "2"))}
	r := f.result(entries, 0)
	if r.Complete() || r.ProblemCount() != 1 {
		t.Fatal("duplicate became complete", r)
	}
	p, _ := r.KnownProblem(0)
	if p.Code != DuplicateKey || p.Location != entries[1].KeySpan() {
		t.Fatal(p)
	}
	title, err := r.Title(context.Background(), f.b)
	if err != nil || title.Known {
		t.Fatal("incomplete missing title proved", title, err)
	}
	upper := bindFixture(t, "---\na: 1\nA: 2\n---\n")
	if !upper.result([]Entry{upper.entry("a", "a", "a: 1\n", upper.scalar("1", Integer, "1")), upper.entry("A", "A", "A: 2\n", upper.scalar("2", Integer, "2"))}, 0).Complete() {
		t.Fatal("sort policy became duplicate policy")
	}
}

func TestExactTopLevelTitlePolicy(t *testing.T) {
	for _, tc := range []struct {
		key      string
		kind     Kind
		semantic string
		present  bool
	}{
		{"title", String, "\u2003", false}, {"title", String, "図 title", true}, {"Title", String, "Title", false}, {"title", Boolean, "true", false}, {"title", Integer, "0", false}, {"title", Null, "", false},
	} {
		t.Run(tc.key+string(tc.kind)+tc.semantic, func(t *testing.T) {
			f := bindFixture(t, "---\n"+tc.key+": value\n---\n")
			v := f.scalar("value", tc.kind, tc.semantic)
			r := f.result([]Entry{f.entry(tc.key, tc.key, tc.key+": value\n", v)}, 0)
			got, err := r.Title(context.Background(), f.b)
			if err != nil || !got.Known || got.Present != tc.present {
				t.Fatal(got, err)
			}
			if got.Present && (got.Text != tc.semantic || got.Location != v.Span()) {
				t.Fatal(got)
			}
		})
	}
	f := bindFixture(t, "---\nouter:\n  title: nested\n---\n")
	nested := f.mapping(f.span("  title: nested\n"), []Entry{f.entry("title", "title", "  title: nested\n", f.scalar("nested", String, "nested"))}, 0)
	r := f.result([]Entry{f.entry("outer", "outer", "outer:\n  title: nested\n", nested)}, 0)
	title, err := r.Title(context.Background(), f.b)
	if err != nil || !title.Known || title.Present {
		t.Fatal("nested title consumed", title, err)
	}
}

func orderedFixture(t *testing.T, keys []string) (fixture, Result) {
	raw := "---\n"
	for _, key := range keys {
		raw += strconv.Quote(key) + ": value\n"
	}
	raw += "---\n"
	f := bindFixture(t, raw)
	entries := make([]Entry, 0, len(keys))
	for _, key := range keys {
		whole := strconv.Quote(key) + ": value\n"
		span := f.span(whole)
		valueSpan, _ := f.b.Source().Span(span.Start()+len(strconv.Quote(key))+2, span.End()-1)
		v, err := scalar(context.Background(), f.b, valueSpan, String, "value", false)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, f.entry(key, strconv.Quote(key), whole, v))
	}
	return f, f.result(entries, 0)
}
func TestOrderingFullFoldStableTiesAndDefaults(t *testing.T) {
	keys := []string{"z", "Σ", "ς", "σ", "SS", "ß", " a", "a ", "A", "a"}
	f, r := orderedFixture(t, keys)
	for _, tc := range []struct {
		options OrderOptions
		want    []int
	}{{OrderOptions{}, []int{6, 8, 4, 9, 7, 0, 5, 1, 2, 3}}, {OrderOptions{CaseInsensitive: true}, []int{6, 8, 9, 7, 4, 5, 0, 1, 2, 3}}} {
		orders, err := r.Ordering(context.Background(), f.b, tc.options)
		if err != nil || len(orders) != 1 || !orders[0].Violation() || !reflect.DeepEqual(indices(orders[0]), tc.want) {
			t.Fatal("desired exact stable order", orders, err, tc.want)
		}
	}
	folded, err := parser.FoldCase(context.Background(), f.b.state.budget, "\x00A  ßΣςK")
	if err != nil || folded != "\x00a  ssσσk" {
		t.Fatal("reference normalization or simple lowercase leaked", folded, err)
	}
	for i, key := range keys {
		e, _ := r.Root().KnownEntry(i)
		if e.Key() != key {
			t.Fatal("sorting mutated source order")
		}
	}
}

func TestRecursiveOrderingDoesNotMoveSequenceElements(t *testing.T) {
	f := bindFixture(t, "---\nitems:\n - {z: 1, a: 2}\n - {b: 3, A: 4}\n---\n")
	var maps []Value
	for _, tc := range []struct{ whole, first, second, n1, n2 string }{{"{z: 1, a: 2}", "z", "a", "1", "2"}, {"{b: 3, A: 4}", "b", "A", "3", "4"}} {
		first := f.scalar(tc.n1, Integer, tc.n1)
		second := f.scalar(tc.n2, Integer, tc.n2)
		e1, err := entry(context.Background(), f.b, tc.first, f.span(tc.first), parser.Span{}, first)
		if err != nil {
			t.Fatal(err)
		}
		e2, err := entry(context.Background(), f.b, tc.second, f.span(tc.second), parser.Span{}, second)
		if err != nil {
			t.Fatal(err)
		}
		maps = append(maps, f.mapping(f.span(tc.whole), []Entry{e1, e2}, 0))
	}
	seq, err := collection(context.Background(), f.b, f.span(" - {z: 1, a: 2}\n - {b: 3, A: 4}\n"), Sequence, nil, maps, 0)
	if err != nil {
		t.Fatal(err)
	}
	r := f.result([]Entry{f.entry("items", "items", "items:\n - {z: 1, a: 2}\n - {b: 3, A: 4}\n", seq)}, 0)
	shallow, err := r.Ordering(context.Background(), f.b, OrderOptions{})
	if err != nil || len(shallow) != 1 {
		t.Fatal(shallow, err)
	}
	recursive, err := r.Ordering(context.Background(), f.b, OrderOptions{Recursive: true})
	if err != nil || len(recursive) != 3 {
		t.Fatal(recursive, err)
	}
	for _, order := range recursive[1:] {
		if !order.Violation() || !reflect.DeepEqual(indices(order), []int{1, 0}) || order.MoveEligibility() != ReportOnly {
			t.Fatal(order)
		}
	}
	for i, want := range maps {
		got, _ := seq.KnownElement(i)
		if got != want {
			t.Fatal("sequence order mutated")
		}
	}
}

func TestWholeEntryPreservationPolicy(t *testing.T) {
	f := bindFixture(t, "---\r\nz: 'two' # inline\r\na: 1\r\n---\r\n")
	entries := []Entry{f.entry("z", "z", "z: 'two' # inline\r\n", f.scalar("'two'", String, "two")), f.entry("a", "a", "a: 1\r\n", f.scalar("1", Integer, "1"))}
	r := f.result(entries, 0)
	orders, err := r.Ordering(context.Background(), f.b, OrderOptions{})
	if err != nil || len(orders) != 1 || orders[0].MoveEligibility() != IndependentEntries {
		t.Fatal(orders, err)
	}
	desired := ""
	for _, i := range indices(orders[0]) {
		e, _ := r.Root().KnownEntry(i)
		text, _ := f.b.Source().Text(e.Span())
		desired += text
	}
	if desired != "a: 1\r\nz: 'two' # inline\r\n" {
		t.Fatal("whole entry lost original quoting/comment/ending", desired)
	}
	for _, restriction := range []MoveRestrictions{StandaloneComments, AnchorsOrAliases, TOMLTables, TOMLDottedKeys, TOMLArrayTables} {
		restricted := f.result(entries, restriction)
		o, err := restricted.Ordering(context.Background(), f.b, OrderOptions{})
		if err != nil || !o[0].Violation() || o[0].MoveEligibility() != ReportOnly {
			t.Fatal("syntax restriction admitted movable entries", restriction, o, err)
		}
	}
	comment := bindFixture(t, "---\n# author note\nz: 2\na: 1\n---\n")
	withComment := comment.result([]Entry{comment.entry("z", "z", "z: 2\n", comment.scalar("2", Integer, "2")), comment.entry("a", "a", "a: 1\n", comment.scalar("1", Integer, "1"))}, 0)
	o, err := withComment.Ordering(context.Background(), comment.b, OrderOptions{})
	if err != nil || o[0].MoveEligibility() != ReportOnly {
		t.Fatal("standalone gap comment moved", o, err)
	}
	overlap := append([]Entry(nil), entries...)
	overlap[1].span = entries[0].span
	overlapped := f.result(overlap, 0)
	o, err = overlapped.Ordering(context.Background(), f.b, OrderOptions{})
	if err != nil || o[0].MoveEligibility() != ReportOnly {
		t.Fatal("overlapping entry movement", o, err)
	}
	flow := bindFixture(t, "---\n{z: 1, a: 2}\n---\n")
	flowEntries := []Entry{flow.entry("z", "z", "z: 1", flow.scalar("1", Integer, "1")), flow.entry("a", "a", "a: 2", flow.scalar("2", Integer, "2"))}
	o, err = flow.result(flowEntries, 0).Ordering(context.Background(), flow.b, OrderOptions{})
	if err != nil || o[0].MoveEligibility() != ReportOnly {
		t.Fatal("partial-line members moveable", o, err)
	}
}

func TestAliasUseAndSyntheticTableLocations(t *testing.T) {
	f := bindFixture(t, "---\nanchor: &x {z: 1}\ncopy: *x\n---\n")
	use := f.span("*x")
	semantic, err := scalar(context.Background(), f.b, use, Integer, "1", false)
	if err != nil {
		t.Fatal(err)
	}
	derived, err := entry(context.Background(), f.b, "z", use, parser.Span{}, semantic)
	if err != nil {
		t.Fatal(err)
	}
	alias := f.mapping(use, []Entry{derived}, AnchorsOrAliases)
	r := f.result([]Entry{f.entry("copy", "copy", "copy: *x\n", alias)}, 0)
	e, _ := r.Root().KnownEntry(0)
	key, _ := e.Value().KnownEntry(0)
	if key.KeySpan() != use || key.Value().Span() != use || key.Span().Present() {
		t.Fatal("alias pointed to definition or fabricated entry", key)
	}
	synthetic := f.mapping(parser.Span{}, []Entry{derived}, TOMLTables)
	if synthetic.Span().Present() {
		t.Fatal("synthetic table invented envelope")
	}
	syntheticEntry, err := entry(context.Background(), f.b, "parent", parser.Span{}, parser.Span{}, synthetic)
	if err != nil || syntheticEntry.KeySpan().Present() || syntheticEntry.Span().Present() {
		t.Fatal(syntheticEntry, err)
	}
}

func TestSharedControlsAndRetainedFacts(t *testing.T) {
	f, r := orderedFixture(t, []string{"z", "a"})
	orders, err := r.Ordering(context.Background(), f.b, OrderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	caller, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	operation, stop := context.WithCancel(context.Background())
	f.b.state.operation = operation
	stop()
	quota := f.b.state.budget.ChargeWork(f.b.state.budget.Limits().WorkUnits + 1)
	_, err = r.Ordering(caller, f.b, OrderOptions{})
	var got, want *parser.LimitExceeded
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, context.Canceled) || !errors.As(err, &got) || !errors.As(quota, &want) || *got != *want {
		t.Fatal("canonical compound controls lost", err, quota)
	}
	if !r.Complete() || !orders[0].Violation() {
		t.Fatal("retained facts lost")
	}
	e, ok := r.Root().KnownEntry(0)
	if !ok || e.Key() != "z" {
		t.Fatal("retained source facts lost")
	}
	if _, err := scalar(caller, f.b, e.Value().Span(), String, "new", false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("new model allocation ignored stop", err)
	}
	nodeFixture := bindFixture(t, "---\nz: 1\n---\n")
	if err := nodeFixture.b.charge(context.Background(), 0, nodeFixture.b.state.budget.Limits().DocumentNodes); err == nil {
		t.Fatal("document node bound ignored")
	}
	limits := parser.DefaultLimits()
	limits.SyntaxDepth = 2
	budget, _ := parser.NewOperationBudget(limits)
	depth := bindBudgetFixture(t, "---\nx\n---\n", budget, context.Background())
	v := depth.scalar("x", String, "x")
	v, err = collection(context.Background(), depth.b, depth.span("x"), Sequence, nil, []Value{v}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = collection(context.Background(), depth.b, depth.span("x"), Sequence, nil, []Value{v}, 0)
	if !errors.As(err, &got) || got.Name != "syntax-depth" {
		t.Fatal("depth bound ignored", err)
	}
}

func TestCompleteHeaderWithIncompleteBodyKeepsRecognition(t *testing.T) {
	limits := parser.DefaultLimits()
	limits.SyntaxDepth = 1
	budget, _ := parser.NewOperationBudget(limits)
	source := parser.NewSource([]byte("---\nx: 1\n---\n> > body\n"))
	options, _ := parser.NewFrontmatterOptions(true, nil)
	parsed := parser.ParseWithOptions(context.Background(), source, budget, parser.ParseOptions{Frontmatter: options})
	if parsed.Status != parser.Incomplete || parsed.Document == nil {
		t.Fatal("fixture did not interrupt body", parsed)
	}
	header, present := parsed.Document.Frontmatter()
	if !present || !header.Closer().Present() || !header.OwnsOperation(budget) {
		t.Fatal("reliable complete header lost beside body uncertainty", header)
	}
	b, err := Bind(context.Background(), context.Background(), budget, parsed.Document)
	if err != nil || b.State() != Undecoded {
		t.Fatal("body completeness unnecessarily required", b, err)
	}
	if _, err := Bind(context.Background(), context.Background(), budget, &parser.Document{}); !errors.Is(err, ErrContract) {
		t.Fatal("zero document accepted", err)
	}
}

func TestRecognizedHeaderSharesOriginalDocumentRecords(t *testing.T) {
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	options, _ := parser.NewFrontmatterOptions(true, nil)
	parsed := parser.ParseWithOptions(context.Background(), parser.NewSource([]byte("---\nx: 1\n---\nbody\n")), budget, parser.ParseOptions{Frontmatter: options})
	header, present := parsed.Document.Frontmatter()
	if !present {
		t.Fatal("missing header")
	}
	first := header.ReserveRecords(context.Background(), budget, budget.Limits().DocumentNodes)
	var quota *parser.LimitExceeded
	if !errors.As(first, &quota) || quota.Name != "document-nodes" || quota.Observed <= quota.Allowed {
		t.Fatal("origin parse construction was reset", first)
	}
	original := *quota
	quota.Name = "mutated caller error"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	again := header.ReserveRecords(ctx, budget, 0)
	var retained *parser.LimitExceeded
	if !errors.Is(again, context.Canceled) || !errors.As(again, &retained) || *retained != original {
		t.Fatal("derived stop lost or caller changed shared state", again)
	}
	if header.OwnsOperation(nil) || (parser.Frontmatter{}).OwnsOperation(budget) {
		t.Fatal("empty operation authority")
	}
}

func TestNativeTemporalAndFloatKindsRetainOriginalSpelling(t *testing.T) {
	raw := "+++\r\nfinite = 1.2500\r\nnot_number = -nan\r\ninfinity = +inf\r\noffset = 1979-05-27T07:32:00Z\r\nlocal = 1979-05-27T07:32:00\r\ndate = 1979-05-27\r\nclock = 07:32:00.500\r\n+++\r\n"
	f := bindFixture(t, raw)
	for _, tc := range []struct {
		key, spelling, semantic string
		kind                    Kind
	}{
		{"finite", "1.2500", "1.25", Float}, {"not_number", "-nan", "nan", Float}, {"infinity", "+inf", "+inf", Float},
		{"offset", "1979-05-27T07:32:00Z", "1979-05-27T07:32:00Z", OffsetDateTime},
		{"local", "1979-05-27T07:32:00", "1979-05-27T07:32:00", LocalDateTime},
		{"date", "1979-05-27", "1979-05-27", LocalDate}, {"clock", "07:32:00.500", "07:32:00.5", LocalTime},
	} {
		// Locate within this actual assignment so common temporal prefixes do not
		// accidentally borrow an earlier value's original source range.
		whole := tc.key + " = " + tc.spelling + "\r\n"
		entrySpan := f.span(whole)
		valueSpan, _ := f.b.Source().Span(entrySpan.Start()+len(tc.key)+3, entrySpan.End()-2)
		v, err := scalar(context.Background(), f.b, valueSpan, tc.kind, tc.semantic, false)
		if err != nil {
			t.Fatal(err)
		}
		text, ok := v.Scalar()
		original, _ := f.b.Source().Text(v.Span())
		if !ok || text != tc.semantic || v.Kind() != tc.kind || original != tc.spelling {
			t.Fatal(tc.key, text, original, v.Kind())
		}
	}
	null := bindFixture(t, "---\nnothing: null\n---\n")
	value := null.scalar("null", Null, "")
	if _, ok := value.Scalar(); ok || value.Kind() != Null {
		t.Fatal("null coerced into a string")
	}
	if _, err := result(context.Background(), f.b, Value{}, nil); !errors.Is(err, ErrContract) {
		t.Fatal("zero model admitted", err)
	}
}

func TestTitleEmptyAndUnknownRootsObserveControls(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		for _, control := range []string{"nil", "caller-cancel", "caller-deadline", "operation-cancel", "document", "total", "work", "compound"} {
			t.Run(strconv.FormatBool(unknown)+"/"+control, func(t *testing.T) {
				operation, stop := context.WithCancel(context.Background())
				defer stop()
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				f := bindBudgetFixture(t, "---\n---\n", budget, operation)
				r := f.result(nil, 0)
				if unknown {
					root, err := scalar(context.Background(), f.b, f.b.Header().Content(), Unknown, "", false)
					if err != nil {
						t.Fatal(err)
					}
					r, err = result(context.Background(), f.b, root, nil)
					if err != nil {
						t.Fatal(err)
					}
				}
				ctx := context.Background()
				var expected error
				switch control {
				case "nil":
					ctx = nil
				case "caller-cancel":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				case "caller-deadline":
					var cancel context.CancelFunc
					ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
					defer cancel()
				case "operation-cancel":
					stop()
				case "document":
					expected = f.b.Header().ReserveRecords(ctx, budget, budget.Limits().DocumentNodes)
				case "total":
					expected = budget.ChargeNodes(budget.Limits().TotalNodes + 1)
				case "work":
					expected = budget.ChargeWork(budget.Limits().WorkUnits + 1)
				case "compound":
					var cancel context.CancelFunc
					ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
					defer cancel()
					stop()
					expected = budget.ChargeWork(budget.Limits().WorkUnits + 1)
				}
				title, err := r.Title(ctx, f.b)
				if err == nil || title.Known || title.Present {
					t.Fatal("stopped title published a new conclusion", title, err)
				}
				switch control {
				case "nil":
					if !errors.Is(err, ErrContract) {
						t.Fatal(err)
					}
				case "caller-cancel", "operation-cancel":
					if !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
				case "caller-deadline":
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Fatal(err)
					}
				case "compound":
					if !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) {
						t.Fatal(err)
					}
				}
				if expected != nil {
					var want, got *parser.LimitExceeded
					if !errors.As(expected, &want) || !errors.As(err, &got) || *got != *want {
						t.Fatal("first quota lost", err, expected)
					}
				}
				if r.Complete() == unknown || r.Root().Kind() == Unknown != unknown {
					t.Fatal("immutable facts changed after stop")
				}
			})
		}
	}
}

func TestTitleEmptyRootsObserveExpiredOperationBudget(t *testing.T) {
	limits := parser.DefaultLimits()
	limits.OperationTime = time.Second
	budget, _ := parser.NewOperationBudget(limits)
	f := bindBudgetFixture(t, "---\n---\n", budget, context.Background())
	complete := f.result(nil, 0)
	root, err := scalar(context.Background(), f.b, f.b.Header().Content(), Unknown, "", false)
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := result(context.Background(), f.b, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	expected := budget.ChargeWork(0)
	var want *parser.LimitExceeded
	if !errors.As(expected, &want) || want.Name != "operation-time" {
		t.Fatal("fixture did not expire operation budget", expected)
	}
	for _, r := range []Result{complete, unknown} {
		got, err := r.Title(context.Background(), f.b)
		var quota *parser.LimitExceeded
		if got.Known || !errors.As(err, &quota) || *quota != *want {
			t.Fatal("elapsed operation budget lost", got, err, expected)
		}
	}
}

func TestWholeEntryPhysicalEndingsAndAbsoluteStarts(t *testing.T) {
	for _, endings := range [][2]string{{"\n", "\n"}, {"\r", "\r"}, {"\r\n", "\r\n"}, {"\r\n", "\r"}, {"\r", "\n"}} {
		t.Run(strconv.Quote(endings[0])+strconv.Quote(endings[1]), func(t *testing.T) {
			z := "z: 2" + endings[0]
			a := "a: 1" + endings[1]
			f := bindFixture(t, "---\n"+z+a+"---\n")
			r := f.result([]Entry{f.entry("z", "z", z, f.scalar("2", Integer, "2")), f.entry("a", "a", a, f.scalar("1", Integer, "1"))}, 0)
			orders, err := r.Ordering(context.Background(), f.b, OrderOptions{})
			if err != nil || !orders[0].Violation() || orders[0].MoveEligibility() != IndependentEntries {
				t.Fatal("genuine physical endings refused", orders, err)
			}
		})
	}
	split := bindFixture(t, "---\r\nz: 2\r\na: 1\r\n---\r\n")
	entries := []Entry{split.entry("z", "z", "z: 2\r", split.scalar("2", Integer, "2")), split.entry("a", "a", "a: 1\r\n", split.scalar("1", Integer, "1"))}
	r := split.result(entries, 0)
	orders, err := r.Ordering(context.Background(), split.b, OrderOptions{})
	if err != nil || !orders[0].Violation() || orders[0].MoveEligibility() != ReportOnly {
		t.Fatal("split CRLF ending eligible", orders, err)
	}
	if original, _ := split.b.Source().Text(entries[0].Span()); original != "z: 2\r" {
		t.Fatal("refusal changed retained span", original)
	}
	for _, tc := range []struct {
		name, raw, mapping, first, second string
		eligible                          bool
	}{
		{"indented-whole-lines", "outer:\n  z: 2\n  a: 1\n", "  z: 2\n  a: 1\n", "  z: 2\n", "  a: 1\n", true},
		{"first-key-after-indent", "outer:\n  z: 2\n  a: 1\n", "z: 2\n  a: 1\n", "z: 2\n", "  a: 1\n", false},
		{"compact-sequence-prefix", "items:\n - z: 2\n   a: 1\n", "z: 2\n   a: 1\n", "z: 2\n", "   a: 1\n", false},
		{"starts-inside-CRLF", "outer:\r\n  z: 2\r\n  a: 1\r\n", "\n  z: 2\r\n  a: 1\r\n", "\n  z: 2\r\n", "  a: 1\r\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := bindFixture(t, "---\n"+tc.raw+"---\n")
			members := []Entry{f.entry("z", "z", tc.first, f.scalar("2", Integer, "2")), f.entry("a", "a", tc.second, f.scalar("1", Integer, "1"))}
			nested := f.mapping(f.span(tc.mapping), members, 0)
			// The nested authored map is enough to inspect its necessary movement
			// predicate; no production decoder or edit is manufactured here.
			got, err := moveEligibility(context.Background(), f.b, nested)
			want := ReportOnly
			if tc.eligible {
				want = IndependentEntries
			}
			if err != nil || got != want {
				t.Fatal("original physical-line ownership", got, want, err)
			}
			key := "outer"
			value := nested
			if tc.name == "compact-sequence-prefix" {
				key = "items"
				content := tc.raw[strings.IndexByte(tc.raw, '\n')+1:]
				var err error
				value, err = collection(context.Background(), f.b, f.span(content), Sequence, nil, []Value{nested}, 0)
				if err != nil {
					t.Fatal(err)
				}
			}
			root := f.result([]Entry{f.entry(key, key, tc.raw, value)}, 0)
			orders, err := root.Ordering(context.Background(), f.b, OrderOptions{Recursive: true})
			if err != nil || len(orders) != 2 || !orders[1].Violation() || orders[1].MoveEligibility() != want || orders[1].Mapping().Span() != nested.Span() {
				t.Fatal("line refusal erased violation or original span", orders, err)
			}
		})
	}
}

func TestAuthoredMappingContainsChildValueWithoutEntryEnvelope(t *testing.T) {
	f := bindFixture(t, "---\nouter: {z: 1}\nother: 2\n---\n")
	misplaced := f.scalar("2", Integer, "2")
	bad, err := entry(context.Background(), f.b, "z", f.span("z"), parser.Span{}, misplaced)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collection(context.Background(), f.b, f.span("{z: 1}"), Mapping, []Entry{bad}, nil, 0); !errors.Is(err, ErrContract) {
		t.Fatal("authored parent accepted unrelated child value", err)
	}
	valid, err := entry(context.Background(), f.b, "z", f.span("z"), parser.Span{}, f.scalar("1", Integer, "1"))
	if err != nil {
		t.Fatal(err)
	}
	flow, err := collection(context.Background(), f.b, f.span("{z: 1}"), Mapping, []Entry{valid}, nil, 0)
	if err != nil || !flow.Complete() {
		t.Fatal("valid flow member refused", err)
	}
	synthetic, err := collection(context.Background(), f.b, parser.Span{}, Mapping, []Entry{bad}, nil, TOMLTables)
	if err != nil || synthetic.Span().Present() {
		t.Fatal("synthetic parent fabricated authored span", err)
	}
}

func containsQuota(err error, want parser.LimitExceeded) bool {
	var quota *parser.LimitExceeded
	if errors.As(err, &quota) && *quota == want {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if containsQuota(child, want) {
				return true
			}
		}
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return containsQuota(wrapped.Unwrap(), want)
	}
	return false
}
func TestOriginalParseDocumentStopRetainsExactFirstQuota(t *testing.T) {
	limits := parser.DefaultLimits()
	limits.DocumentNodes = 6
	budget, _ := parser.NewOperationBudget(limits)
	options, _ := parser.NewFrontmatterOptions(true, nil)
	parsed := parser.ParseWithOptions(context.Background(), parser.NewSource([]byte("---\nx: 1\n---\nbody *text*\n")), budget, parser.ParseOptions{Frontmatter: options})
	if parsed.Limit == nil || parsed.Limit.Name != "document-nodes" || parsed.Limit.Observed != 7 || parsed.Limit.Allowed != 6 {
		t.Fatal("fixture did not reach original body construction stop", parsed.Limit)
	}
	original := *parsed.Limit
	parsed.Limit.Name = "caller mutation"
	header, present := parsed.Document.Frontmatter()
	if !present || !header.Closer().Present() || !header.OwnsOperation(budget) {
		t.Fatal("known recognition lost")
	}
	first := header.ReserveRecords(context.Background(), budget, 0)
	var quota *parser.LimitExceeded
	if !errors.As(first, &quota) || *quota != original {
		t.Fatal("original stop recomputed or lost", first, original)
	}
	quota.Observed = 12345
	_, err := Bind(context.Background(), context.Background(), budget, parsed.Document)
	if !containsQuota(err, original) {
		t.Fatal("Bind replaced first original quota", err, original)
	}
	// Document-local exhaustion must not become an operation-wide stop.
	other := parser.ParseWithOptions(context.Background(), parser.NewSource([]byte("---\n---\n")), budget, parser.ParseOptions{Frontmatter: options})
	if other.Status != parser.Complete {
		t.Fatal("different document lost independent allowance", other.Limit)
	}
	otherHeader, _ := other.Document.Frontmatter()
	if err := otherHeader.ReserveRecords(context.Background(), budget, 0); err != nil {
		t.Fatal("unrelated document inherited stop", err)
	}
	global := budget.ChargeWork(budget.Limits().WorkUnits + 1)
	var globalQuota *parser.LimitExceeded
	if !errors.As(global, &globalQuota) {
		t.Fatal(global)
	}
	caller, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	operation, stop := context.WithCancel(context.Background())
	stop()
	_, err = Bind(caller, operation, budget, parsed.Document)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) || !containsQuota(err, original) || !containsQuota(err, *globalQuota) {
		t.Fatal("compound original/operation controls lost", err)
	}
	retained := header.ReserveRecords(context.Background(), budget, 0)
	if !containsQuota(retained, original) {
		t.Fatal("stored original quota was mutable", retained)
	}
}
