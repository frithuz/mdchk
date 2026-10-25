package resource

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func fragmentTestResolver(t *testing.T, f serviceFixture, view View) *FragmentResolver {
	t.Helper()
	n, err := NewNormalizer(view, DefaultPathLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewFragmentResolver(f.service, n)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func fragmentTestQuery(t *testing.T, r *FragmentResolver, destination string) FragmentQuery {
	t.Helper()
	target, _, _ := r.normalizer.view.Target("source.md")
	span, _ := target.Source.Span(0, 1)
	link, err := NewLink(target.ID, "MD051", destination, target.Source, span)
	if err != nil {
		t.Fatal(err)
	}
	q, err := r.Prepare(link)
	if err != nil {
		t.Fatal(err)
	}
	return q
}
func TestFragmentExactDecodedMembershipAndPolicy(t *testing.T) {
	text := "<i id='A+B'></i> <i id='a b'></i> <i id='a%20b'></i> <i id='a/b'></i> <i id='é'></i> <i id='éx'></i> <i id='é'></i> <i id='�'></i> <i id='D'></i><b id='D'></b>\n\n# ~~Gone~~\n"
	for _, style := range []string{"github", "explicit"} {
		for _, gfm := range []bool{false, true} {
			f := newAnchorFixture(t, text, style, parser.ParseOptions{GFM: gfm})
			r := fragmentTestResolver(t, f, f.view)
			for _, row := range []struct {
				fragment string
				want     FragmentOutcome
				matches  int
			}{
				{"#A+B", FragmentFound, 1}, {"#A%2BB", FragmentFound, 1}, {"#a+b", FragmentAbsentTarget, 0}, {"#a%20b", FragmentFound, 1}, {"#a%2520b", FragmentFound, 1}, {"#a%2Fb", FragmentFound, 1}, {"#%C3%A9", FragmentFound, 1}, {"#e%CC%81", FragmentFound, 1}, {"#e%CC%81x", FragmentAbsentTarget, 0}, {"#�", FragmentFound, 1}, {"#%EF%BF%BD", FragmentFound, 1}, {"#D", FragmentFound, 2}, {"#d", FragmentAbsentTarget, 0}, {"#%", FragmentMalformed, 0}, {"#%FF", FragmentMalformed, 0}, {"#%C3", FragmentMalformed, 0},
			} {
				q := fragmentTestQuery(t, r, "a.md"+row.fragment)
				got, err := r.Resolve(context.Background(), q, true)
				if err != nil || got.Outcome() != row.want {
					t.Fatalf("%s gfm=%v %q: %s %v", style, gfm, row.fragment, got.Outcome(), err)
				}
				matches, err := got.Matches(context.Background())
				expected := map[string]string{"#A+B": "A+B", "#A%2BB": "A+B", "#a%20b": "a b", "#a%2520b": "a%20b", "#a%2Fb": "a/b", "#%C3%A9": "é", "#e%CC%81": "é", "#�": "�", "#%EF%BF%BD": "�", "#D": "D"}
				if len(matches) > 0 && matches[0].ID != expected[row.fragment] {
					t.Fatalf("once-decoded exact ID %q: got %q want %q", row.fragment, matches[0].ID, expected[row.fragment])
				}
				if err != nil || len(matches) != row.matches {
					t.Fatal(row, matches, err)
				}
				if len(matches) > 0 {
					matches[0].ID = "changed"
					again, _ := got.Matches(context.Background())
					if again[0].ID == "changed" {
						t.Fatal("mutable resolution")
					}
				}
			}
			wanted := "~~gone~~"
			if gfm {
				wanted = "gone"
			}
			want := FragmentFound
			if style == "explicit" {
				want = FragmentAbsentTarget
			}
			got, err := r.Resolve(context.Background(), fragmentTestQuery(t, r, "a.md#"+wanted), true)
			if err != nil || got.Outcome() != want {
				t.Fatal(style, gfm, got.Outcome(), err)
			}
		}
	}
}
func TestFragmentPreparedOwnershipAndTransactionalReport(t *testing.T) {
	f := newServiceFixture(t, context.Background(), `{"entries":[{"path":"unknown.md","kind":"unavailable"}]}`, DefaultServiceLimits())
	r := fragmentTestResolver(t, f, f.view)
	q := fragmentTestQuery(t, r, "missing.md#x")
	if _, err := r.Resolve(nil, q, true); !errors.Is(err, ErrContract) { //nolint:staticcheck // Deliberately verify the nil-context rejection contract.
		t.Fatal(err)
	}
	other := fragmentTestResolver(t, f, f.view)
	if _, err := other.Resolve(context.Background(), q, true); !errors.Is(err, ErrContract) {
		t.Fatal(err)
	}
	got, err := r.Resolve(context.Background(), q, true)
	if err != nil || got.Outcome() != FragmentContextUnavailable {
		t.Fatal(got.Outcome(), err)
	}
	if _, err := r.Resolve(context.Background(), q, true); !errors.Is(err, ErrContract) {
		t.Fatal("copied query reusable", err)
	}
	owner, _ := NewFragmentReport(r, result.NewLint("cli"))
	if err := owner.Append(f.view, []FragmentResolution{got, {}}); !errors.Is(err, ErrContract) {
		t.Fatal(err)
	}
	if len(owner.state.out.Coverage) != 0 {
		t.Fatal("partial invalid batch mutated output")
	}
	if err := owner.Append(f.view, []FragmentResolution{got, got}); !errors.Is(err, ErrContract) {
		t.Fatal(err)
	}
	if err := owner.Append(f.view, []FragmentResolution{got}); err != nil {
		t.Fatal(err)
	}
	alias := *owner
	out, err := owner.Finish()
	if err != nil || len(out.Coverage) != 1 {
		t.Fatal(out, err)
	}
	if _, err := alias.Finish(); !errors.Is(err, ErrContract) {
		t.Fatal("copied owner reused", err)
	}
	if err := alias.Append(f.view, nil); !errors.Is(err, ErrContract) {
		t.Fatal(err)
	}
	var zero FragmentReport
	if _, err := zero.Finish(); !errors.Is(err, ErrContract) {
		t.Fatal(err)
	}
	foreign := newServiceFixture(t, context.Background(), "", DefaultServiceLimits())
	fn, _ := NewNormalizer(foreign.view, DefaultPathLimits(), nil)
	if _, err := NewFragmentResolver(f.service, fn); !errors.Is(err, ErrContract) {
		t.Fatal(err)
	}
	span, _ := f.source.Span(0, 1)
	alien := parser.NewSource(f.source.Bytes())
	aspan, _ := alien.Span(0, 1)
	link, _ := NewLink("source.md", "MD051", "#source", alien, aspan)
	if _, err := r.Prepare(link); !errors.Is(err, ErrContract) {
		t.Fatal("same bytes admitted foreign owner", err)
	}
	link, _ = NewLink("source.md", "MD051", "#source", f.source, span)
	if err := f.op.Budget().ChargeWork(f.op.Budget().Limits().WorkUnits); err == nil {
		t.Fatal("quota did not stop")
	}
	if rejected, err := r.Prepare(link); err == nil || rejected.state != nil {
		t.Fatal("unadmitted query after stop", err)
	}
}
func TestFragmentActualLookupQuotaRetainsCompleteIndex(t *testing.T) {
	f := newServiceFixture(t, context.Background(), "", DefaultServiceLimits())
	r := fragmentTestResolver(t, f, f.view)
	warm, err := f.service.TargetAnchors(context.Background(), f.view, "source.md")
	if err != nil || warm.Availability() != Available {
		t.Fatal(warm.Availability(), err)
	}
	id := strings.Repeat("x", 100000)
	q := fragmentTestQuery(t, r, "#"+id)
	// Preparation reserves 16 byte inspections per semantic destination byte.
	// Leave a live allowance for a ready-cache hit but less than this actual
	// Lookup's ID hash charge. ChargeWork must succeed before Resolve begins.
	if err := f.op.Budget().ChargeWork(f.op.Budget().Limits().WorkUnits - 17*int64(len(id)) - 5000); err != nil {
		t.Fatal("pre-lookup allowance exhausted", err)
	}
	got, err := r.Resolve(context.Background(), q, false)
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || limit.Observed-limit.Allowed < int64(len(id))/2 || got.Outcome() != FragmentContextUnavailable || got.Anchors().Availability() != Available {
		t.Fatal(got.Outcome(), got.Anchors().Availability(), err)
	}
	if _, _, ok := got.Anchors().Content(); !ok {
		t.Fatal("query stop erased content witness")
	}
	owner, _ := NewFragmentReport(r, result.NewLint("cli"))
	if err := owner.Append(f.view, []FragmentResolution{got}); err != nil {
		t.Fatal(err)
	}
	out, _ := owner.Finish()
	if len(out.Coverage) != 1 || len(out.Interruptions) != 1 || len(out.Coverage[0].CauseIDs) != 1 || out.Coverage[0].CauseIDs[0] != out.Interruptions[0].ID {
		t.Fatal("complete resource hid failed query", out)
	}
}

func TestFragmentSourceViewsAndFrontmatter(t *testing.T) {
	fm, err := parser.NewFrontmatterOptions(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := newAnchorFixture(t, "---\nkey: value\n---\n\n# Body\n", "github", parser.ParseOptions{Frontmatter: fm})
	r := fragmentTestResolver(t, f, f.view)
	got, err := r.Resolve(context.Background(), fragmentTestQuery(t, r, "a.md#body"), true)
	if err != nil || got.Outcome() != FragmentFound {
		t.Fatal(got.Outcome(), err)
	}
	candidate := parser.NewSource([]byte("# Replacement\n"))
	view, err := f.op.NewView(Candidate, []Replacement{{TargetID: "source.md", Source: candidate}})
	if err != nil {
		t.Fatal(err)
	}
	cr := fragmentTestResolver(t, f, view)
	for _, row := range []struct {
		destination string
		want        FragmentOutcome
	}{{"#replacement", FragmentFound}, {"#source", FragmentAbsentTarget}} {
		got, err := cr.Resolve(context.Background(), fragmentTestQuery(t, cr, row.destination), false)
		if err != nil || got.Outcome() != row.want {
			t.Fatal(row, got.Outcome(), err)
		}
	}
	saved, err := f.op.NewView(Saved, []Replacement{{TargetID: "source.md", Unavailable: true}})
	if err != nil {
		t.Fatal(err)
	}
	sr := fragmentTestResolver(t, f, saved)
	span, _ := candidate.Span(0, 1)
	link, _ := NewLink("source.md", "MD051", "#replacement", candidate, span)
	if _, err := sr.Prepare(link); !errors.Is(err, ErrContract) {
		t.Fatal("unavailable Saved borrowed Candidate", err)
	}
}

func TestFragmentSelectedAnyExtensionAndMissingStopOwnership(t *testing.T) {
	f := fragmentIntegrationSupplied(t, "docs/selected.txt", "index.md", `{"complete":true,"entries":[{"path":"docs/missing.md","kind":"missing"}]}`, []string{"#part", "missing.md#%FF"})
	self := fragmentIntegrationResolve(t, f.resolver, f.source, f.id, "#part", false)
	if self.Outcome() != FragmentFound {
		t.Fatal("selected non-Markdown extension lost its explicit document authority", self.Outcome())
	}
	// Admit the true missing fact, then prepare another query before cancellation.
	missing := fragmentIntegrationResolve(t, f.resolver, f.source, f.id, "missing.md#%FF", true)
	if missing.Outcome() != FragmentInapplicable {
		t.Fatal(missing.Outcome())
	}
	span, _ := f.source.Span(0, 1)
	link, _ := NewLink(f.id, "MD051", "missing.md#%FF", f.source, span)
	q, err := f.resolver.Prepare(link)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := f.resolver.Resolve(ctx, q, true)
	if !errors.Is(err, context.Canceled) || got.Outcome() != FragmentInapplicable {
		t.Fatal("disabled MD057 ownership transferred", got.Outcome(), err)
	}
	owner, _ := NewFragmentReport(f.resolver, result.NewLint("cli"))
	if err := owner.Append(f.view, []FragmentResolution{got}); err != nil {
		t.Fatal(err)
	}
	out, err := owner.Finish()
	if err != nil || len(out.Coverage) != 0 || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
		t.Fatal("known absence invented fragment coverage or discarded real stop", out, err)
	}
}

func TestFragmentConcurrentQueriesShareServiceProduction(t *testing.T) {
	f := newAnchorFixture(t, "# Target\n", "github", parser.ParseOptions{})
	r := fragmentTestResolver(t, f, f.view)
	const count = 12
	queries := make([]FragmentQuery, count)
	for i := range queries {
		queries[i] = fragmentTestQuery(t, r, "a.md#target")
	}
	var builds atomic.Int64
	build := f.service.buildAnchors
	f.service.buildAnchors = func(ctx context.Context, op *Operation, key ParseCacheKey, p parser.ParseResult) (AnchorResult, error) {
		builds.Add(1)
		return build(ctx, op, key, p)
	}
	var wg sync.WaitGroup
	results := make(chan FragmentResolution, count)
	errs := make(chan error, count)
	for _, q := range queries {
		wg.Go(func() { got, err := r.Resolve(context.Background(), q, true); results <- got; errs <- err })
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for got := range results {
		if got.Outcome() != FragmentFound {
			t.Fatal(got.Outcome())
		}
	}
	if builds.Load() != 1 {
		t.Fatal("fragment orchestration bypassed shared index production", builds.Load())
	}
}

func TestFragmentOperationalCausesAreNotMissingNames(t *testing.T) {
	for _, kind := range []ProblemKind{ReadFailure, InternalFailure} {
		t.Run(string(kind), func(t *testing.T) {
			f := newAnchorFixture(t, "# Target\n", "github", parser.ParseOptions{})
			r := fragmentTestResolver(t, f, f.view)
			if kind == InternalFailure {
				f.service.buildAnchors = func(_ context.Context, op *Operation, key ParseCacheKey, _ parser.ParseResult) (AnchorResult, error) {
					return AnchorResult{&anchorResultState{operation: op, key: key, availability: Failed, problem: Problem{Kind: InternalFailure, Message: "controlled parser failure"}}}, anchorInternalError(&parser.Failure{Code: parser.InternalFailure, Message: "controlled parser failure"})
				}
			} else {
				read := f.service.read
				f.service.read = func(ctx context.Context, q Request, receipt filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
					if q.Capability() == ExistenceOnly {
						return read(ctx, q, receipt)
					}
					return ObservationSpec{Existence: File, Content: Failed, Anchors: Unavailable, Problem: Problem{Kind: ReadFailure, Message: "controlled data failure"}}, filesystemReceipt{}, &FilesystemReadError{IO: errors.New("controlled data failure")}
				}
			}
			got, err := r.Resolve(context.Background(), fragmentTestQuery(t, r, "a.md#target"), true)
			if err == nil || got.Outcome() != FragmentContextUnavailable {
				t.Fatal(got.Outcome(), err)
			}
			owner, _ := NewFragmentReport(r, result.NewLint("cli"))
			if err := owner.Append(f.view, []FragmentResolution{got}); err != nil {
				t.Fatal(err)
			}
			out, err := owner.Finish()
			category := result.IO
			if kind == InternalFailure {
				category = result.Internal
			}
			if err != nil || len(out.Errors) != 1 || out.Errors[0].Category != category || out.Errors[0].Path != "a.md" || len(out.Coverage) != 1 || len(out.Coverage[0].CauseIDs) != 1 || out.Coverage[0].CauseIDs[0] != out.Errors[0].ID {
				t.Fatal("operational cause/source relation lost", out, err)
			}
		})
	}
}

func TestFragmentProspectiveIndexBoundsDoNotRejectOrdinaryFiles(t *testing.T) {
	const name = "long-target-name.md"
	for _, limits := range []PathLimits{{Bytes: int64(len(name)), Depth: 8}, {Bytes: 1024, Depth: 1}} {
		for _, directory := range []bool{false, true} {
			kind := "file"
			payload := `,"content":"# Target\n"`
			want := FragmentFound
			if directory {
				kind = "directory"
				payload = ""
				want = FragmentContextUnavailable
			}
			raw := `{"entries":[{"path":"` + name + `","kind":"` + kind + `"` + payload + `}]}`
			f := newServiceFixture(t, context.Background(), raw, DefaultServiceLimits())
			n, err := NewNormalizer(f.view, limits, nil)
			if err != nil {
				t.Fatal(err)
			}
			r, err := NewFragmentResolver(f.service, n)
			if err != nil {
				t.Fatal(err)
			}
			q := fragmentTestQuery(t, r, name+"#target")
			got, err := r.Resolve(context.Background(), q, true)
			if got.Outcome() != want {
				t.Fatal(limits, directory, got.Outcome(), err)
			}
			if directory {
				var limit *parser.LimitExceeded
				if !errors.As(err, &limit) {
					t.Fatal("required index bound lost", err)
				}
				if observations := got.Observations(); len(observations) != 1 || observations[0].Existence() != Directory {
					t.Fatal("prospective failure erased directory fact", observations)
				}
			} else if err != nil {
				t.Fatal("unneeded child rejected valid file", err)
			}
		}
	}
}
