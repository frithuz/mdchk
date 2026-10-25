package resource

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

type serviceFixture struct {
	service *Service
	reader  *SuppliedReader
	op      *Operation
	view    View
	source  parser.Source
}

func newServiceFixture(t *testing.T, ctx context.Context, raw string, limits ServiceLimits) serviceFixture {
	settings, _ := NewSettings(parser.ParseOptions{}, "index.md", "github", "service-v1")
	return newServiceFixturePolicy(t, ctx, raw, limits, settings)
}
func newServiceFixturePolicy(t *testing.T, ctx context.Context, raw string, limits ServiceLimits, settings Settings) serviceFixture {
	t.Helper()
	root, _ := NewRoot(SuppliedRoot)
	budget, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	source := parser.NewSource([]byte("# Source\n[x](a.md) [y](missing.md)\n"))
	key, _ := NewKey("source.md")
	op, err := NewOperation(ctx, budget, root, settings, []Target{{ID: "source.md", ResourceKey: key, Source: source}})
	if err != nil {
		t.Fatal(err)
	}
	var data []byte
	if raw != "" {
		data = []byte(raw)
	}
	input, err := DecodeSupplied(ctx, budget, DefaultInputLimits(), data, []SuppliedFile{{Key: key, Source: source}})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewSuppliedReader(ctx, op, input, DefaultInputLimits())
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewSuppliedService(reader, limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})
	view, err := op.NewView(Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	return serviceFixture{service, reader, op, view, source}
}
func serviceRequest(t *testing.T, f serviceFixture, name string, capability Capability, views ...View) Request {
	t.Helper()
	view := f.view
	if len(views) > 0 {
		view = views[0]
	}
	target, available, _ := view.Target("source.md")
	if !available {
		t.Fatal("source unavailable")
	}
	span, _ := target.Source.Span(0, 1)
	link, err := NewLink(target.ID, "MD057", name, target.Source, span)
	if err != nil {
		t.Fatal(err)
	}
	key, err := NewKey(name)
	if err != nil {
		t.Fatal(err)
	}
	q, err := view.Request(RequestSpec{Key: key, Link: link, Class: Local, Capability: capability, Required: RegularFile})
	if err != nil {
		t.Fatal(err)
	}
	return q
}
func serviceRecord(t *testing.T, f serviceFixture, q Request, spec ObservationSpec) Observation {
	t.Helper()
	o, err := f.op.Record(q, spec)
	if err != nil {
		t.Fatal("invalid cached spec", spec, err)
	}
	return o
}

func TestServiceSuppliedFirstFactsAndViewOverrides(t *testing.T) {
	f := newServiceFixture(t, context.Background(), `{"entries":[{"path":"a.md","kind":"file","content":"# first\r\n"},{"path":"image.bin","kind":"file"},{"path":"private","kind":"unavailable"}]}`, DefaultServiceLimits())
	calls := 0
	read := f.service.read
	f.service.read = func(ctx context.Context, q Request, p filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
		calls++
		return read(ctx, q, p)
	}
	for _, tt := range []struct {
		name    string
		cap     Capability
		exist   Existence
		content Availability
		kind    ProblemKind
	}{
		{"a.md", TextContent, File, Available, NoProblem}, {"missing.md", TextContent, Unknown, Unavailable, PartialInventory}, {"private/x.md", TextContent, Unknown, Unavailable, NotSupplied}, {"image.bin", ExistenceOnly, File, NotRequested, NoProblem},
	} {
		q := serviceRequest(t, f, tt.name, tt.cap)
		for range 3 {
			spec, err := f.service.Read(context.Background(), q)
			if err != nil || spec.Existence != tt.exist || spec.Content != tt.content || spec.Problem.Kind != tt.kind {
				t.Fatal(tt, spec, err)
			}
			serviceRecord(t, f, q, spec)
		}
	}
	if calls != 4 {
		t.Fatal("repeated facts reacquired", calls)
	}
}

