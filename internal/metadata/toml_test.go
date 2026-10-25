package metadata

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func tomlDecoded(t *testing.T, payload string) (fixture, Result) {
	t.Helper()
	f := bindFixture(t, "+++\n"+payload+"+++\nbody\n")
	r, err := Decode(context.Background(), f.b)
	if err != nil {
		t.Fatal(err)
	}
	return f, r
}
func tomlFind(t *testing.T, v Value, key string) Entry {
	t.Helper()
	for i := 0; i < v.EntryCount(); i++ {
		e, _ := v.KnownEntry(i)
		if e.Key() == key {
			return e
		}
	}
	t.Fatalf("missing %q among %d entries", key, v.EntryCount())
	return Entry{}
}
func TestTOMLNativeSourceValues(t *testing.T) {
	payload := "title = \"雪 # title\" # inline\nlarge = 9007199254740993\nbase = 0x20_0000_0000_0001\ntruth = true\nnumber = 1.25e+2\ninf = +inf\nnan = -nan\ndate = 2024-02-29\ntime = 07:32:00.123456789012300\nlocal = 2024-02-29t07:32:00.1200\noffset = 2024-02-29 07:32:00.000000000001+05:30\nshort = 07:32\nitems = [{name = \"one\"}, {name = \"two\"}]\n"
	f, r := tomlDecoded(t, payload)
	if !r.Complete() || r.Root().EntryCount() != 13 {
		t.Fatalf("incomplete/native entries: complete=%v count=%d problems=%d", r.Complete(), r.Root().EntryCount(), r.ProblemCount())
	}
	for _, want := range []struct {
		key  string
		kind Kind
		text string
	}{
		{"title", String, "雪 # title"}, {"large", Integer, "9007199254740993"}, {"base", Integer, "9007199254740993"}, {"number", Float, "125e0"}, {"inf", Float, "+inf"}, {"nan", Float, "nan"}, {"date", LocalDate, "2024-02-29"}, {"time", LocalTime, "07:32:00.1234567890123"}, {"local", LocalDateTime, "2024-02-29T07:32:00.12"}, {"offset", OffsetDateTime, "2024-02-29T07:32:00.000000000001+05:30"}, {"short", LocalTime, "07:32:00"},
	} {
		e := tomlFind(t, r.Root(), want.key)
		actual, _ := e.Value().Scalar()
		if e.Value().Kind() != want.kind || actual != want.text {
			t.Errorf("%s = %s %q, want %s %q", want.key, e.Value().Kind(), actual, want.kind, want.text)
		}
		if e.Value().Span().SourceToken() != f.b.Source().Token() {
			t.Fatal("foreign source")
		}
	}
	truth, ok := tomlFind(t, r.Root(), "truth").Value().Boolean()
	if !ok || !truth {
		t.Fatal("boolean lost")
	}
	items := tomlFind(t, r.Root(), "items").Value()
	if items.Kind() != Sequence || items.ElementCount() != 2 {
		t.Fatal("sequence lost")
	}
	second, _ := items.KnownElement(1)
	name, _ := tomlFind(t, second, "name").Value().Scalar()
	if name != "two" {
		t.Fatal(name)
	}
	title := tomlFind(t, r.Root(), "title")
	for _, pair := range []struct {
		span parser.Span
		want string
	}{{title.KeySpan(), "title"}, {title.Value().Span(), "\"雪 # title\""}, {title.Span(), "title = \"雪 # title\" # inline\n"}} {
		actual, _ := f.b.Source().Text(pair.span)
		if actual != pair.want {
			t.Errorf("source = %q, want %q", actual, pair.want)
		}
	}
}
func TestTOMLSemanticOwnershipAndSourceOrder(t *testing.T) {
	payload := "first = \"retained \\u96EA\"\n\"literal.dot\" = 1\na.x = 2\na.y = 3\n[parent.child]\nvalue = \"child\"\n[parent]\nname = \"parent\"\n[[products]]\nname = \"one\"\n[[products]]\nname = \"two\"\n"
	f, r := tomlDecoded(t, payload)
	if !r.Complete() {
		t.Fatalf("incomplete: %#v", r)
	}
	for i, want := range []string{"first", "literal.dot", "a", "parent", "products"} {
		e, _ := r.Root().KnownEntry(i)
		if e.Key() != want {
			t.Errorf("key %d=%q want %q", i, e.Key(), want)
		}
	}
	first := tomlFind(t, r.Root(), "first")
	text, _ := first.Value().Scalar()
	raw, _ := f.b.Source().Text(first.Value().Span())
	if text != "retained 雪" || raw != "\"retained \\u96EA\"" {
		t.Fatalf("ephemeral first expression lost %q %q", text, raw)
	}
	for _, key := range []string{"a", "parent", "products"} {
		e := tomlFind(t, r.Root(), key)
		if e.Span().Present() || e.Value().Span().Present() || e.KeySpan().Present() {
			t.Errorf("synthetic %s has authored span", key)
		}
	}
	products := tomlFind(t, r.Root(), "products").Value()
	if products.ElementCount() != 2 {
		t.Fatal("AOT element identity lost")
	}
	for i, want := range []string{"one", "two"} {
		e, _ := products.KnownElement(i)
		actual, _ := tomlFind(t, e, "name").Value().Scalar()
		if actual != want {
			t.Errorf("element %d=%q", i, actual)
		}
	}
	orders, err := r.Ordering(context.Background(), f.b, OrderOptions{})
	if err != nil || len(orders) != 1 || orders[0].MoveEligibility() != ReportOnly {
		t.Fatal("table movement authority", orders, err)
	}
	for _, bad := range []string{"a=1\n\"a\"=2\n", "a=1\n\"\\u0061\"=2\n", "[a]\nx=1\n[a]\ny=2\n", "a=1\n[a]\nx=2\n", "a={b=1}\na.c=2\n", "a=[]\n[a]\nx=1\n", "[[a]]\nb=1\n[a]\nc=2\n"} {
		t.Run(bad, func(t *testing.T) {
			f, r := tomlDecoded(t, bad)
			if r.Complete() || r.ProblemCount() == 0 {
				t.Fatalf("invalid TOML accepted %q", bad)
			}
			p, _ := r.KnownProblem(0)
			if p.Location.SourceToken() != f.b.Source().Token() || !within(f.b.Header().Content(), p.Location) {
				t.Fatal("unlocated invalid TOML")
			}
			title, err := r.Title(context.Background(), f.b)
			if err != nil || title.Known {
				t.Fatal("incomplete negative authority", title, err)
			}
		})
	}
}
func TestTOMLMultilineRangesAndEligibility(t *testing.T) {
	payload := "\"\\u0061\" = \"\"\"\n雪 # [ ignored\ntext\"\"\" # inline\nb = { x = \"#\", y = [1, 2] }\n"
	for _, ending := range []string{"\n", "\r\n"} {
		raw := "\ufeff+++" + ending + strings.ReplaceAll(payload, "\n", ending) + "+++"
		f := bindFixture(t, raw)
		r, err := Decode(context.Background(), f.b)
		if err != nil || !r.Complete() {
			t.Fatalf("decode %q: complete %v err %v", ending, r.Complete(), err)
		}
		a := tomlFind(t, r.Root(), "a")
		spanText, _ := f.b.Source().Text(a.Value().Span())
		want := "\"\"\"" + ending + "雪 # [ ignored" + ending + "text\"\"\""
		if spanText != want {
			t.Errorf("multiline source=%q want %q", spanText, want)
		}
		keyText, _ := f.b.Source().Text(a.KeySpan())
		if keyText != "\"\\u0061\"" {
			t.Fatal(keyText)
		}
		orders, err := r.Ordering(context.Background(), f.b, OrderOptions{})
		if err != nil || len(orders) != 1 || orders[0].MoveEligibility() != IndependentEntries {
			t.Fatalf("whole entries %v %v", orders, err)
		}
		if string(f.b.Source().Bytes()) != raw {
			t.Fatal("source changed")
		}
	}
	for _, tc := range []struct {
		payload     string
		eligibility MoveEligibility
	}{{"b=\"#\" # inline\na=1\n", IndependentEntries}, {"# standalone\nb=1\na=2\n", ReportOnly}, {"b.x=1\na=2\n", ReportOnly}} {
		f, r := tomlDecoded(t, tc.payload)
		orders, err := r.Ordering(context.Background(), f.b, OrderOptions{})
		if err != nil || len(orders) != 1 || orders[0].MoveEligibility() != tc.eligibility {
			t.Errorf("%q eligibility %v err %v", tc.payload, orders, err)
		}
	}
	_, r := tomlDecoded(t, "")
	if !r.Complete() || r.Root().Kind() != Mapping || r.Root().EntryCount() != 0 || !r.Root().Span().Present() {
		t.Fatal("empty header is not complete mapping")
	}
	for _, bad := range []string{"x=[\n", "date=2024+02-29\n", "date=2024-02-290\n", "time=07:32.0\n", "time=07:32:00.\n", "date=2023-02-29\n", "time=24:00:00\n", "offset=2024-01-01T00:00:00+24:00\n", "x=1\ry=2\n"} {
		_, r := tomlDecoded(t, bad)
		if r.Complete() || r.ProblemCount() == 0 {
			t.Errorf("invalid scalar/syntax accepted %q", bad)
		}
	}
}

