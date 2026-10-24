package resource_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"sync"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
	r "github.com/frithuz/mdchk/internal/resource"
)

func suppliedBudget(t *testing.T) *parser.OperationBudget {
	t.Helper()
	b, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func decodeSupplied(t *testing.T, raw string, files []r.SuppliedFile) (r.SuppliedInput, error) {
	t.Helper()
	return r.DecodeSupplied(context.Background(), suppliedBudget(t), r.DefaultInputLimits(), []byte(raw), files)
}
func TestSuppliedInputFactsAndOwnership(t *testing.T) {
	key, _ := r.NewKey("excluded.md")
	original := parser.NewSource([]byte("excluded\r\n雪"))
	files := []r.SuppliedFile{{Key: key, Source: original}}
	raw := []byte(`{"complete":true,"entries":[{"path":"empty.md","kind":"file","content":""},{"path":"a%20b.md","kind":"file"},{"path":"folder","kind":"directory"},{"path":"gone","kind":"missing"},{"path":"private","kind":"unavailable"},{"path":"replacement-�","kind":"file","content":"雪\r\n"}]}`)
	input, err := r.DecodeSupplied(context.Background(), suppliedBudget(t), r.DefaultInputLimits(), raw, files)
	if err != nil {
		t.Fatal(err)
	}
	for i := range raw {
		raw[i] = 'x'
	}
	files[0] = r.SuppliedFile{}
	if !input.Present() || !input.Complete() || len(input.Entries()) != 6 || input.Files()[0].Source != original {
		t.Fatalf("lost facts: %+v", input)
	}
	entries := input.Entries()
	empty, ok := entries[0].Content()
	if !ok || empty == (parser.Source{}) || empty.Len() != 0 {
		t.Fatal("empty file was omitted")
	}
	if _, ok := entries[1].Content(); ok || entries[1].Key().String() != "a%20b.md" {
		t.Fatal("literal name/content presence lost")
	}
	text, ok := entries[5].Content()
	if !ok || string(text.Bytes()) != "雪\r\n" {
		t.Fatal("changed exact Unicode/line endings")
	}
	for i, want := range []r.EntryKind{r.EntryFile, r.EntryFile, r.EntryDirectory, r.EntryMissing, r.EntryUnavailable, r.EntryFile} {
		if entries[i].Kind() != want {
			t.Fatal(i, entries[i])
		}
	}
	entries[0] = r.SuppliedEntry{}
	out := input.Files()
	out[0] = r.SuppliedFile{}
	body := text.Bytes()
	body[0] = 'x'
	if input.Entries()[0].Key().String() != "empty.md" || input.Files()[0].Source != original || string(text.Bytes()) != "雪\r\n" {
		t.Fatal("exposed mutable storage")
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 50 {
				if len(input.Entries()) != 6 || input.Files()[0].Source != original {
					t.Error("concurrent immutable read changed")
				}
			}
		})
	}
	wg.Wait()
	omitted, err := r.DecodeSupplied(context.Background(), suppliedBudget(t), r.DefaultInputLimits(), nil, []r.SuppliedFile{{Key: key, Source: original}})
	if err != nil || omitted.Present() || omitted.Complete() || len(omitted.Files()) != 1 || len(omitted.Entries()) != 0 {
		t.Fatal(omitted, err)
	}
	partial, err := decodeSupplied(t, `{"entries":[]}`, nil)
	if err != nil || !partial.Present() || partial.Complete() {
		t.Fatal(partial, err)
	}
	// Input validation neither registers a selected target nor acquires unknown
	// names. Such lookup/outcome derivation belongs to #252.
}
func TestSuppliedClosedInputAndRawValidation(t *testing.T) {
	cases := []struct{ raw, path string }{
		{`null`, "resources"}, {``, "resources"}, {`[]`, "resources"}, {`{}`, "resources.entries"}, {`{"entries":null}`, "resources.entries"},
		{`{"entries":[],"complete":1}`, "resources.complete"}, {`{"entries":[],"limit":99}`, "resources.limit"},
		{`{"entries":[{"path":"x"}]}`, "resources.entries[0].kind"}, {`{"entries":[{"kind":"file"}]}`, "resources.entries[0].path"},
		{`{"entries":[{"path":"x","kind":"other"}]}`, "resources.entries[0].kind"},
		{`{"entries":[{"path":"x","kind":"file","content":null}]}`, "resources.entries[0].content"},
		{`{"entries":[{"path":"x","kind":"directory","content":""}]}`, "resources.entries[0].content"},
		{`{"entries":[{"path":"x","kind":"missing","content":""}]}`, "resources.entries[0].content"},
		{`{"entries":[{"path":"x","kind":"unavailable","content":""}]}`, "resources.entries[0].content"},
		{`{"entries":[{"path":"x","kind":"file","extra":""}]}`, "resources.entries[0].extra"},
		{`{"entries":[],"\u0065ntries":[]}`, "resources.entries"},
		{`{"entries":[{"path":"x","kind":"file","content":"\ud800"}]}`, "resources.entries[0].content"},
		{`{"entries":[{"path":"x","kind":"file","content":"\udc00"}]}`, "resources.entries[0].content"},
		{"{\"entries\":[{\"path\":\"x\",\"kind\":\"file\",\"content\":\"\xff\"}]}", "resources"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			_, err := decodeSupplied(t, tc.raw, nil)
			var e *r.InputError
			if !errors.As(err, &e) || e.Path != tc.path {
				t.Fatalf("wanted located %s input failure, got %v", tc.path, err)
			}
		})
	}
	for _, name := range []string{".", "..", "/x", "x/", "x//y", "x/./y", "x/../y", "x\\y", "C:x", "x\x00", "x\u0085"} {
		raw, _ := json.Marshal(map[string]any{"entries": []any{map[string]any{"path": name, "kind": "file"}}})
		if _, err := decodeSupplied(t, string(raw), nil); err == nil {
			t.Fatal("accepted bad literal name", name)
		}
	}
	for _, name := range []string{"Guide.md", "guide.md", "é.md", "e\u0301.md", "a%20b.md", "a b.md", "x/a:b.md"} {
		raw, _ := json.Marshal(map[string]any{"entries": []any{map[string]any{"path": name, "kind": "file"}}})
		input, err := decodeSupplied(t, string(raw), nil)
		if err != nil || input.Entries()[0].Key().String() != name {
			t.Fatal(name, err)
		}
	}
}
func TestSuppliedDeclarationConflicts(t *testing.T) {
	for _, kind := range []string{"file", "missing", "unavailable"} {
		for _, reverse := range []bool{false, true} {
			a, b := map[string]any{"path": "a", "kind": kind}, map[string]any{"path": "a/b/c", "kind": "file"}
			if reverse {
				a, b = b, a
			}
			raw, _ := json.Marshal(map[string]any{"entries": []any{a, b}})
			if _, err := decodeSupplied(t, string(raw), nil); err == nil {
				t.Fatal(kind, reverse, "contradictory ancestor accepted")
			}
		}
	}
	key, _ := r.NewKey("a")
	files := []r.SuppliedFile{{Key: key, Source: parser.NewSource(nil)}}
	for _, raw := range []string{`{"entries":[{"path":"a","kind":"file"}]}`, `{"entries":[{"path":"a/b","kind":"file"}]}`, `{"entries":[{"path":"x","kind":"file"},{"path":"x","kind":"file"}]}`} {
		if _, err := decodeSupplied(t, raw, files); err == nil {
			t.Fatal("accepted conflict", raw)
		}
	}
	if _, err := decodeSupplied(t, `{"entries":[]}`, append(files, files...)); err == nil {
		t.Fatal("duplicate files")
	}
	if _, err := decodeSupplied(t, `{"entries":[{"path":"folder","kind":"directory"},{"path":"folder/b","kind":"file"}]}`, nil); err != nil {
		t.Fatal(err)
	}
}
func TestSuppliedPreflightAndSharedStops(t *testing.T) {
	cases := []struct {
		name, raw string
		mutate    func(*r.InputLimits)
		files     []r.SuppliedFile
	}{
		{"request-bytes", `{"entries":[]}`, func(l *r.InputLimits) { l.RawBytes = 2 }, nil},
		{"resource-identities", `{"entries":[{"path":"a","kind":"file"},{"path":"b","kind":"file"}]}`, func(l *r.InputLimits) { l.TargetFiles = 1; l.Identities = 1 }, nil},
		{"path-bytes", `{"entries":[{"path":"long","kind":"file"}]}`, func(l *r.InputLimits) { l.PathBytes = 3 }, nil},
		{"path-depth", `{"entries":[{"path":"a/b","kind":"file"}]}`, func(l *r.InputLimits) { l.PathDepth = 1 }, nil},
	}
	for _, tc := range cases {
		l := r.DefaultInputLimits()
		tc.mutate(&l)
		_, err := r.DecodeSupplied(context.Background(), suppliedBudget(t), l, []byte(tc.raw), tc.files)
		var e *r.InputError
		if !errors.As(err, &e) || e.Limit == nil || e.Limit.Name != tc.name || e.Limit.Observed <= e.Limit.Allowed {
			t.Fatal(tc.name, err)
		}
	}
	// Count all submitted targets (including later excluded snapshots), and their
	// content together with resource payloads. Exact boundaries remain accepted.
	key, _ := r.NewKey("excluded")
	files := []r.SuppliedFile{{Key: key, Source: parser.NewSource([]byte("1234"))}}
	pl := parser.DefaultLimits()
	pl.DocumentBytes = 4
	makeBudget := func() *parser.OperationBudget {
		b, err := parser.NewOperationBudget(pl)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	l := r.DefaultInputLimits()
	l.TotalContentBytes = 6
	for _, tc := range []struct{ raw, name string }{{`{"entries":[{"path":"a","kind":"file","content":"12345"}]}`, "document-bytes"}, {`{"entries":[{"path":"a","kind":"file","content":"123"}]}`, "total-content-bytes"}} {
		_, err := r.DecodeSupplied(context.Background(), makeBudget(), l, []byte(tc.raw), files)
		var e *r.InputError
		if !errors.As(err, &e) || e.Limit == nil || e.Limit.Name != tc.name {
			t.Fatal(tc.name, err)
		}
	}
	if _, err := r.DecodeSupplied(context.Background(), makeBudget(), l, []byte(`{"entries":[{"path":"a","kind":"file","content":"12"}]}`), files); err != nil {
		t.Fatal(err)
	}
	l = r.DefaultInputLimits()
	l.TargetFiles = 1
	if _, err := r.DecodeSupplied(context.Background(), suppliedBudget(t), l, nil, append(files, files...)); err == nil {
		t.Fatal("excluded target count ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.DecodeSupplied(ctx, suppliedBudget(t), r.DefaultInputLimits(), nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	pl = parser.DefaultLimits()
	pl.WorkUnits = 20
	b, err := parser.NewOperationBudget(pl)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		_, err = r.DecodeSupplied(context.Background(), b, r.DefaultInputLimits(), []byte(`{"entries":[]}`), nil)
		var limit *parser.LimitExceeded
		var input *r.InputError
		if !errors.As(err, &limit) || errors.As(err, &input) {
			t.Fatal("runtime quota reclassified or reset", err)
		}
	}
}
func TestSuppliedSchemaContract(t *testing.T) {
	python := os.Getenv("MDCHK_RESULT_VALIDATOR_PYTHON")
	if python == "" {
		return
	}
	cases := []struct {
		Raw   string
		Valid bool
	}{
		{`{"entries":[]}`, true}, {`{"complete":true,"entries":[{"path":"é.md","kind":"file","content":""},{"path":"a%20b","kind":"file"},{"path":"d","kind":"directory"},{"path":"m","kind":"missing"},{"path":"u","kind":"unavailable"}]}`, true},
		{`{}`, false}, {`null`, false}, {`{"entries":[],"extra":1}`, false}, {`{"entries":[],"complete":null}`, false}, {`{"entries":[{"path":"x","kind":"missing","content":""}]}`, false}, {`{"entries":[{"path":"a/../b","kind":"file"}]}`, false}, {`{"entries":[{"path":"/a","kind":"file"}]}`, false},
	}
	rawPaths, err := os.ReadFile("testdata/supplied-paths.json")
	if err != nil {
		t.Fatal(err)
	}
	var paths []struct {
		Path  string `json:"path"`
		Valid bool   `json:"valid"`
	}
	if err := json.Unmarshal(rawPaths, &paths); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"file", "directory", "missing", "unavailable"} {
		for _, path := range paths {
			raw, err := json.Marshal(map[string]any{"entries": []any{map[string]any{"path": path.Path, "kind": kind}}})
			if err != nil {
				t.Fatal(err)
			}
			cases = append(cases, struct {
				Raw   string
				Valid bool
			}{string(raw), path.Valid})
		}
	}
	payload, _ := json.Marshal(map[string]any{"schema": json.RawMessage(r.SuppliedSchema()), "cases": cases})
	script := `import json,sys
from jsonschema import Draft202012Validator
x=json.load(sys.stdin);Draft202012Validator.check_schema(x['schema']);v=Draft202012Validator(x['schema'])
for c in x['cases']:
 ok=v.is_valid(json.loads(c['Raw']))
 assert ok==c['Valid'],c
`
	command := exec.Command(python, "-B", "-c", script) //nolint:gosec // Explicit test interpreter executes a fixed schema-validation script.
	command.Stdin = bytes.NewReader(payload)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("schema: %v\n%s", err, output)
	}
	for _, tc := range cases {
		_, err := decodeSupplied(t, tc.Raw, nil)
		if (err == nil) != tc.Valid {
			t.Fatal("schema/validator disagreement", tc, err)
		}
	}
	schema := r.SuppliedSchema()
	schema[0] = 'x'
	if r.SuppliedSchema()[0] != '{' {
		t.Fatal("schema storage mutable")
	}
}
func TestSuppliedDefaultsMatchAcceptedProfile(t *testing.T) {
	raw, err := os.ReadFile("../../docs/engineering/limits.json")
	if err != nil {
		t.Fatal(err)
	}
	var profile struct {
		Defaults map[string]struct {
			Value int64 `json:"value"`
		} `json:"defaults"`
	}
	if err := json.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}
	l := r.DefaultInputLimits()
	for name, n := range map[string]int64{"request-bytes": l.RawBytes, "total-content-bytes": l.TotalContentBytes, "target-files": l.TargetFiles, "resource-identities": l.Identities, "path-bytes": l.PathBytes, "path-depth": l.PathDepth} {
		if profile.Defaults[name].Value != n {
			t.Fatal(name, n, profile.Defaults[name])
		}
	}
	// A resource tool cannot supply operator limits, and the schema is closed.
	if _, err := decodeSupplied(t, `{"entries":[],"limits":{"request-bytes":999999}}`, nil); err == nil {
		t.Fatal("input raised limits")
	}
}