func TestServiceSelectedViewsAndResourceOriginal(t *testing.T) {
	f := newServiceFixture(t, context.Background(), `{"entries":[{"path":"a.md","kind":"file","content":"original resource"}]}`, DefaultServiceLimits())
	candidateSource := parser.NewSource([]byte("# candidate\n"))
	candidate, err := f.op.NewView(Candidate, []Replacement{{TargetID: "source.md", Source: candidateSource}})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := f.op.NewView(Saved, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	read := f.service.read
	f.service.read = func(ctx context.Context, q Request, p filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
		calls++
		return read(ctx, q, p)
	}
	for _, view := range []View{f.view, candidate, saved} {
		q := serviceRequest(t, f, "a.md", TextContent, view)
		spec, err := f.service.Read(context.Background(), q)
		if err != nil || string(spec.Source.Bytes()) != "original resource" {
			t.Fatal(spec, err)
		}
		serviceRecord(t, f, q, spec)
	}
	if calls != 1 {
		t.Fatal("resource originals reopened per view", calls)
	}
	for _, tt := range []struct {
		view   View
		source parser.Source
	}{{f.view, f.source}, {candidate, candidateSource}, {saved, f.source}} {
		q := serviceRequest(t, f, "source.md", TextContent, tt.view)
		spec, err := f.service.Read(context.Background(), q)
		if err != nil || spec.Source != tt.source {
			t.Fatal("selected snapshot substituted", spec, err)
		}
		serviceRecord(t, f, q, spec)
	}
	if calls != 4 {
		t.Fatal("selected view entries not independent", calls)
	}
}

func TestServiceAdmissionUnionAndStickyBound(t *testing.T) {
	limits := DefaultServiceLimits()
	limits.Identities = 3
	limits.Lookups = 3
	f := newServiceFixture(t, context.Background(), `{"entries":[{"path":"a/b.md","kind":"file","content":"b"}]}`, limits)
	// source.md + a/b.md + inferred a: querying seeds does not add identities.
	calls := 0
	read := f.service.read
	f.service.read = func(ctx context.Context, q Request, p filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
		calls++
		return read(ctx, q, p)
	}
	seed := serviceRequest(t, f, "a/b.md", TextContent)
	spec, err := f.service.Read(context.Background(), seed)
	if err != nil || spec.Existence != File {
		t.Fatal(spec, err)
	}
	if len(f.service.identities) != 3 || len(f.service.lookups) != 1 {
		t.Fatal("union counted seeds twice")
	}
	miss := serviceRequest(t, f, "not-present.md", ExistenceOnly)
	spec, err = f.service.Read(context.Background(), miss)
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || limit.Name != "resource-identities" || spec.Existence == Missing {
		t.Fatal("cap became absence", spec, err)
	}
	spec, err = f.service.Read(context.Background(), seed)
	if !errors.As(err, &limit) || spec.Content != Available || calls != 1 {
		t.Fatal("saturation forgot retained fact", spec, err, calls)
	}
	same, err := NewSuppliedService(f.reader, limits)
	if err != nil || same != f.service {
		t.Fatal("wrapper reset service", err)
	}
	other := limits
	other.Lookups++
	if _, err := NewSuppliedService(f.reader, other); !errors.Is(err, ErrContract) {
		t.Fatal("policy reset accepted", err)
	}
	rebuilt, err := NewSuppliedReader(context.Background(), f.op, SuppliedInput{}, DefaultInputLimits())
	if err == nil {
		if _, err := NewSuppliedService(rebuilt, limits); !errors.Is(err, ErrContract) {
			t.Fatal("adapter reset accepted")
		}
	}
}

func TestServiceDistinctLookupBoundKeepsMiss(t *testing.T) {
	limits := DefaultServiceLimits()
	limits.Lookups = 1
	f := newServiceFixture(t, context.Background(), `{"complete":true,"entries":[]}`, limits)
	first := serviceRequest(t, f, "miss.md", ExistenceOnly)
	second := serviceRequest(t, f, "second.md", ExistenceOnly)
	calls := 0
	read := f.service.read
	f.service.read = func(ctx context.Context, q Request, p filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
		calls++
		return read(ctx, q, p)
	}
	spec, err := f.service.Read(context.Background(), first)
	if err != nil || spec.Existence != Missing {
		t.Fatal(spec, err)
	}
	spec, err = f.service.Read(context.Background(), second)
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || limit.Name != "resource-lookups" || spec.Existence == Missing {
		t.Fatal(spec, err)
	}
	spec, err = f.service.Read(context.Background(), first)
	if !errors.As(err, &limit) || spec.Existence != Missing || calls != 1 {
		t.Fatal("miss evicted/reopened", spec, err, calls)
	}
}

func TestServiceCoalescesAndCallerCancellationDoesNotPoison(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newServiceFixture(t, context.Background(), `{"entries":[{"path":"a.md","kind":"file","content":"# content"}]}`, DefaultServiceLimits())
		q := serviceRequest(t, f, "a.md", TextContent)
		entered, release := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		read := f.service.read
		f.service.read = func(ctx context.Context, q Request, p filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
			if calls.Add(1) == 1 {
				close(entered)
			}
			select {
			case <-release:
			case <-ctx.Done():
				return filesystemStop(Unknown, ctx.Err()), p, ctx.Err()
			}
			return read(ctx, q, p)
		}
		type response struct {
			spec ObservationSpec
			err  error
		}
		leaderCtx, cancel := context.WithCancel(context.Background())
		leader := make(chan response, 1)
		go func() { spec, err := f.service.Read(leaderCtx, q); leader <- response{spec, err} }()
		<-entered
		followers := make(chan response, 16)
		for range 16 {
			go func() { spec, err := f.service.Read(context.Background(), q); followers <- response{spec, err} }()
		}
		synctest.Wait()
		cancel()
		synctest.Wait()
		if r := <-leader; !errors.Is(r.err, context.Canceled) || r.spec.Existence == Missing {
			t.Fatal("leader cancellation", r)
		}
		if calls.Load() != 1 {
			t.Fatal("duplicates not coalesced", calls.Load())
		}
		close(release)
		synctest.Wait()
		var owner parser.Source
		for range 16 {
			r := <-followers
			if r.err != nil || r.spec.Content != Available {
				t.Fatal("cancelled leader poisoned follower", r)
			}
			if owner == (parser.Source{}) {
				owner = r.spec.Source
			} else if owner != r.spec.Source {
				t.Fatal("facts not immutable")
			}
		}
		if calls.Load() != 1 || len(f.service.lookups) != 1 {
			t.Fatal("duplicate work charged again")
		}
	})
}