func TestTOMLBeforeNativeDepthAdmission(t *testing.T) {
	for _, tc := range []struct {
		payload  string
		accepted bool
	}{
		{"a=[[1]]\n", true}, {"a=[[[1]]]\n", false}, {"a=[[[]]]\n", true}, {"a={b={c=1}}\n", true}, {"a={b={c={d=1}}}\n", false}, {"a.b.c=1\n", true}, {"a.b.c.d=1\n", false}, {"[a.b.c]\n", true}, {"[a.\"b.c\".d]\n", true}, {"[a.\"b.c\".d.e]\n", false}, {"[a.b.c.d]\n", false}, {"[a.b.c.d]\n?\n", false}, {"[[a.b]]\nx=1\n", false}, {"a='''quoted''''\nb=[[[?\n", false}, {"a='''quoted'''''\nb=[[[?\n", false}, {"a=\"\"\"quoted\"\"\"\"\nb=[[[?\n", false}, {"a=\"\"\"quoted\"\"\"\"\"\nb=[[[?\n", false}, {"a=\"escaped \\\\\"\nb=[[[?\n", false}, {"a=\"[[[ # {\"\n", true}, {"a='''quoted''''\nb=[[1]]\n", true}, {"a='''quoted'''''\nb=[[1]]\n", true}, {"a=\"\"\"quoted\"\"\"\"\nb=[[1]]\n", true}, {"a=\"\"\"quoted\"\"\"\"\"\nb=[[1]]\n", true},
		// Deliberately malformed after excessive nesting: admission must reject
		// before native syntax failure. Removing the guard yields Malformed,
		// independently of the later model's own depth validation.
		{"a=[[[?\n", false},
	} {
		t.Run(tc.payload, func(t *testing.T) {
			limits := parser.DefaultLimits()
			limits.SyntaxDepth = 4
			budget, err := parser.NewOperationBudget(limits)
			if err != nil {
				t.Fatal(err)
			}
			f := bindBudgetFixture(t, "+++\n"+tc.payload+"+++\n", budget, context.Background())
			r, err := Decode(context.Background(), f.b)
			var limit *parser.LimitExceeded
			if tc.accepted {
				if err != nil || !r.Complete() {
					t.Fatalf("boundary rejected complete=%v err=%v", r.Complete(), err)
				}
			} else if !errors.As(err, &limit) || limit.Name != "syntax-depth" || limit.Allowed != 4 {
				t.Fatalf("missing before-entry depth cause: result complete=%v err=%v", r.Complete(), err)
			}
		})
	}
}

