package resource_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
	r "github.com/frithuz/mdchk/internal/resource"
)

func operation(t *testing.T, ctx context.Context, limits parser.Limits) (*r.Operation, r.View, parser.Source) {
	t.Helper()
	root, err := r.NewRoot(r.SuppliedRoot)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := r.NewSettings(parser.ParseOptions{}, "index.md", "github", "parser-v1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	source := parser.NewSource([]byte("雪 [a](asset.md)\r\n"))
	key, _ := r.NewKey("docs/source.md")
	op, err := r.NewOperation(ctx, b, root, settings, []r.Target{{ID: "source.md", Source: source, ResourceKey: key}})
	if err != nil {
		t.Fatal(err)
	}
	view, err := op.NewView(r.Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	return op, view, source
}
func request(t *testing.T, v r.View, source parser.Source, rule string, capability r.Capability, kind r.RequiredType) r.Request {
	t.Helper()
	span, _ := source.Span(8, 16)
	link, err := r.NewLink("source.md", rule, "asset.md", source, span)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := r.NewKey("asset.md")
	req, err := v.Request(r.RequestSpec{Key: key, Link: link, Capability: capability, Required: kind, Class: r.Local})
	if err != nil {
		t.Fatal(err)
	}
	return req
}
func TestCanonicalKeysRemainLiteral(t *testing.T) {
	for _, value := range []string{"Guide.md", "guide.md", "a%20b.md", "a b.md", "docs/a:b.md", "é.md", "e\u0301.md", "replacement-\ufffd.md"} {
		key, err := r.NewKey(value)
		if err != nil || key.String() != value {
			t.Fatal(value, key, err)
		}
	}
	for _, value := range []string{"", ".", "..", "/a", "a/", "a//b", "a/./b", "a/../b", "a\\b", "C:relative", "C:/a", "bad\x00name", string([]byte{255})} {
		if _, err := r.NewKey(value); !errors.Is(err, r.ErrContract) {
			t.Fatal("accepted noncanonical name", value, err)
		}
	}
	if _, err := r.NewSettings(parser.ParseOptions{}, "*.md", "github", "v1"); !errors.Is(err, r.ErrContract) {
		t.Fatal(err)
	}
}
func TestOutcomeContractMatrix(t *testing.T) {
	op, view, source := operation(t, context.Background(), parser.DefaultLimits())
	empty := parser.NewSource(nil)
	text := parser.NewSource([]byte("# A\n"))
	seed, _ := r.NewKey("docs/source.md")
	asset, _ := r.NewKey("asset.md")
	input, err := r.DecodeSupplied(op.Context(), op.Budget(), r.DefaultInputLimits(), nil, []r.SuppliedFile{{Key: seed, Source: source}, {Key: asset, Source: text}})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := r.NewSuppliedReader(op.Context(), op, input, r.DefaultInputLimits())
	if err != nil {
		t.Fatal(err)
	}
	service, err := r.NewSuppliedService(reader, r.DefaultServiceLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})
	indexed, err := service.Read(op.Context(), request(t, view, source, "MD057", r.ParsedAnchors, r.RegularFile))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		cap      r.Capability
		required r.RequiredType
		spec     r.ObservationSpec
		want     r.Treatment
	}{
		{"existence needs no bytes", r.ExistenceOnly, r.RegularFile, r.ObservationSpec{Existence: r.File, Content: r.NotRequested, Anchors: r.NotRequested}, r.Complete},
		{"omitted content preserves file", r.ExistenceOnly, r.RegularFile, r.ObservationSpec{Existence: r.File, Content: r.Unavailable, Anchors: r.Unavailable, Problem: r.Problem{Kind: r.NotSupplied, Message: "not supplied"}}, r.Complete},
		{"omitted anchors incomplete", r.ParsedAnchors, r.RegularFile, r.ObservationSpec{Existence: r.File, Content: r.Unavailable, Anchors: r.Unavailable, Problem: r.Problem{Kind: r.NotSupplied, Message: "not supplied"}}, r.ContextIncomplete},
		{"empty is acquired", r.TextContent, r.RegularFile, r.ObservationSpec{Existence: r.File, Content: r.Available, Anchors: r.NotRequested, Source: empty}, r.Complete},
		{"computed index available", r.ParsedAnchors, r.RegularFile, indexed, r.Complete},
		{"uncertain parsed structure", r.ParsedAnchors, r.RegularFile, r.ObservationSpec{Existence: r.File, Content: r.Available, Anchors: r.Uncertain, Source: text, Problem: r.Problem{Kind: r.ParserUncertain, Message: "uncertain"}}, r.ContextIncomplete},
		{"read fails after existence", r.TextContent, r.RegularFile, r.ObservationSpec{Existence: r.File, Content: r.Failed, Anchors: r.Unavailable, Problem: r.Problem{Kind: r.ReadFailure, Message: "read failed"}}, r.ContextIncomplete},
		{"decode fails after existence", r.ParsedAnchors, r.RegularFile, r.ObservationSpec{Existence: r.File, Content: r.Failed, Anchors: r.Unavailable, Problem: r.Problem{Kind: r.DecodeFailure, Message: "decode failed"}}, r.ContextIncomplete},
		{"known directory", r.ExistenceOnly, r.DirectoryType, r.ObservationSpec{Existence: r.Directory, Content: r.Inapplicable, Anchors: r.Inapplicable}, r.Complete},
		{"directory valued index", r.ExistenceOnly, r.RegularFile, r.ObservationSpec{Existence: r.Directory, Content: r.Inapplicable, Anchors: r.Inapplicable}, r.MissingTarget},
		{"file where directory required", r.ExistenceOnly, r.DirectoryType, r.ObservationSpec{Existence: r.File, Content: r.NotRequested, Anchors: r.NotRequested}, r.MissingTarget},
		{"authoritative absent", r.ExistenceOnly, r.RegularFile, r.ObservationSpec{Existence: r.Missing, Content: r.Inapplicable, Anchors: r.Inapplicable}, r.MissingTarget},
		{"partial omission unknown", r.ParsedAnchors, r.RegularFile, r.ObservationSpec{Existence: r.Unknown, Content: r.Unavailable, Anchors: r.Unavailable, Problem: r.Problem{Kind: r.PartialInventory, Message: "unknown"}}, r.ContextIncomplete},
		{"blocked not missing", r.ExistenceOnly, r.RegularFile, r.ObservationSpec{Existence: r.Unknown, Content: r.Unavailable, Anchors: r.Unavailable, Problem: r.Problem{Kind: r.Blocked, Message: "blocked"}}, r.ContextIncomplete},
	}
	parsedOnly := indexed
	parsedOnly.Index = r.AnchorIndex{}
	if _, err := op.Record(request(t, view, source, "MD051", r.ParsedAnchors, r.RegularFile), parsedOnly); !errors.Is(err, r.ErrContract) {
		t.Fatal("parsed data alone asserted a computed index", err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := request(t, view, source, "MD057", tc.cap, tc.required)
			obs, err := op.Record(q, tc.spec)
			if err != nil || obs.Treatment() != tc.want || obs.Existence() != tc.spec.Existence {
				t.Fatal(obs, err)
			}
			if tc.name == "empty is acquired" {
				s, ok := obs.Content()
				if !ok || s.Len() != 0 || s == (parser.Source{}) {
					t.Fatal("empty conflated with omitted")
				}
			}
		})
	}
	for _, kind := range []r.Existence{r.Missing, r.Directory} {
		q := request(t, view, source, "MD051", r.ParsedAnchors, r.RegularFile)
		obs, err := op.Record(q, r.ObservationSpec{Existence: kind, Content: r.Inapplicable, Anchors: r.Inapplicable})
		if err != nil || obs.Treatment() != r.InapplicableCheck {
			t.Fatal("MD051 took missing ownership", err)
		}
	}
}
func TestContradictoryAndForeignFactsRejected(t *testing.T) {
	op, view, source := operation(t, context.Background(), parser.DefaultLimits())
	q := request(t, view, source, "MD051", r.ParsedAnchors, r.RegularFile)
	for _, spec := range []r.ObservationSpec{{}, {Existence: r.File, Content: r.Available, Anchors: r.NotRequested}, {Existence: r.Missing, Content: r.Available, Anchors: r.Inapplicable, Source: source}, {Existence: r.File, Content: r.NotRequested, Anchors: r.Available}, {Existence: r.Unknown, Content: r.NotRequested, Anchors: r.NotRequested}, {Existence: r.File, Content: r.Failed, Anchors: r.Unavailable}, {Existence: r.File, Content: r.Interrupted, Anchors: r.Unavailable}, {Existence: r.File, Content: r.NotRequested, Anchors: r.Inapplicable}} {
		if _, err := op.Record(q, spec); !errors.Is(err, r.ErrContract) {
			t.Fatal("accepted contradiction", spec, err)
		}
	}
	other, _, _ := operation(t, context.Background(), parser.DefaultLimits())
	if _, err := other.Record(q, r.ObservationSpec{Existence: r.File, Content: r.NotRequested, Anchors: r.NotRequested}); !errors.Is(err, r.ErrContract) {
		t.Fatal(err)
	}
	span, _ := source.Span(0, 3)
	if _, err := r.NewLink("x.md", "MD051", "x", parser.NewSource(source.Bytes()), span); !errors.Is(err, r.ErrContract) {
		t.Fatal("foreign span accepted", err)
	}
	text := parser.NewSource([]byte("# A\n"))
	otherParsed := parser.Parse(context.Background(), text)
	if _, err := op.Record(q, r.ObservationSpec{Existence: r.File, Content: r.Available, Anchors: r.Available, Source: text, Parsed: &otherParsed}); err == nil {
		t.Fatal("foreign completed parse trusted")
	}
}
func TestImmutableViewsAndReadOnlyAuthority(t *testing.T) {
	op, original, source := operation(t, context.Background(), parser.DefaultLimits())
	targets := op.Targets()
	targets[0].ID = "resource-only.md"
	if op.Targets()[0].ID != "source.md" {
		t.Fatal("mutable targets")
	}
	replacement := parser.NewSource([]byte("candidate\n"))
	values := []r.Replacement{{TargetID: "source.md", Source: replacement}}
	candidate, err := op.NewView(r.Candidate, values)
	if err != nil {
		t.Fatal(err)
	}
	values[0].Source = parser.NewSource([]byte("mutated"))
	target, available, ok := candidate.Target("source.md")
	if !ok || !available || target.Source != replacement {
		t.Fatal("replacement aliased")
	}
	old, available, _ := original.Target("source.md")
	if !available || old.Source != source || candidate.Same(original) {
		t.Fatal("original mutated")
	}
	if _, err := op.NewView(r.Candidate, []r.Replacement{{TargetID: "resource-only.md", Source: replacement}}); !errors.Is(err, r.ErrContract) {
		t.Fatal("resource promoted", err)
	}
	saved, err := op.NewView(r.Saved, []r.Replacement{{TargetID: "source.md", Unavailable: true}})
	if err != nil {
		t.Fatal(err)
	}
	target, available, ok = saved.Target("source.md")
	if !ok || available || target.Source != (parser.Source{}) {
		t.Fatal("failed write presumed saved")
	}
	if saved.Same(candidate) || saved.Same(original) || (r.View{}).Same(r.View{}) {
		t.Fatal("view identity conflated")
	}
	q := request(t, original, source, "MD057", r.ExistenceOnly, r.RegularFile)
	obs, err := op.Record(q, r.ObservationSpec{Existence: r.File, Content: r.NotRequested, Anchors: r.NotRequested})
	if err != nil {
		t.Fatal(err)
	}
	_ = obs
	if len(op.Targets()) != 1 {
		t.Fatal("query registered target")
	}
	if _, _, found := original.Target("asset.md"); found {
		t.Fatal("resource became target")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				got, yes, _ := original.Target("source.md")
				if !yes || got.Source != source {
					t.Error("concurrent view mutation")
				}
			}
		})
	}
	wg.Wait()
}
func TestSharedCancellationAndQuotaRetainKnownExistence(t *testing.T) {
	for _, kind := range []string{"cancel", "deadline", "quota"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			op, view, source := operation(t, ctx, parser.DefaultLimits())
			q := request(t, view, source, "MD051", r.ParsedAnchors, r.RegularFile)
			var stop error
			switch kind {
			case "cancel":
				cancel()
				stop = op.Checkpoint(0)
			case "deadline":
				stop = context.DeadlineExceeded
			case "quota":
				stop = op.Checkpoint(op.Budget().Limits().WorkUnits)
			}
			p, ok := r.InterruptionProblem(stop)
			if !ok {
				t.Fatal(stop)
			}
			obs, err := op.Record(q, r.ObservationSpec{Existence: r.File, Content: r.Interrupted, Anchors: r.Interrupted, Problem: p})
			if err != nil || obs.Existence() != r.File || obs.Treatment() != r.ContextIncomplete {
				t.Fatal(obs, err)
			}
			if p.Limit != nil {
				p.Limit.Name = "mutated"
				got := obs.Problem()
				if got.Limit.Name == "mutated" {
					t.Fatal("limit alias")
				}
				got.Limit.Name = "again"
				if obs.Problem().Limit.Name == "again" {
					t.Fatal("accessor alias")
				}
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root, _ := r.NewRoot(r.SuppliedRoot)
	settings, _ := r.NewSettings(parser.ParseOptions{}, "index.md", "github", "v1")
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	op, err := r.NewOperation(ctx, b, root, settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = op.NewView(r.Original, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRequestUsesSelectedViewSource(t *testing.T) {
	op, original, source := operation(t, context.Background(), parser.DefaultLimits())
	candidateSource := parser.NewSource(source.Bytes())
	candidate, err := op.NewView(r.Candidate, []r.Replacement{{TargetID: "source.md", Source: candidateSource}})
	if err != nil {
		t.Fatal(err)
	}
	span, _ := source.Span(8, 16)
	link, _ := r.NewLink("source.md", "MD051", "asset.md", source, span)
	key, _ := r.NewKey("asset.md")
	spec := r.RequestSpec{Key: key, Link: link, Capability: r.ParsedAnchors, Required: r.RegularFile, Class: r.Local}
	if _, err := candidate.Request(spec); !errors.Is(err, r.ErrContract) {
		t.Fatal("original snapshot accepted in candidate view", err)
	}
	foreignLink, _ := r.NewLink("not-selected.md", "MD051", "asset.md", source, span)
	spec.Link = foreignLink
	if _, err := original.Request(spec); !errors.Is(err, r.ErrContract) {
		t.Fatal("unselected requesting source accepted", err)
	}
}

func TestDirectoryIndexAndClassificationSeams(t *testing.T) {
	op, view, source := operation(t, context.Background(), parser.DefaultLimits())
	span, _ := source.Span(8, 16)
	link, _ := r.NewLink("source.md", "MD057", "folder/", source, span)
	for _, attempt := range []struct {
		key  string
		typ  r.RequiredType
		fact r.Existence
		want r.Treatment
	}{
		{"folder", r.DirectoryType, r.Directory, r.Complete},
		{"folder/index.md", r.RegularFile, r.Missing, r.MissingTarget},
		{"folder/index.md", r.RegularFile, r.Directory, r.MissingTarget},
	} {
		key, _ := r.NewKey(attempt.key)
		q, err := view.Request(r.RequestSpec{Key: key, Link: link, Capability: r.ExistenceOnly, Required: attempt.typ, Class: r.Local})
		if err != nil {
			t.Fatal(err)
		}
		obs, err := op.Record(q, r.ObservationSpec{Existence: attempt.fact, Content: r.Inapplicable, Anchors: r.Inapplicable})
		if err != nil || obs.Treatment() != attempt.want {
			t.Fatal(attempt, obs, err)
		}
	}
	for _, tc := range []struct {
		class        r.ReferenceClass
		problem      r.Problem
		availability r.Availability
		want         r.Treatment
	}{
		{r.Ignored, r.Problem{}, r.NotRequested, r.IgnoredCheck},
		{r.BlockedReference, r.Problem{Kind: r.Blocked, Message: "outside root"}, r.Unavailable, r.ContextIncomplete},
		{r.InvalidReference, r.Problem{Kind: r.MalformedReference, Message: "malformed escape"}, r.Unavailable, r.ContextIncomplete},
	} {
		q, err := view.Request(r.RequestSpec{Link: link, Capability: r.ExistenceOnly, Required: r.RegularFile, Class: tc.class})
		if err != nil {
			t.Fatal(err)
		}
		obs, err := op.Record(q, r.ObservationSpec{Existence: r.Unknown, Content: tc.availability, Anchors: tc.availability, Problem: tc.problem})
		if err != nil || obs.Treatment() != tc.want {
			t.Fatal(obs, err)
		}
	}
}