func TestServiceOperationCancellationJoinsWorkerAndQueuedFlights(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opctx, cancel := context.WithCancel(context.Background())
		f := newServiceFixture(t, opctx, `{"complete":true,"entries":[]}`, DefaultServiceLimits())
		entered := make(chan struct{})
		var calls atomic.Int32
		f.service.read = func(ctx context.Context, _ Request, p filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
			if calls.Add(1) == 1 {
				close(entered)
			}
			<-ctx.Done()
			return filesystemStop(Unknown, ctx.Err()), p, ctx.Err()
		}
		qs := []Request{serviceRequest(t, f, "a.md", ExistenceOnly), serviceRequest(t, f, "b.md", ExistenceOnly), serviceRequest(t, f, "c.md", ExistenceOnly)}
		done := make(chan error, 3)
		go func() { _, err := f.service.Read(context.Background(), qs[0]); done <- err }()
		<-entered
		for _, q := range qs[1:] {
			go func() {
				spec, err := f.service.Read(context.Background(), q)
				if spec.Existence == Missing {
					t.Error("provisional absence")
				}
				done <- err
			}()
		}
		synctest.Wait()
		cancel()
		synctest.Wait()
		for range 3 {
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		}
		if err := f.service.Close(); err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 1 {
			t.Fatal("queued work ran after cancellation", calls.Load())
		}
		select {
		case <-f.service.done:
		default:
			t.Fatal("worker not joined")
		}
	})
}

func TestServiceWaitPollsElapsedOperationBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newServiceFixture(t, context.Background(), `{"entries":[]}`, DefaultServiceLimits())
		// The real shared clock starts in the synctest bubble; an operation-budget
		// stop does not require Context.Done. The owned worker is joined explicitly.
		entered, release := make(chan struct{}), make(chan struct{})
		f.service.read = func(_ context.Context, _ Request, p filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
			close(entered)
			<-release
			return ObservationSpec{Existence: Missing, Content: Inapplicable, Anchors: Inapplicable}, p, nil
		}
		q := serviceRequest(t, f, "late.md", ExistenceOnly)
		done := make(chan error, 1)
		go func() { _, err := f.service.Read(context.Background(), q); done <- err }()
		<-entered
		time.Sleep(61 * time.Second)
		err := <-done
		var limit *parser.LimitExceeded
		if !errors.As(err, &limit) || limit.Name != "operation-time" {
			t.Fatal("wait ignored elapsed meter", err)
		}
		close(release)
		synctest.Wait()
	})
}

func TestServiceParseFullPolicyAndSourceOwnership(t *testing.T) {
	f := newServiceFixture(t, context.Background(), `{"entries":[{"path":"a.md","kind":"file","content":"# heading\r\n"},{"path":"b.md","kind":"file","content":"# heading\r\n"}]}`, DefaultServiceLimits())
	q := serviceRequest(t, f, "a.md", TextContent)
	spec, err := f.service.Read(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	key, err := f.service.ParseKey(f.view, spec.Validated)
	if err != nil {
		t.Fatal(err)
	}
	if key.ParseOptions() != f.op.settings.parsing || key.ParserRevision() != "service-v1" || key.AnchorPolicy() != "github" {
		t.Fatal("policy absent from key")
	}
	first, err := f.service.Parse(context.Background(), f.view, spec.Validated)
	if err != nil || first.Status != parser.Complete || first.Source != spec.Source || first.Document.Source() != spec.Source {
		t.Fatal(first, err)
	}
	second, err := f.service.Parse(context.Background(), f.view, spec.Validated)
	if err != nil || first.Document != second.Document {
		t.Fatal("parse cache not reused", err)
	}
	second.Document = nil
	third, err := f.service.Parse(context.Background(), f.view, spec.Validated)
	if err != nil || third.Status != parser.Complete {
		t.Fatal("caller mutated cached parse", err)
	}
	unadmitted, err := f.op.ValidateContent(parser.NewSource(spec.Source.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ParseKey(f.view, unadmitted); !errors.Is(err, ErrContract) {
		t.Fatal("unadmitted bytes bypassed content accounting", err)
	}
	other, err := f.service.Read(context.Background(), serviceRequest(t, f, "b.md", TextContent))
	if err != nil {
		t.Fatal(err)
	}
	foreign, evidence := other.Source, other.Validated
	foreignKey, err := f.service.ParseKey(f.view, evidence)
	if err != nil || key == foreignKey || key.Digest() != foreignKey.Digest() {
		t.Fatal("digest substituted Source ownership", err)
	}
	parsed, err := f.service.Parse(context.Background(), f.view, evidence)
	if err != nil || parsed.Source != foreign || parsed.Document == first.Document {
		t.Fatal("foreign owned tree reused", err)
	}
	if _, err := f.service.ParseKey(View{}, spec.Validated); !errors.Is(err, ErrContract) {
		t.Fatal(err)
	}
	f2 := newServiceFixture(t, context.Background(), `{"entries":[]}`, DefaultServiceLimits())
	if _, err := f2.service.ParseKey(f2.view, spec.Validated); !errors.Is(err, ErrContract) {
		t.Fatal("foreign budget evidence accepted", err)
	}
}

func TestServiceConstructionLimitsAndAdmission(t *testing.T) {
	for _, n := range []int64{0, -1, 1 << 31} {
		l := DefaultServiceLimits()
		l.Identities = n
		if l.valid(&Operation{budget: mustServiceBudget(t)}) {
			t.Fatal("invalid limits", n)
		}
	}
	f := newServiceFixture(t, context.Background(), `{"entries":[]}`, DefaultServiceLimits())
	var invalidContext context.Context
	if _, err := f.service.Read(invalidContext, Request{}); !errors.Is(err, ErrContract) {
		t.Fatal(err)
	}
	if _, err := new(Service).Read(context.Background(), Request{}); !errors.Is(err, ErrContract) {
		t.Fatal(err)
	}
	if _, err := NewFilesystemService(nil, DefaultServiceLimits()); !errors.Is(err, ErrContract) {
		t.Fatal(err)
	}
	if _, err := NewSuppliedService(nil, DefaultServiceLimits()); !errors.Is(err, ErrContract) {
		t.Fatal(err)
	}
}
func mustServiceBudget(t *testing.T) *parser.OperationBudget {
	t.Helper()
	b, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestServiceFollowerCancellationAndLazyUpgradeCoalesce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newServiceFixture(t, context.Background(), `{"entries":[{"path":"a.md","kind":"file","content":"# stable"}]}`, DefaultServiceLimits())
		existence := serviceRequest(t, f, "a.md", ExistenceOnly)
		if spec, err := f.service.Read(context.Background(), existence); err != nil || spec.Existence != File || spec.Content != NotRequested {
			t.Fatal(spec, err)
		}
		q := serviceRequest(t, f, "a.md", TextContent)
		entered, release := make(chan struct{}), make(chan struct{})
		calls := 0
		read := f.service.read
		f.service.read = func(ctx context.Context, q Request, p filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
			calls++
			if calls == 1 {
				close(entered)
			}
			select {
			case <-release:
			case <-ctx.Done():
				return filesystemStop(File, ctx.Err()), p, ctx.Err()
			}
			return read(ctx, q, p)
		}
		leader := make(chan error, 1)
		go func() { _, err := f.service.Read(context.Background(), q); leader <- err }()
		<-entered
		caller, cancel := context.WithCancel(context.Background())
		follower := make(chan error, 1)
		go func() {
			spec, err := f.service.Read(caller, q)
			if spec.Existence != File {
				t.Error("known existence lost on follower cancellation", spec)
			}
			follower <- err
		}()
		synctest.Wait()
		cancel()
		synctest.Wait()
		if err := <-follower; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		close(release)
		synctest.Wait()
		if err := <-leader; err != nil {
			t.Fatal("follower poisoned leader", err)
		}
		for range 4 {
			spec, err := f.service.Read(context.Background(), q)
			if err != nil || spec.Content != Available {
				t.Fatal(spec, err)
			}
		}
		if calls != 1 {
			t.Fatal("content upgraded more than once", calls)
		}
	})
}

func TestServiceQuotaRefusesQueuedNewWorkWithoutEviction(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limits := DefaultServiceLimits()
		limits.Lookups = 2
		f := newServiceFixture(t, context.Background(), `{"complete":true,"entries":[]}`, limits)
		qs := []Request{serviceRequest(t, f, "first.md", ExistenceOnly), serviceRequest(t, f, "queued.md", ExistenceOnly), serviceRequest(t, f, "refused.md", ExistenceOnly)}
		entered, release := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		read := f.service.read
		f.service.read = func(ctx context.Context, q Request, p filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
			calls.Add(1)
			close(entered)
			<-release
			return read(ctx, q, p)
		}
		done := make(chan error, 2)
		go func() { _, err := f.service.Read(context.Background(), qs[0]); done <- err }()
		<-entered
		go func() { _, err := f.service.Read(context.Background(), qs[1]); done <- err }()
		synctest.Wait()
		if _, err := f.service.Read(context.Background(), qs[2]); err == nil {
			t.Fatal("lookup cap not enforced")
		}
		close(release)
		synctest.Wait()
		for range 2 {
			err := <-done
			var limit *parser.LimitExceeded
			if !errors.As(err, &limit) {
				t.Fatal(err)
			}
		}
		if calls.Load() != 1 {
			t.Fatal("queued work executed after quota stop", calls.Load())
		}
		spec, err := f.service.Read(context.Background(), qs[0])
		if spec.Existence != Missing || err == nil {
			t.Fatal("completed miss forgotten", spec, err)
		}
	})
}

