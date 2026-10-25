package resource_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	r "github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
)

type inventoryFixture struct {
	reportFixture
	reader *r.SuppliedReader
	input  r.SuppliedInput
}

func newInventoryFixture(t *testing.T, ctx context.Context, raw, text string, extras ...r.SuppliedFile) inventoryFixture {
	t.Helper()
	f := newReportFixture(t, ctx, text)
	files := append([]r.SuppliedFile{{Key: mustKey(t, "docs/a.md"), Source: f.source}}, extras...)
	var data []byte
	if raw != "" {
		data = []byte(raw)
	}
	input, err := r.DecodeSupplied(ctx, f.operation.Budget(), r.DefaultInputLimits(), data, files)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := r.NewSuppliedReader(ctx, f.operation, input, r.DefaultInputLimits())
	if err != nil {
		t.Fatal(err)
	}
	return inventoryFixture{f, reader, input}
}

func mustKey(t *testing.T, name string) r.Key {
	t.Helper()
	key, err := r.NewKey(name)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func inventoryRequest(t *testing.T, f reportFixture, destination, rule string, capability r.Capability, required r.RequiredType) r.Request {
	t.Helper()
	start := strings.Index(f.text, destination)
	if start < 0 {
		t.Fatal("fixture destination absent", destination)
	}
	span, err := f.source.Span(start, start+len(destination))
	if err != nil {
		t.Fatal(err)
	}
	link, err := r.NewLink("docs/a.md", rule, destination, f.source, span)
	if err != nil {
		t.Fatal(err)
	}
	normalizer, err := r.NewNormalizer(f.view, r.DefaultPathLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	n, err := normalizer.Normalize(link)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := n.Location().Key()
	request, err := f.view.Request(r.RequestSpec{Key: key, Link: link, Class: n.Class(), Capability: capability, Required: required})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func inventoryObservation(t *testing.T, f inventoryFixture, q r.Request) r.Observation {
	t.Helper()
	spec, err := f.reader.Read(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	o, err := f.operation.Record(q, spec)
	if err != nil {
		t.Fatal(err, spec)
	}
	return o
}

func TestSuppliedInventoryLookupTruthTable(t *testing.T) {
	entries := `[{"path":"docs/empty","kind":"directory"},{"path":"docs/gone","kind":"missing"},{"path":"docs/private","kind":"unavailable"},{"path":"docs/file.md","kind":"file","content":"# Unicode 雪\r\n"},{"path":"docs/no-content.md","kind":"file"},{"path":"docs/empty.md","kind":"file","content":""},{"path":"docs/images/logo.png","kind":"file"},{"path":"docs/deep/a/b.md","kind":"file"},{"path":"ghost/absent.md","kind":"missing"}]`
	type row struct {
		name      string
		cap       r.Capability
		required  r.RequiredType
		existence r.Existence
		content   r.Availability
		problem   r.ProblemKind
		treatment r.Treatment
		bytes     string
	}
	rows := []row{
		{"empty", r.ExistenceOnly, r.DirectoryType, r.Directory, r.Inapplicable, r.NoProblem, r.Complete, ""},
		{"gone", r.ExistenceOnly, r.AnyType, r.Missing, r.Inapplicable, r.NoProblem, r.MissingTarget, ""},
		{"gone/child.md", r.TextContent, r.RegularFile, r.Missing, r.Inapplicable, r.NoProblem, r.MissingTarget, ""},
		{"file.md/child", r.TextContent, r.RegularFile, r.Missing, r.Inapplicable, r.NoProblem, r.MissingTarget, ""},
		{"private", r.ExistenceOnly, r.AnyType, r.Unknown, r.Unavailable, r.NotSupplied, r.ContextIncomplete, ""},
		{"private/child.md", r.TextContent, r.RegularFile, r.Unknown, r.Unavailable, r.NotSupplied, r.ContextIncomplete, ""},
		{"file.md", r.TextContent, r.RegularFile, r.File, r.Available, r.NoProblem, r.Complete, "# Unicode 雪\r\n"},
		{"file.md", r.ExistenceOnly, r.RegularFile, r.File, r.NotRequested, r.NoProblem, r.Complete, ""},
		{"empty.md", r.TextContent, r.RegularFile, r.File, r.Available, r.NoProblem, r.Complete, ""},
		{"no-content.md", r.ExistenceOnly, r.RegularFile, r.File, r.NotRequested, r.NoProblem, r.Complete, ""},
		{"no-content.md", r.TextContent, r.RegularFile, r.File, r.Unavailable, r.NotSupplied, r.ContextIncomplete, ""},
		{"images/logo.png", r.ExistenceOnly, r.RegularFile, r.File, r.NotRequested, r.NoProblem, r.Complete, ""},
		{"images/logo.png", r.ParsedAnchors, r.RegularFile, r.File, r.NotRequested, r.NotSupplied, r.ContextIncomplete, ""},
		{"images", r.ExistenceOnly, r.DirectoryType, r.Directory, r.Inapplicable, r.NoProblem, r.Complete, ""},
		{"deep/a", r.ExistenceOnly, r.DirectoryType, r.Directory, r.Inapplicable, r.NoProblem, r.Complete, ""},
		{"file.md", r.ExistenceOnly, r.DirectoryType, r.File, r.NotRequested, r.NoProblem, r.MissingTarget, ""},
	}
	for _, complete := range []bool{false, true} {
		for i, row := range rows {
			t.Run(fmt.Sprintf("complete=%v/%d/%s", complete, i, row.name), func(t *testing.T) {
				f := newInventoryFixture(t, context.Background(), fmt.Sprintf(`{"complete":%v,"entries":%s}`, complete, entries), "雪 [q]("+row.name+")\r\n")
				q := inventoryRequest(t, f.reportFixture, row.name, "MD057", row.cap, row.required)
				o := inventoryObservation(t, f, q)
				if o.Existence() != row.existence || o.ContentAvailability() != row.content || o.Problem().Kind != row.problem || o.Treatment() != row.treatment {
					t.Fatalf("lookup: %+v/%s expected %+v", o, o.Treatment(), row)
				}
				source, available := o.Content()
				if available != (row.content == r.Available) || available && string(source.Bytes()) != row.bytes {
					t.Fatal("exact content/presence lost", available, source)
				}
				if row.treatment == r.MissingTarget {
					q51 := inventoryRequest(t, f.reportFixture, row.name, "MD051", row.cap, row.required)
					if inventoryObservation(t, f, q51).Treatment() != r.InapplicableCheck {
						t.Fatal("missing/type responsibility transferred to MD051")
					}
				}
			})
		}
		for _, destination := range []string{"unlisted.md", "empty/unlisted.md", "../ghost"} {
			f := newInventoryFixture(t, context.Background(), fmt.Sprintf(`{"complete":%v,"entries":%s}`, complete, entries), "[q]("+destination+")\n")
			q := inventoryRequest(t, f.reportFixture, destination, "MD057", r.ExistenceOnly, r.AnyType)
			o := inventoryObservation(t, f, q)
			if complete && (o.Existence() != r.Missing || o.Treatment() != r.MissingTarget) || !complete && (o.Existence() != r.Unknown || o.Problem().Kind != r.PartialInventory) {
				t.Fatal("omission/negative parent invented names or absence", destination, complete, o)
			}
		}
	}
}

func TestSuppliedInventoryOmissionExcludedSeedsAndIsolation(t *testing.T) {
	for _, raw := range []string{"", `{"entries":[]}`, `{"complete":false,"entries":[]}`} {
		extra := r.SuppliedFile{Key: mustKey(t, "docs/excluded.md"), Source: parser.NewSource([]byte("# excluded original\r\n"))}
		f := newInventoryFixture(t, context.Background(), raw, "[q](excluded.md) [u](unknown.md)\n", extra)
		q := inventoryRequest(t, f.reportFixture, "excluded.md", "MD051", r.TextContent, r.RegularFile)
		o := inventoryObservation(t, f, q)
		source, available := o.Content()
		if !available || source != extra.Source || len(f.operation.Targets()) != 1 {
			t.Fatal("excluded seed lost bytes or gained target authority")
		}
		// Copies returned by the strict input boundary cannot mutate reader state.
		files := f.input.Files()
		files[1].Source = parser.NewSource([]byte("caller changed copy"))
		if after, _ := inventoryObservation(t, f, q).Content(); after != extra.Source {
			t.Fatal("caller slice changed retained input")
		}
		out := f.result(t, false)
		if err := f.reader.AppendEffects(&out, f.view, []r.Observation{o}, nil); err != nil {
			t.Fatal(err)
		}
		engine.FinalizeLintResult(&out)
		if out.Status != "clean" || !out.Complete || len(out.Coverage) != 0 {
			t.Fatal("partial inventory alone emitted deficit", out)
		}
		assertResourceTargetsUnchanged(t, out)
		validateResourceResult(t, out)
		other := newInventoryFixture(t, context.Background(), `{"complete":true,"entries":[]}`, f.text)
		foreign := inventoryRequest(t, other.reportFixture, "excluded.md", "MD057", r.ExistenceOnly, r.AnyType)
		if _, err := f.reader.Read(context.Background(), foreign); !errors.Is(err, r.ErrContract) {
			t.Fatal("cross-call request accepted", err)
		}
		if inventoryObservation(t, other, foreign).Existence() != r.Missing {
			t.Fatal("other inventory inherited resource bytes")
		}
	}
	// A real host file is irrelevant even when its exact basename is requested.
	sentinelRoot := t.TempDir()
	t.Chdir(sentinelRoot)
	if err := os.Mkdir("docs", 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sentinelRoot, "docs", "host-only.md")
	if err := os.WriteFile(path, []byte("secret host bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, complete := range []bool{false, true} {
		f := newInventoryFixture(t, context.Background(), fmt.Sprintf(`{"complete":%v,"entries":[]}`, complete), "[q](host-only.md)\n")
		o := inventoryObservation(t, f, inventoryRequest(t, f.reportFixture, "host-only.md", "MD057", r.TextContent, r.RegularFile))
		if _, available := o.Content(); available || complete && o.Existence() != r.Missing || !complete && o.Existence() != r.Unknown {
			t.Fatal("inventory escaped to host", o)
		}
	}
}

func TestSuppliedInventoryLiteralNamesAndGitPolicy(t *testing.T) {
	names := []string{"CON", "name:stream", "trailing.", "trailing ", "é.md", "e\u0301.md", "line\u2028separator.md", "line\u2029separator.md", "a%2Fb.md", ".GIT/config"}
	entries := make([]map[string]string, 0, len(names)+1)
	for _, name := range names {
		entries = append(entries, map[string]string{"path": "docs/" + name, "kind": "file", "content": name})
	}
	entries = append(entries, map[string]string{"path": "docs/.git/config", "kind": "file", "content": "blocked"})
	raw, err := json.Marshal(map[string]any{"complete": true, "entries": entries})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range append(names, ".git/config") {
		f := newInventoryFixture(t, context.Background(), string(raw), "[q]("+name+")\n")
		// Constructing a request from a literal key here intentionally bypasses
		// URL syntax: the inventory domain itself has no native alias policy.
		q := f.request(t, name, false, r.TextContent)
		key := mustKey(t, "docs/"+name)
		q, err = f.view.Request(r.RequestSpec{Key: key, Link: q.Link(), Class: r.Local, Capability: r.TextContent, Required: r.RegularFile})
		if err != nil {
			t.Fatal(err)
		}
		o := inventoryObservation(t, f, q)
		if name == ".git/config" {
			if o.Existence() != r.Unknown || o.Problem().Kind != r.Blocked {
				t.Fatal("Git metadata was readable", o)
			}
		} else if source, ok := o.Content(); !ok || string(source.Bytes()) != name {
			t.Fatal("supplied name inherited a native alias policy", name, o)
		}
	}
}

func TestSuppliedInventoryVirtualRootAndOneIndex(t *testing.T) {
	f := newInventoryFixture(t, context.Background(), `{"entries":[{"path":"index.md","kind":"file","content":"# root\n"}]}`, "[q](../)\n")
	span, _ := f.source.Span(4, 7)
	link, err := r.NewLink("docs/a.md", "MD051", "../", f.source, span)
	if err != nil {
		t.Fatal(err)
	}
	normalizer, err := r.NewNormalizer(f.view, r.DefaultPathLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	n, err := normalizer.Normalize(link)
	if err != nil || !n.Location().IsRootDirectory() {
		t.Fatal("root decision lost", n, err)
	}
	if existence, err := f.reader.RootDirectory(context.Background(), n.Location()); err != nil || existence != r.Directory {
		t.Fatal("virtual root unavailable", existence, err)
	}
	if _, err := f.reader.RootDirectory(context.Background(), r.Location{}); !errors.Is(err, r.ErrContract) {
		t.Fatal("zero root admitted", err)
	}
	other := newInventoryFixture(t, context.Background(), "", f.text)
	if _, err := other.reader.RootDirectory(context.Background(), n.Location()); !errors.Is(err, r.ErrContract) {
		t.Fatal("foreign root admitted", err)
	}
	index, err := n.DirectoryIndex()
	if err != nil {
		t.Fatal(err)
	}
	key, ok := index.Location().Key()
	if !ok || key.String() != "index.md" || index.RequiredType() != r.RegularFile {
		t.Fatal("root index plan invalid", index)
	}
	q, err := f.view.Request(r.RequestSpec{Key: key, Link: link, Class: index.Class(), Required: index.RequiredType(), Capability: r.TextContent})
	if err != nil {
		t.Fatal(err)
	}
	if source, ok := inventoryObservation(t, f, q).Content(); !ok || string(source.Bytes()) != "# root\n" {
		t.Fatal("one-level root index unavailable")
	}
}

func TestSuppliedInventorySelectedViewsAndSeedAuthority(t *testing.T) {
	f := newReportFixture(t, context.Background(), "[b](b.txt) [e](excluded.md)\r\n")
	bKey, aKey := mustKey(t, "docs/b.txt"), mustKey(t, "docs/a.md")
	bOriginal, excluded := parser.NewSource([]byte("# original b\n")), parser.NewSource([]byte("# excluded\r\n"))
	input, err := r.DecodeSupplied(context.Background(), f.operation.Budget(), r.DefaultInputLimits(), nil, []r.SuppliedFile{{Key: aKey, Source: f.source}, {Key: bKey, Source: bOriginal}, {Key: mustKey(t, "docs/excluded.md"), Source: excluded}})
	if err != nil {
		t.Fatal(err)
	}
	op, err := r.NewOperation(context.Background(), f.operation.Budget(), f.operation.Root(), f.operation.Settings(), []r.Target{{ID: "docs/a.md", Source: f.source, ResourceKey: aKey}, {ID: "second-report-id", Source: bOriginal, ResourceKey: bKey}})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := r.NewSuppliedReader(context.Background(), op, input, r.DefaultInputLimits())
	if err != nil {
		t.Fatal(err)
	}
	candidate, saved := parser.NewSource([]byte("# candidate b\n")), parser.NewSource([]byte("# saved b\r\n"))
	for _, row := range []struct {
		phase       r.Phase
		replacement []r.Replacement
		expected    parser.Source
		unavailable bool
	}{{r.Original, nil, bOriginal, false}, {r.Candidate, []r.Replacement{{TargetID: "second-report-id", Source: candidate}}, candidate, false}, {r.Saved, []r.Replacement{{TargetID: "second-report-id", Source: saved}}, saved, false}, {r.Saved, []r.Replacement{{TargetID: "second-report-id", Unavailable: true}}, parser.Source{}, true}} {
		view, err := op.NewView(row.phase, row.replacement)
		if err != nil {
			t.Fatal(err)
		}
		f.operation, f.view = op, view
		fixture := inventoryFixture{f, reader, input}
		q := inventoryRequest(t, f, "b.txt", "MD051", r.ParsedAnchors, r.RegularFile)
		o := inventoryObservation(t, fixture, q)
		if row.unavailable {
			if o.Existence() != r.Unknown || o.Problem().Kind != r.Blocked {
				t.Fatal("unavailable saved resource fell back", o)
			}
		} else if source, ok := o.Content(); !ok || source != row.expected {
			t.Fatal("selected view did not override original seed", row.phase, o)
		}
		q = inventoryRequest(t, f, "excluded.md", "MD051", r.TextContent, r.RegularFile)
		if source, ok := inventoryObservation(t, fixture, q).Content(); !ok || source != excluded || len(op.Targets()) != 2 {
			t.Fatal("excluded seed changed view or target set")
		}
	}
	for _, source := range []parser.Source{parser.NewSource(f.source.Bytes()), excluded} {
		bad, err := r.NewOperation(context.Background(), op.Budget(), op.Root(), op.Settings(), []r.Target{{ID: "docs/a.md", Source: source, ResourceKey: aKey}})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := r.NewSuppliedReader(context.Background(), bad, input, r.DefaultInputLimits()); got != nil || !errors.Is(err, r.ErrContract) {
			t.Fatal("foreign/equal-byte source minted selected authority", got, err)
		}
	}
}

func TestSuppliedInventoryBoundsCancellationAndConcurrentReads(t *testing.T) {
	f := newInventoryFixture(t, context.Background(), `{"entries":[{"path":"docs/deep/shared/a.md","kind":"file"},{"path":"docs/deep/shared/b.md","kind":"file"}]}`, "[q](deep/shared/a.md)\n")
	limits := r.DefaultInputLimits()
	limits.TargetFiles, limits.Identities = 1, 5 // 3 explicit + docs/deep/shared = 6, rejected before retention.
	if reader, err := r.NewSuppliedReader(context.Background(), f.operation, f.input, limits); reader != nil || err == nil {
		t.Fatal("inferred identity cap did not reject unpublished index", reader, err)
	} else {
		var input *r.InputError
		if !errors.As(err, &input) || input.Limit == nil || input.Limit.Name != "resource-identities" {
			t.Fatal("known inference excess lost preflight identity", err)
		}
	}
	limits.Identities = 6 // docs, docs/deep and docs/deep/shared are each retained once.
	if _, err := r.NewSuppliedReader(context.Background(), f.operation, f.input, limits); err != nil {
		t.Fatal("shared parents charged duplicate identities", err)
	}
	q := inventoryRequest(t, f.reportFixture, "deep/shared/a.md", "MD057", r.ExistenceOnly, r.RegularFile)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 12 {
				spec, err := f.reader.Read(context.Background(), q)
				if err != nil || spec.Existence != r.File {
					t.Error("immutable concurrent lookup", spec, err)
				}
			}
		})
	}
	wg.Wait()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if reader, err := r.NewSuppliedReader(ctx, f.operation, f.input, r.DefaultInputLimits()); reader != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("constructor ignored caller stop", reader, err)
	}
	if spec, err := f.reader.Read(ctx, q); !errors.Is(err, context.Canceled) || spec.Existence != r.Unknown || spec.Problem.Kind != r.Cancelled {
		t.Fatal("lookup invented facts after caller stop", spec, err)
	}
	if err := f.operation.Budget().ChargeWork(f.operation.Budget().Limits().WorkUnits); err == nil {
		t.Fatal("test did not exhaust real shared budget")
	}
	if reader, err := r.NewSuppliedReader(context.Background(), f.operation, f.input, r.DefaultInputLimits()); reader != nil || err == nil {
		t.Fatal("new reader reset operation allowance", reader, err)
	}
	if _, err := f.reader.Read(context.Background(), q); err == nil {
		t.Fatal("lookup escaped sticky operation stop")
	}
	var zero r.SuppliedReader
	if _, err := zero.Read(context.Background(), q); !errors.Is(err, r.ErrContract) {
		t.Fatal("zero reader accepted", err)
	}
}

// Err causes an actual cancel on a chosen checkpoint; Done and all context
// behavior still come from that same real context. No fake Err-only stop exists.
type inventoryCancelContext struct {
	context.Context
	cancel context.CancelFunc
	at     int32
	calls  atomic.Int32
}

func (c *inventoryCancelContext) Err() error {
	if c.calls.Add(1) == c.at {
		c.cancel()
	}
	return c.Context.Err()
}

func TestSuppliedInventoryLateStopsPreserveKnownFactsAndFinalStatus(t *testing.T) {
	for _, row := range []struct {
		name      string
		existence r.Existence
		content   r.Availability
		treatment r.Treatment
	}{{"directory", r.Directory, r.Inapplicable, r.MissingTarget}, {"missing.md", r.Missing, r.Inapplicable, r.MissingTarget}, {"file.md", r.File, r.Available, r.Complete}, {"unknown.md", r.Unknown, r.Unavailable, r.ContextIncomplete}} {
		t.Run(row.name, func(t *testing.T) {
			f := newInventoryFixture(t, context.Background(), `{"entries":[{"path":"docs/directory","kind":"directory"},{"path":"docs/missing.md","kind":"missing"},{"path":"docs/file.md","kind":"file","content":"# acquired\r\n"}]}`, "雪 [q]("+row.name+")\r\n")
			q := inventoryRequest(t, f.reportFixture, row.name, "MD057", r.TextContent, r.RegularFile)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			at := int32(2)
			if row.name == "unknown.md" {
				at = 4 // entry miss, ancestor walk to root, then completion.
			}
			caller := &inventoryCancelContext{Context: ctx, cancel: cancel, at: at}
			spec, stop := f.reader.Read(caller, q)
			if !errors.Is(stop, context.Canceled) || spec.Existence != row.existence || spec.Content != row.content {
				t.Fatal("late stop erased established fact", spec, stop, caller.calls.Load())
			}
			select {
			case <-ctx.Done():
			default:
				t.Fatal("stop was not actual context cancellation")
			}
			o, err := f.operation.Record(q, spec)
			if err != nil || o.Treatment() != row.treatment {
				t.Fatal("known fact could not be recorded after stop", o, err)
			}
			out := f.result(t, false)
			if err := f.reader.AppendEffects(&out, f.view, []r.Observation{o}, []r.SuppliedStop{{Request: q, Err: stop}}); err != nil {
				t.Fatal(err)
			}
			engine.FinalizeLintResult(&out)
			if out.Status != "incomplete" || out.Complete || len(out.Interruptions) != 1 || len(out.Errors) != 0 || len(out.Diagnostics) != 0 || out.Interruptions[0].Reason != "cancelled" {
				t.Fatal("actual stop lost or invented IO/finding", out)
			}
			wantCoverage := 0
			if row.treatment == r.ContextIncomplete {
				wantCoverage = 1
			}
			if len(out.Coverage) != wantCoverage {
				t.Fatal("known missing/type/content gained false context deficit", out.Coverage)
			}
			if wantCoverage != 0 && (out.Coverage[0].Path != "docs/a.md" || out.Coverage[0].Snapshot == nil || *out.Coverage[0].Snapshot != *out.Files[0].InputSnapshot || out.Coverage[0].Range == nil || len(out.Coverage[0].CauseIDs) != 1 || out.Coverage[0].CauseIDs[0] != out.Interruptions[0].ID) {
				t.Fatal("late stop source/cause attribution lost", out.Coverage)
			}
			assertResourceTargetsUnchanged(t, out)
			validateResourceResult(t, out)
		})
	}
}

func TestSuppliedInventoryEffectsAdmissionAndResultReuse(t *testing.T) {
	f := newInventoryFixture(t, context.Background(), "", "雪 [one](one.md) [two](two.md) [three](three.md)\r\n")
	second, err := r.NewSuppliedReader(context.Background(), f.operation, f.input, r.DefaultInputLimits())
	if err != nil {
		t.Fatal(err)
	}
	out := f.result(t, false)
	out.Interruptions = []result.Interruption{{ID: "resource-stop-1", Reason: "cancelled", Phase: "acquisition", Message: "prior occupied ID"}, {ID: "resource-stop-17", Reason: "cancelled", Phase: "acquisition", Message: "prior sparse occupied ID"}}
	for i, name := range []string{"one.md", "two.md", "three.md"} {
		q := inventoryRequest(t, f.reportFixture, name, "MD051", r.TextContent, r.RegularFile)
		o := inventoryObservation(t, f, q)
		reader := f.reader
		if i == 1 {
			reader = second
		}
		if err := reader.AppendEffects(&out, f.view, []r.Observation{o}, []r.SuppliedStop{{Request: q, Err: context.Canceled}}); err != nil {
			t.Fatal(err)
		}
		lastCoverage := out.Coverage[len(out.Coverage)-1]
		lastCause := out.Interruptions[len(out.Interruptions)-1]
		if len(lastCoverage.CauseIDs) != 1 || lastCoverage.CauseIDs[0] != lastCause.ID {
			t.Fatal("interleaved reader reused stale cause identity", out)
		}
	}
	ids := map[string]bool{}
	for _, item := range out.Interruptions {
		if ids[item.ID] {
			t.Fatal("collision with sparse occupied/shared-reader causes", item)
		}
		ids[item.ID] = true
	}
	q := inventoryRequest(t, f.reportFixture, "one.md", "MD051", r.TextContent, r.RegularFile)
	o := inventoryObservation(t, f, q)
	for _, stops := range [][]r.SuppliedStop{{{Request: q, Err: errors.New("not a runtime stop")}}, {{Request: q, Err: nil}}, {{Request: q, Err: context.Canceled}, {Request: q, Err: context.Canceled}}} {
		before, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.reader.AppendEffects(&out, f.view, []r.Observation{o}, stops); !errors.Is(err, r.ErrContract) {
			t.Fatal("invalid batch accepted", err)
		}
		after, err := json.Marshal(out)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("rejected batch mutated result", err)
		}
	}
	for _, phase := range []r.Phase{r.Original, r.Candidate, r.Saved} {
		view, err := f.operation.NewView(phase, nil)
		if err != nil {
			t.Fatal(err)
		}
		f.view = view
		q := inventoryRequest(t, f.reportFixture, "one.md", "MD051", r.TextContent, r.RegularFile)
		o := inventoryObservation(t, f, q)
		out = f.result(t, false) // Deliberately reuse the same caller storage.
		if err := f.reader.AppendEffects(&out, view, []r.Observation{o}, []r.SuppliedStop{{Request: q, Err: context.Canceled}}); err != nil || len(out.Interruptions) != 1 || len(out.Coverage) != 1 || len(out.Coverage[0].CauseIDs) != 1 || out.Coverage[0].CauseIDs[0] != out.Interruptions[0].ID {
			t.Fatal("fresh result lost stop through reused caller storage", phase, out, err)
		}
	}
}

func TestSuppliedInventoryLowerBoundsAndRuntimeStopIdentity(t *testing.T) {
	f := newInventoryFixture(t, context.Background(), `{"entries":[{"path":"docs/deep/a.md","kind":"file"}]}`, "[q](long-unlisted-destination.md)\n")
	q := inventoryRequest(t, f.reportFixture, "long-unlisted-destination.md", "MD057", r.ExistenceOnly, r.AnyType)
	limits := r.DefaultInputLimits()
	limits.PathBytes = int64(len("docs/deep/a.md"))
	reader, err := r.NewSuppliedReader(context.Background(), f.operation, f.input, limits)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := reader.Read(context.Background(), q)
	var bound *parser.LimitExceeded
	var input *r.InputError
	if !errors.As(err, &bound) || bound.Name != "path-bytes" || errors.As(err, &input) || spec.Existence != r.Unknown || spec.Problem.Kind != r.Limited {
		t.Fatal("runtime path limit lost stop identity or invented absence", spec, err)
	}
	for _, mode := range []string{"caller during construction", "operation cancelled", "node quota"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			operationContext := context.Background()
			if mode == "operation cancelled" {
				operationContext = ctx
			}
			budgetLimits := parser.DefaultLimits()
			if mode == "node quota" {
				budgetLimits.TotalNodes, budgetLimits.DocumentNodes = 6, 6
			}
			budget, err := parser.NewOperationBudget(budgetLimits)
			if err != nil {
				t.Fatal(err)
			}
			op, err := r.NewOperation(operationContext, budget, f.operation.Root(), f.operation.Settings(), f.operation.Targets())
			if err != nil {
				t.Fatal(err)
			}
			caller := context.Background()
			switch mode {
			case "caller during construction":
				caller = &inventoryCancelContext{Context: ctx, cancel: cancel, at: 4}
			case "operation cancelled":
				cancel()
			}
			reader, err := r.NewSuppliedReader(caller, op, f.input, r.DefaultInputLimits())
			if reader != nil || err == nil || errors.As(err, &input) {
				t.Fatal("runtime stop published partial index or became input error", reader, err)
			}
			if _, controlled := r.InterruptionProblem(err); !controlled {
				t.Fatal("runtime error lost controlled identity", err)
			}
		})
	}
}