func TestTOMLSignedZeroAndExactNumericPrecision(t *testing.T) {
	f, r := tomlDecoded(t, "negative = -0.0\npositive = +0.0\nprecise = 1.234567890123456789e3\nexponent = 1e+1000\nmin = -9223372036854775808\nover64 = 9223372036854775808\nbig = 184467440737095516160\nbinary = 0b1_000\noctal = 0o1_000\n")
	if !r.Complete() {
		t.Fatal("numeric fixture incomplete")
	}
	for _, want := range []struct {
		key       string
		kind      Kind
		text, raw string
	}{
		{"negative", Float, "-0e0", "-0.0"}, {"positive", Float, "0e0", "+0.0"}, {"precise", Float, "1234567890123456789e-15", "1.234567890123456789e3"}, {"exponent", Float, "1e1000", "1e+1000"}, {"min", Integer, "-9223372036854775808", "-9223372036854775808"}, {"over64", Integer, "9223372036854775808", "9223372036854775808"}, {"big", Integer, "184467440737095516160", "184467440737095516160"}, {"binary", Integer, "8", "0b1_000"}, {"octal", Integer, "512", "0o1_000"},
	} {
		e := tomlFind(t, r.Root(), want.key)
		text, _ := e.Value().Scalar()
		raw, _ := f.b.Source().Text(e.Value().Span())
		if e.Value().Kind() != want.kind || text != want.text || raw != want.raw {
			t.Errorf("%s: kind=%s text=%q raw=%q", want.key, e.Value().Kind(), text, raw)
		}
	}
	_, duplicate := tomlDecoded(t, "a = 1\n\"\\u0061\" = 2\n")
	problem, ok := duplicate.KnownProblem(0)
	if !ok || problem.Code != DuplicateKey {
		t.Fatal("decoded duplicate code", problem, ok)
	}
}