func TestServicePathAndDerivedBounds(t *testing.T) {
	f := newServiceFixture(t, context.Background(), `{"entries":[]}`, DefaultServiceLimits())
	for _, name := range []string{strings.Repeat("a", 4097), strings.Repeat("a/", 64) + "a"} {
		q := serviceRequest(t, f, name, ExistenceOnly)
		spec, err := f.service.Read(context.Background(), q)
		var limit *parser.LimitExceeded
		if !errors.As(err, &limit) || spec.Existence == Missing {
			t.Fatal("path bound lost", err)
		}
		if len(f.service.lookups) != 0 {
			t.Fatal("overlong key retained before bound")
		}
	}
	limits := DefaultServiceLimits()
	limits.Identities = 1
	f2 := newServiceFixture(t, context.Background(), `{"entries":[]}`, limits)
	selected, err := f2.service.Read(context.Background(), serviceRequest(t, f2, "source.md", TextContent))
	if err != nil {
		t.Fatal(err)
	}
	evidence := selected.Validated
	if _, err := f2.service.Parse(context.Background(), f2.view, evidence); err != nil {
		t.Fatal(err)
	}
	candidate, err := f2.op.NewView(Candidate, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f2.service.Parse(context.Background(), candidate, evidence); err == nil {
		t.Fatal("unbounded view/policy cache")
	}
}

func TestServiceSelectedUnavailableSavedAndExcludedSeeds(t *testing.T) {
	ctx := context.Background()
	budget := mustServiceBudget(t)
	root, _ := NewRoot(SuppliedRoot)
	settings, _ := NewSettings(parser.ParseOptions{}, "index.md", "github", "selected-v1")
	main := parser.NewSource([]byte("# main [other](other.md)\n"))
	other := parser.NewSource([]byte("# original other\n"))
	excluded := parser.NewSource([]byte("# excluded\n"))
	mainKey, _ := NewKey("source.md")
	otherKey, _ := NewKey("other.md")
	excludedKey, _ := NewKey("excluded.md")
	op, err := NewOperation(ctx, budget, root, settings, []Target{{ID: "source.md", ResourceKey: mainKey, Source: main}, {ID: "other.md", ResourceKey: otherKey, Source: other}})
	if err != nil {
		t.Fatal(err)
	}
	input, err := DecodeSupplied(ctx, budget, DefaultInputLimits(), nil, []SuppliedFile{{Key: mainKey, Source: main}, {Key: otherKey, Source: other}, {Key: excludedKey, Source: excluded}})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewSuppliedReader(ctx, op, input, DefaultInputLimits())
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewSuppliedService(reader, DefaultServiceLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})
	original, err := op.NewView(Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	changed := parser.NewSource([]byte("# candidate other\r\n"))
	candidate, err := op.NewView(Candidate, []Replacement{{TargetID: "other.md", Source: changed}})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := op.NewView(Saved, []Replacement{{TargetID: "other.md", Unavailable: true}})
	if err != nil {
		t.Fatal(err)
	}
	f := serviceFixture{service: service, reader: reader, op: op, view: original, source: main}
	for _, row := range []struct {
		view     View
		expected parser.Source
		exist    Existence
	}{{original, other, File}, {candidate, changed, File}, {saved, parser.Source{}, Unknown}, {original, other, File}} {
		q := serviceRequest(t, f, "other.md", TextContent, row.view)
		spec, err := service.Read(ctx, q)
		if err != nil || spec.Source != row.expected || spec.Existence != row.exist {
			t.Fatal("selected view fell back to original", spec, err)
		}
		serviceRecord(t, f, q, spec)
		q = serviceRequest(t, f, "excluded.md", TextContent, row.view)
		spec, err = service.Read(ctx, q)
		if err != nil || spec.Source != excluded {
			t.Fatal("excluded resource gained replacement", spec, err)
		}
	}
	if len(op.Targets()) != 2 || len(service.identities) != 3 {
		t.Fatal("excluded seed was selected or double counted")
	}
}

func TestServiceCachedDecodeFailureRetainsIndependentIO(t *testing.T) {
	f := newServiceFixture(t, context.Background(), `{"entries":[]}`, DefaultServiceLimits())
	var calls atomic.Int32
	f.service.read = func(_ context.Context, _ Request, p filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
		calls.Add(1)
		return ObservationSpec{Existence: File, Content: Failed, Anchors: NotRequested, Problem: Problem{Kind: DecodeFailure, Message: "resource content is not UTF-8"}}, p, nil
	}
	content := serviceRequest(t, f, "bad.md", TextContent)
	spec, err := f.service.Read(context.Background(), content)
	if spec.Content != Failed || err == nil {
		t.Fatal("real passive decode error not retained", spec, err)
	}
	serviceRecord(t, f, content, spec)
	existence := serviceRequest(t, f, "bad.md", ExistenceOnly)
	spec, err = f.service.Read(context.Background(), existence)
	if spec.Existence != File || spec.Content != NotRequested || err == nil || calls.Load() != 1 {
		t.Fatal("existence hit erased encountered decode failure", spec, err)
	}
	if o := serviceRecord(t, f, existence, spec); o.Treatment() != Complete {
		t.Fatal("known existence lost", o)
	}
}

func TestServiceRetainedLimitEvidenceDefensiveCopies(t *testing.T) {
	f := newServiceFixture(t, context.Background(), `{"entries":[]}`, DefaultServiceLimits())
	original := &parser.LimitExceeded{Name: "directory-entries", Observed: 3, Allowed: 2}
	calls := 0
	f.service.read = func(_ context.Context, _ Request, p filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
		calls++
		return filesystemStop(Unknown, original), p, original
	}
	q := serviceRequest(t, f, "unknown.md", ExistenceOnly)
	spec, err := f.service.Read(context.Background(), q)
	var returned *parser.LimitExceeded
	if !errors.As(err, &returned) || spec.Problem.Limit == nil {
		t.Fatal(spec, err)
	}
	returned.Observed = 999
	spec.Problem.Limit.Observed = 888
	again, err := f.service.Read(context.Background(), q)
	if !errors.As(err, &returned) || returned.Observed != 3 || again.Problem.Limit.Observed != 3 || calls != 1 {
		t.Fatal("caller mutated retained evidence", again, err)
	}
	limits := DefaultServiceLimits()
	limits.Lookups = 1
	g := newServiceFixture(t, context.Background(), `{"entries":[]}`, limits)
	if _, err := g.service.Read(context.Background(), serviceRequest(t, g, "first.md", ExistenceOnly)); err != nil {
		t.Fatal(err)
	}
	_, err = g.service.Read(context.Background(), serviceRequest(t, g, "second.md", ExistenceOnly))
	if !errors.As(err, &returned) {
		t.Fatal(err)
	}
	returned.Observed = 1000
	_, err = g.service.Read(context.Background(), serviceRequest(t, g, "third.md", ExistenceOnly))
	if !errors.As(err, &returned) || returned.Observed != 2 {
		t.Fatal("sticky limit was exposed", err)
	}
}

func TestServiceRetainedIOMessagesAreImmutable(t *testing.T) {
	f := newServiceFixture(t, context.Background(), `{"entries":[]}`, DefaultServiceLimits())
	failure := &os.PathError{Op: "read", Path: "original.md", Err: os.ErrPermission}
	f.service.read = func(_ context.Context, _ Request, p filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
		return filesystemFailure(File, failure), p, &FilesystemReadError{IO: failure}
	}
	q := serviceRequest(t, f, "a.md", TextContent)
	_, err := f.service.Read(context.Background(), q)
	var exposed *os.PathError
	if !errors.As(err, &exposed) || !errors.Is(err, os.ErrPermission) {
		t.Fatal("lost actual IO identity", err)
	}
	exposed.Path = "caller-mutated.md"
	spec, err := f.service.Read(context.Background(), q)
	if strings.Contains(err.Error(), "caller-mutated") || strings.Contains(spec.Problem.Message, "caller-mutated") || !strings.Contains(err.Error(), "original.md") {
		t.Fatal("caller changed retained IO evidence", spec, err)
	}
}

func TestServiceParseKeyUsesActualOperationPolicy(t *testing.T) {
	fm, err := parser.NewFrontmatterOptions(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		name             string
		options          parser.ParseOptions
		revision, anchor string
	}{
		{"base", parser.ParseOptions{}, "revision-a", "github"},
		{"gfm", parser.ParseOptions{GFM: true}, "revision-a", "github"},
		{"frontmatter", parser.ParseOptions{Frontmatter: fm}, "revision-a", "github"},
		{"revision", parser.ParseOptions{}, "revision-b", "github"},
		{"anchor", parser.ParseOptions{}, "revision-a", "explicit"},
	} {
		t.Run(row.name, func(t *testing.T) {
			settings, err := NewSettings(row.options, "index.md", row.anchor, row.revision)
			if err != nil {
				t.Fatal(err)
			}
			f := newServiceFixturePolicy(t, context.Background(), `{"entries":[{"path":"a.md","kind":"file","content":"---\ntitle: example\n---\n- [x] done\n"}]}`, DefaultServiceLimits(), settings)
			q := serviceRequest(t, f, "a.md", TextContent)
			spec, err := f.service.Read(context.Background(), q)
			if err != nil {
				t.Fatal(err)
			}
			key, err := f.service.ParseKey(f.view, spec.Validated)
			if err != nil {
				t.Fatal(err)
			}
			if key.ParseOptions() != row.options || key.ParserRevision() != row.revision || key.AnchorPolicy() != row.anchor {
				t.Fatal("operation policy lost", key)
			}
			parsed, err := f.service.Parse(context.Background(), f.view, spec.Validated)
			if err != nil || parsed.Status != parser.Complete || parsed.Source != spec.Source {
				t.Fatal(parsed, err)
			}
		})
	}
}

func TestServiceRootFactAndClassifiedStops(t *testing.T) {
	f := newServiceFixture(t, context.Background(), `{"entries":[]}`, DefaultServiceLimits())
	span, _ := f.source.Span(0, 1)
	link, err := NewLink("source.md", "MD057", "./", f.source, span)
	if err != nil {
		t.Fatal(err)
	}
	normalizer, err := NewNormalizer(f.view, DefaultPathLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizer.Normalize(link)
	if err != nil || !normalized.Location().IsRootDirectory() {
		t.Fatal(normalized, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	existence, err := f.service.RootDirectory(ctx, normalized.Location())
	if existence != Directory || !errors.Is(err, context.Canceled) {
		t.Fatal("root fact erased", existence, err)
	}
	if _, err := f.service.RootDirectory(context.Background(), Location{}); !errors.Is(err, ErrContract) {
		t.Fatal(err)
	}
	for _, class := range []ReferenceClass{Ignored, BlockedReference, InvalidReference} {
		q, err := f.view.Request(RequestSpec{Link: link, Class: class, Capability: TextContent, Required: RegularFile})
		if err != nil {
			t.Fatal(err)
		}
		spec, err := f.service.Read(ctx, q)
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err := f.op.Record(q, spec); err != nil {
			t.Fatal("static classification became invalid after stop", class, spec, err)
		}
	}
}