func TestSuppliedInventoryInputAdmissionAndNoImplicitSelection(t *testing.T) {
	f := newInventoryFixture(t, context.Background(), `{"entries":[]}`, "[q](missing.md)\n")
	// Resource construction does not select submitted/excluded files. The real
	// lint/transport producer still owns its existing all-excluded usage check.
	op, err := r.NewOperation(context.Background(), f.operation.Budget(), f.operation.Root(), f.operation.Settings(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if reader, err := r.NewSuppliedReader(context.Background(), op, f.input, r.DefaultInputLimits()); err != nil || reader == nil || len(op.Targets()) != 0 {
		t.Fatal("resource seed invented a selected target", reader, err)
	}
	project, err := r.NewRoot(r.ProjectRoot)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := r.NewOperation(context.Background(), f.operation.Budget(), project, f.operation.Settings(), f.operation.Targets())
	if err != nil {
		t.Fatal(err)
	}
	if reader, err := r.NewSuppliedReader(context.Background(), bad, f.input, r.DefaultInputLimits()); reader != nil || !errors.Is(err, r.ErrContract) {
		t.Fatal("supplied adapter accepted native authority", reader, err)
	}
	for _, limits := range []r.InputLimits{{}, {RawBytes: 1, TotalContentBytes: 1, TargetFiles: 1, Identities: 1, PathBytes: 1, PathDepth: 1}} {
		if reader, err := r.NewSuppliedReader(context.Background(), f.operation, f.input, limits); reader != nil || !errors.Is(err, r.ErrContract) {
			t.Fatal("invalid operator limits accepted", reader, err)
		}
	}
	foreign := newInventoryFixture(t, context.Background(), "", f.text)
	q := inventoryRequest(t, f.reportFixture, "missing.md", "MD051", r.TextContent, r.RegularFile)
	foreignQ := inventoryRequest(t, foreign.reportFixture, "missing.md", "MD051", r.TextContent, r.RegularFile)
	o := inventoryObservation(t, f, q)
	out := f.result(t, false)
	before, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, stops := range [][]r.SuppliedStop{{{Request: foreignQ, Err: context.Canceled}}, {{Request: r.Request{}, Err: context.Canceled}}} {
		if err := f.reader.AppendEffects(&out, f.view, []r.Observation{o}, stops); !errors.Is(err, r.ErrContract) {
			t.Fatal("foreign/zero stop carrier accepted", err)
		}
		after, err := json.Marshal(out)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("rejected foreign carrier mutated output")
		}
	}
	if err := f.reader.AppendEffects(&out, f.view, []r.Observation{o, o}, nil); !errors.Is(err, r.ErrContract) {
		t.Fatal("duplicate observation accepted", err)
	}
}

func TestSuppliedInventoryEffectsAcrossViewsKeepSourceCauseRelations(t *testing.T) {
	f := newInventoryFixture(t, context.Background(), "", "雪 [q](absent.md)\r\n")
	out := f.result(t, false)
	type want struct {
		location result.Range
		snapshot result.Snapshot
		reason   string
	}
	var expected []want
	for i, phase := range []r.Phase{r.Original, r.Candidate, r.Saved} {
		text := strings.Repeat("雪", i+1) + " [q](absent.md)\r\n"
		source := f.source
		var replacements []r.Replacement
		if phase != r.Original {
			source = parser.NewSource([]byte(text))
			replacements = []r.Replacement{{TargetID: "docs/a.md", Source: source}}
		}
		view, err := f.operation.NewView(phase, replacements)
		if err != nil {
			t.Fatal(err)
		}
		f.view, f.source, f.text = view, source, text
		q := inventoryRequest(t, f.reportFixture, "absent.md", "MD051", r.TextContent, r.RegularFile)
		o := inventoryObservation(t, f, q)
		stops := []error{context.Canceled, context.DeadlineExceeded, &parser.LimitExceeded{Name: "work-units", Observed: 1001, Allowed: 1000}}
		reasons := []string{"cancelled", "deadline-exceeded", "limit-exceeded"}
		if err := f.reader.AppendEffects(&out, view, []r.Observation{o}, []r.SuppliedStop{{Request: q, Err: stops[i]}}); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(source.Bytes())
		prefix := text[:strings.Index(text, "absent.md")]
		startColumn := utf8.RuneCountInString(prefix) + 1
		expected = append(expected, want{location: result.Range{Start: parser.Position{Line: 1, Column: startColumn}, End: parser.Position{Line: 1, Column: startColumn + len("absent.md")}}, snapshot: result.Snapshot{View: string(phase), SHA256: fmt.Sprintf("%x", hash), SizeBytes: len(text)}, reason: reasons[i]})
	}
	assert := func() {
		t.Helper()
		if len(out.Coverage) != 3 || len(out.Interruptions) != 3 || len(out.Errors) != 0 {
			t.Fatal("view batches lost independent effects", out)
		}
		causes := map[string]string{}
		for _, stop := range out.Interruptions {
			if stop.ID == "" || causes[stop.ID] != "" {
				t.Fatal("empty/duplicate cause ID", stop)
			}
			causes[stop.ID] = stop.Reason
		}
		for _, want := range expected {
			matched := false
			for _, coverage := range out.Coverage {
				if coverage.Snapshot != nil && *coverage.Snapshot == want.snapshot && coverage.Range != nil && *coverage.Range == want.location && coverage.Path == "docs/a.md" && coverage.Rule == "MD051" {
					matched = len(coverage.CauseIDs) == 1 && causes[coverage.CauseIDs[0]] == want.reason
				}
			}
			if !matched {
				t.Fatal("coverage borrowed another view's range, snapshot or stop", want, out.Coverage)
			}
		}
	}
	assert()
	engine.FinalizeLintResult(&out)
	assert()
	if out.Status != "incomplete" || out.Complete {
		t.Fatal("stopped batches became complete", out)
	}
	assertResourceTargetsUnchanged(t, out)
	validateResourceResult(t, out)
}

func TestSuppliedInventoryGitSelectedSourceStaysSelfOnly(t *testing.T) {
	f := newReportFixture(t, context.Background(), "[self](a.md) [other](.git/a.md)\n")
	ordinary, metadata := mustKey(t, "docs/start.md"), mustKey(t, "docs/.git/a.md")
	for _, self := range []bool{false, true} {
		key := ordinary
		if self {
			key = metadata
		}
		files := []r.SuppliedFile{{Key: metadata, Source: f.source}}
		targets := []r.Target{{ID: "docs/a.md", Source: f.source, ResourceKey: key}}
		if !self {
			files = append(files, r.SuppliedFile{Key: ordinary, Source: f.source})
			targets = append(targets, r.Target{ID: "metadata-report", Source: f.source, ResourceKey: metadata})
		}
		input, err := r.DecodeSupplied(context.Background(), f.operation.Budget(), r.DefaultInputLimits(), nil, files)
		if err != nil {
			t.Fatal(err)
		}
		op, err := r.NewOperation(context.Background(), f.operation.Budget(), f.operation.Root(), f.operation.Settings(), targets)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := r.NewSuppliedReader(context.Background(), op, input, r.DefaultInputLimits())
		if err != nil {
			t.Fatal(err)
		}
		view, err := op.NewView(r.Original, nil)
		if err != nil {
			t.Fatal(err)
		}
		f.operation, f.view = op, view
		destination := ".git/a.md"
		if self {
			destination = "a.md"
		}
		q := inventoryRequest(t, f, destination, "MD051", r.TextContent, r.RegularFile)
		o := inventoryObservation(t, inventoryFixture{f, reader, input}, q)
		if self {
			if source, available := o.Content(); !available || source != f.source {
				t.Fatal("selected source lost independent self authority", o)
			}
		} else if o.Existence() != r.Unknown || o.Problem().Kind != r.Blocked {
			t.Fatal("another selected snapshot granted cross-file Git access", o)
		}
	}
}