func TestTOML11NativeFeaturesAndAOTScope(t *testing.T) {
	f, r := tomlDecoded(t, "escapes = \"\\xFF\\e\"\nobject = {\n x = 1, # inline\n y = [2,],\n}\nshort = 2024-02-29t07:32z\n")
	if !r.Complete() {
		t.Fatal("TOML 1.1 fixture incomplete")
	}
	text, _ := tomlFind(t, r.Root(), "escapes").Value().Scalar()
	if text != "ÿ\x1b" {
		t.Errorf("escapes=%q", text)
	}
	text, _ = tomlFind(t, r.Root(), "short").Value().Scalar()
	if text != "2024-02-29T07:32:00Z" {
		t.Errorf("optional seconds=%q", text)
	}
	object := tomlFind(t, r.Root(), "object").Value()
	raw, _ := f.b.Source().Text(object.Span())
	if raw != "{\n x = 1, # inline\n y = [2,],\n}" || object.EntryCount() != 2 {
		t.Fatalf("inline table source=%q count=%d", raw, object.EntryCount())
	}
	limits := parser.DefaultLimits()
	limits.SyntaxDepth = 6
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	fixture := bindBudgetFixture(t, "+++\n[[a]]\n[[a.b]]\n[[a]]\n[a.b.c]\nx=1\n+++\n", budget, context.Background())
	result, err := Decode(context.Background(), fixture.b)
	if err != nil || !result.Complete() {
		t.Fatalf("new AOT element inherited prior element path: complete=%v err=%v", result.Complete(), err)
	}
}

func TestTOMLCivilLeapSecondPrecision(t *testing.T) {
	f, r := tomlDecoded(t, "offset = 2016-12-31T23:59:60Z\nlocal = 23:59:60\nprecise = 2016-12-31t23:59:60.123456789012300+00:00\n")
	if !r.Complete() {
		t.Fatalf("civil leap marker unsupported: problems=%d", r.ProblemCount())
	}
	for _, want := range []struct {
		key       string
		kind      Kind
		text, raw string
	}{
		{"offset", OffsetDateTime, "2016-12-31T23:59:60Z", "2016-12-31T23:59:60Z"},
		{"local", LocalTime, "23:59:60", "23:59:60"},
		{"precise", OffsetDateTime, "2016-12-31T23:59:60.1234567890123Z", "2016-12-31t23:59:60.123456789012300+00:00"},
	} {
		e := tomlFind(t, r.Root(), want.key)
		text, _ := e.Value().Scalar()
		raw, _ := f.b.Source().Text(e.Value().Span())
		if e.Value().Kind() != want.kind || text != want.text || raw != want.raw {
			t.Errorf("%s: kind=%s text=%q raw=%q", want.key, e.Value().Kind(), text, raw)
		}
	}
	for _, bad := range []string{"offset=2016-12-31T23:59:61Z\n", "local=23:59:61\n"} {
		_, r := tomlDecoded(t, bad)
		if r.Complete() || r.ProblemCount() == 0 {
			t.Errorf("invalid second accepted: %q", bad)
		}
	}
}