func TestSuppliedInvalidOperatorContracts(t *testing.T) {
	for _, mutate := range []func(*r.InputLimits){
		func(l *r.InputLimits) { l.RawBytes = 0 }, func(l *r.InputLimits) { l.PathDepth = -1 },
		func(l *r.InputLimits) { l.RawBytes = 1 << 31 }, func(l *r.InputLimits) { l.TotalContentBytes = 1 },
		func(l *r.InputLimits) { l.Identities = 1 },
	} {
		l := r.DefaultInputLimits()
		mutate(&l)
		if _, err := r.DecodeSupplied(context.Background(), suppliedBudget(t), l, nil, nil); !errors.Is(err, r.ErrContract) {
			t.Fatal(err)
		}
	}
	if _, err := r.DecodeSupplied(context.Background(), &parser.OperationBudget{}, r.DefaultInputLimits(), nil, nil); !errors.Is(err, r.ErrContract) {
		t.Fatal("zero budget accepted", err)
	}
	key, _ := r.NewKey("x")
	if _, err := decodeSupplied(t, `{"entries":[]}`, []r.SuppliedFile{{Key: key}}); err == nil {
		t.Fatal("missing supplied source")
	}
	if _, err := decodeSupplied(t, `{"entries":[]}`, []r.SuppliedFile{{Key: key, Source: parser.NewSource([]byte{255})}}); err == nil {
		t.Fatal("malformed submitted UTF8")
	}
}

// This fixture is also consumed by the required Node ECMAScript gate. Keep it
// independent of Go/JavaScript regex syntax: expected literal names are policy.
func TestSuppliedPathDialectParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/supplied-paths.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Path  string `json:"path"`
		Valid bool   `json:"valid"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"file", "directory", "missing", "unavailable"} {
		for _, fixture := range fixtures {
			raw, err := json.Marshal(map[string]any{"entries": []any{map[string]any{"path": fixture.Path, "kind": kind}}})
			if err != nil {
				t.Fatal(err)
			}
			input, err := decodeSupplied(t, string(raw), nil)
			if (err == nil) != fixture.Valid {
				t.Fatalf("kind=%s path=%q valid=%v: %v", kind, fixture.Path, fixture.Valid, err)
			}
			if fixture.Valid && input.Entries()[0].Key().String() != fixture.Path {
				t.Fatalf("changed literal path %q", fixture.Path)
			}
		}
	}
}
