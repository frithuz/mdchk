package resource

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/frithuz/mdchk/internal/parser"
)

func newAnchorFixture(t *testing.T, text, style string, options parser.ParseOptions) serviceFixture {
	t.Helper()
	settings, err := NewSettings(options, "index.md", style, "anchors-v1")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"entries": []any{map[string]any{"path": "a.md", "kind": "file", "content": text}}})
	if err != nil {
		t.Fatal(err)
	}
	return newServiceFixturePolicy(t, context.Background(), string(raw), DefaultServiceLimits(), settings)
}

func readAnchorFixture(t *testing.T, f serviceFixture) (ObservationSpec, AnchorIndex) {
	t.Helper()
	q := serviceRequest(t, f, "a.md", ParsedAnchors)
	spec, err := f.service.Read(context.Background(), q)
	if err != nil || spec.Anchors != Available || spec.Index.state == nil {
		t.Fatal("anchor index unavailable", spec.Anchors, spec.Problem, err)
	}
	o, err := f.op.Record(q, spec)
	if err != nil || o.Treatment() != Complete {
		t.Fatal("computed anchors did not bind", o, err)
	}
	index, ok := o.Index()
	if !ok || index != spec.Index {
		t.Fatal("bound index lost", ok)
	}
	return spec, index
}

func TestAnchorVisibleTextAndProjectAlgorithm(t *testing.T) {
	for _, row := range []struct {
		name, markdown, visible, base string
		gfm                           bool
	}{
		{"markup", "# Hello, *World*!\n", "Hello, World!", "hello-world", false},
		{"image-alt", "# A ![red fox](img.png \"ignored title\")\n", "A red fox", "a-red-fox", false},
		{"nested-labels", "# [*A* **B**](ignored) ![C ![D](inner)](outer)\n", "A B C D", "a-b-c-d", false},
		{"symbols", "# A+B $ C!\n", "A+B $ C!", "a+b-$-c", false},
		{"unicode", "# É—猫 💡\n", "É—猫 💡", "é猫-💡", false},
		{"lower-not-fold", "# ẞ Σ K\n", "ẞ Σ K", "ß-σ-k", false},
		{"preserve-hyphen-underscore", "# A_B-C\n", "A_B-C", "a_b-c", false},
		{"code-space", "# `A  B`\n", "A  B", "a--b", false},
		{"entities-once", "# A &amp;lt; B\n", "A &lt; B", "a-lt-b", false},
		{"escape", "# A\\*B\n", "A*B", "ab", false},
		{"html-wrappers", "# A <em>B</em>\n", "A B", "a-b", false},
		{"html-image-alt", "# ![<b>C</b>](img)\n", "C", "c", false},
		{"soft-break", "A\nB\n===\n", "A\nB", "a-b", false},
		{"hard-break", "A  \nB\n===\n", "A\nB", "a-b", false},
		{"unicode-whitespace", "# A\u2003\u2003B\n", "A\u2003\u2003B", "a--b", false},
		{"combining-not-normalized", "# E\u0301\n", "E\u0301", "e\u0301", false},
		{"no-pandoc-id", "# A {#custom}\n", "A {#custom}", "a-custom", false},
		{"core-tilde-visible", "# ~~Gone~~\n", "~~Gone~~", "~~gone~~", false},
		{"gfm-strike", "# ~~Gone~~\n", "Gone", "gone", true},
		{"punctuation-empty", "# !!!\n", "!!!", "", false},
	} {
		t.Run(row.name, func(t *testing.T) {
			_, index := readAnchorFixture(t, newAnchorFixture(t, row.markdown, "github", parser.ParseOptions{GFM: row.gfm}))
			headings, err := index.Headings(context.Background())
			if err != nil || len(headings) != 1 {
				t.Fatal(headings, err)
			}
			h := headings[0]
			if h.Visible != row.visible || h.Base != parser.Some(row.base) || h.ID != parser.Some(row.base) || h.Usable != (row.base != "") || h.Explicit {
				t.Fatalf("got visible=%q base=%+v id=%+v usable=%v", h.Visible, h.Base, h.ID, h.Usable)
			}
			original, err := index.Source().Text(h.Envelope)
			if err != nil || !strings.Contains(original, strings.TrimSpace(strings.Split(row.markdown, "\n")[0])) {
				t.Fatal("heading lost original provenance", original, h.Envelope, err)
			}
		})
	}
}

func TestAnchorGlobalExplicitReservationAndLowestSuffix(t *testing.T) {
	text := "# Foo\n\n# Foo-1\n\n# Foo\n\n# Foo\n\n<span id='foo-1'></span>\n<i id='foo-3'></i>\n"
	_, index := readAnchorFixture(t, newAnchorFixture(t, text, "github", parser.ParseOptions{}))
	headings, err := index.Headings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got, bases []string
	for _, h := range headings {
		got = append(got, h.ID.Value)
		bases = append(bases, h.Base.Value)
	}
	if !reflect.DeepEqual(got, []string{"foo", "foo-1-1", "foo-2", "foo-4"}) || !reflect.DeepEqual(bases, []string{"foo", "foo-1", "foo", "foo"}) {
		t.Fatal("not globally reserved/lowest available", got, bases)
	}
	occurrences, err := index.Occurrences(context.Background())
	if err != nil || len(occurrences) != 6 {
		t.Fatal(occurrences, err)
	}
	last := -1
	for _, occurrence := range occurrences {
		if occurrence.Envelope.Start() < last {
			t.Fatal("occurrences not in source order", occurrences)
		}
		last = occurrence.Envelope.Start()
	}
	for _, id := range []string{"foo", "foo-1", "foo-1-1", "foo-2", "foo-3", "foo-4"} {
		matches, err := index.Lookup(context.Background(), id)
		if err != nil || len(matches) != 1 || matches[0].ID != id || !matches[0].Usable {
			t.Fatal(id, matches, err)
		}
	}
}

func TestAnchorEmptyBasesAndExplicitOccurrenceOwnership(t *testing.T) {
	text := "# !!!\n\n# ???\n\n<span id='-1'></span>\n<i id='' ID='ignored'></i>\n<b id='X' ID='Y'></b>\n<em id='X'></em>\n<a name='legacy'></a>\n<strong id=' '></strong>\n<q id='%41'></q>\n"
	_, index := readAnchorFixture(t, newAnchorFixture(t, text, "github", parser.ParseOptions{}))
	headings, err := index.Headings(context.Background())
	if err != nil || len(headings) != 2 || headings[0].Base != parser.Some("") || headings[0].ID != parser.Some("") || headings[0].Usable || headings[1].ID != parser.Some("-2") {
		t.Fatal(headings, err)
	}
	for _, row := range []struct {
		id    string
		count int
	}{{"", 0}, {"ignored", 0}, {"Y", 0}, {"X", 2}, {"x", 0}, {"legacy", 0}, {" ", 1}, {"%41", 1}, {"A", 0}} {
		got, err := index.Lookup(context.Background(), row.id)
		if err != nil || len(got) != row.count {
			t.Fatal(row.id, got, err)
		}
	}
	all, err := index.Occurrences(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ignored int
	for _, occurrence := range all {
		if occurrence.Kind == HTMLAnchorKind {
			if _, err := index.Source().Text(occurrence.Attribute); err != nil {
				t.Fatal("attribute lost source", occurrence, err)
			}
			if !occurrence.Effective {
				ignored++
			}
		}
	}
	if ignored != 2 {
		t.Fatal("ignored duplicate attributes lost", all)
	}
}

func TestAnchorExplicitModeAndHeadingContainerFacts(t *testing.T) {
	text := "# Root\n\n> ## Quote\n\n- ### Item\n\n<h1 id='html-heading'>Not Markdown</h1>\n\n# Text {#fictional}\n"
	_, index := readAnchorFixture(t, newAnchorFixture(t, text, "explicit", parser.ParseOptions{}))
	headings, err := index.Headings(context.Background())
	if err != nil || len(headings) != 4 {
		t.Fatal(headings, err)
	}
	for _, heading := range headings {
		if heading.ID.Present || heading.Base.Present || heading.Usable || heading.Explicit {
			t.Fatal("explicit mode fabricated heading ID", heading)
		}
	}
	if len(headings[0].Containers) != 0 || len(headings[1].Containers) != 1 || headings[1].Containers[0].Kind != parser.BlockQuote || len(headings[2].Containers) != 2 || headings[2].Containers[0].Kind != parser.List || headings[2].Containers[1].Kind != parser.ListItem {
		t.Fatal("heading ancestry lost", headings)
	}
	if matches, err := index.Lookup(context.Background(), "html-heading"); err != nil || len(matches) != 1 || matches[0].Kind != HTMLAnchorKind {
		t.Fatal(matches, err)
	}
	if matches, err := index.Lookup(context.Background(), "fictional"); err != nil || len(matches) != 0 {
		t.Fatal("cancelled syntax invented a target", matches, err)
	}
}

func TestAnchorUnavailableParseAndHTMLCannotProveAbsence(t *testing.T) {
	frontmatter, _ := parser.NewFrontmatterOptions(true, nil)
	for _, row := range []struct {
		text    string
		options parser.ParseOptions
	}{
		{"---\n# Not body\n", parser.ParseOptions{Frontmatter: frontmatter}},
		{"# Heading\n\n<script id='filtered'>x</script>\n", parser.ParseOptions{GFM: true}},
	} {
		f := newAnchorFixture(t, row.text, "github", row.options)
		q := serviceRequest(t, f, "a.md", ParsedAnchors)
		spec, err := f.service.Read(context.Background(), q)
		if err != nil || spec.Content != Available || spec.Anchors != Uncertain || spec.Problem.Kind != ParserUncertain || spec.Source == (parser.Source{}) || spec.Index.state != nil || spec.Validated.identity == nil {
			t.Fatal("uncertainty became absence/success or erased content", spec, err)
		}
		if _, err := spec.Index.Lookup(context.Background(), "anything"); !errors.Is(err, ErrContract) {
			t.Fatal("zero index granted negative authority", err)
		}
		if o := serviceRecord(t, f, q, spec); o.Treatment() != ContextIncomplete {
			t.Fatal(o)
		}
	}
}

func TestAnchorCacheOwnershipAndPostStopFacts(t *testing.T) {
	f := newAnchorFixture(t, "# One\n\n<span id='id'></span>\n", "github", parser.ParseOptions{})
	spec, index := readAnchorFixture(t, f)
	result, err := f.service.Anchors(context.Background(), f.view, spec.Validated)
	if err != nil {
		t.Fatal(err)
	}
	if again, ok := result.Index(); !ok || again != index {
		t.Fatal("derived index was not reused", again, ok)
	}
	headings, _ := index.Headings(context.Background())
	headings[0].Visible = "changed"
	all, _ := index.Occurrences(context.Background())
	for i := range all {
		all[i].ID = "changed"
		if len(all[i].Value.Segments) > 0 {
			all[i].Value.Segments[0].Semantic = parser.Some("changed")
		}
	}
	if after, _ := index.Headings(context.Background()); after[0].Visible != "One" {
		t.Fatal("headings mutable", after)
	}
	if after, _ := index.Lookup(context.Background(), "id"); len(after) != 1 || after[0].ID != "id" || after[0].Value.Semantic != parser.Some("id") {
		t.Fatal("target facts mutable", after)
	}
	unadmitted, _ := f.op.ValidateContent(spec.Source)
	if _, err := f.service.Anchors(context.Background(), f.view, unadmitted); !errors.Is(err, ErrContract) {
		t.Fatal("generic validation bypassed service admission", err)
	}
	other := newAnchorFixture(t, "# One\n\n<span id='id'></span>\n", "github", parser.ParseOptions{})
	if _, err := f.service.Anchors(context.Background(), other.view, spec.Validated); !errors.Is(err, ErrContract) {
		t.Fatal("foreign view borrowed index", err)
	}
	q := serviceRequest(t, f, "a.md", ParsedAnchors)
	if err := f.op.budget.ChargeWork(f.op.budget.Limits().WorkUnits); err == nil {
		t.Fatal("actual quota did not stop")
	}
	late, stopped := f.service.Read(context.Background(), q)
	var limit *parser.LimitExceeded
	if !errors.As(stopped, &limit) || late.Index != index || late.Anchors != Available || late.Content != Available {
		t.Fatal("late stop erased completed index/content", late, stopped)
	}
	if o := serviceRecord(t, f, q, late); o.Treatment() != Complete {
		t.Fatal(o)
	}
}

func TestAnchorCoalescesAndCallerStopDoesNotPoisonIndex(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newAnchorFixture(t, "# Shared\n", "github", parser.ParseOptions{})
		q := serviceRequest(t, f, "a.md", TextContent)
		spec, err := f.service.Read(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.Parse(context.Background(), f.view, spec.Validated); err != nil {
			t.Fatal(err)
		}
		var builds atomic.Int32
		started, release := make(chan struct{}), make(chan struct{})
		build := f.service.buildAnchors
		f.service.buildAnchors = func(ctx context.Context, operation *Operation, key ParseCacheKey, parsed parser.ParseResult) (AnchorResult, error) {
			builds.Add(1)
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return stoppedAnchorResult(operation, key, ctx.Err()), ctx.Err()
			}
			return build(ctx, operation, key, parsed)
		}
		leader, cancel := context.WithCancel(context.Background())
		leaderDone := make(chan error, 1)
		go func() { _, err := f.service.Anchors(leader, f.view, spec.Validated); leaderDone <- err }()
		<-started
		type outcome struct {
			result AnchorResult
			err    error
		}
		followers := make(chan outcome, 16)
		for range 16 {
			go func() {
				out, err := f.service.Anchors(context.Background(), f.view, spec.Validated)
				followers <- outcome{out, err}
			}()
		}
		synctest.Wait()
		cancel()
		if err := <-leaderDone; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		close(release)
		var first AnchorIndex
		for range 16 {
			out := <-followers
			index, ok := out.result.Index()
			if out.err != nil || !ok {
				t.Fatal(out.err, ok)
			}
			if first.state == nil {
				first = index
			} else if first != index {
				t.Fatal("duplicate derived production")
			}
		}
		if builds.Load() != 1 {
			t.Fatal("duplicate producer", builds.Load())
		}
	})
}

func TestAnchorComputedProofSourceViewAndPolicyIsolation(t *testing.T) {
	f := newAnchorFixture(t, "# Same\n", "github", parser.ParseOptions{})
	spec, first := readAnchorFixture(t, f)
	for _, phase := range []Phase{Candidate, Saved} {
		view, err := f.op.NewView(phase, nil)
		if err != nil {
			t.Fatal(err)
		}
		q := serviceRequest(t, f, "a.md", ParsedAnchors, view)
		if _, err := f.op.Record(q, spec); !errors.Is(err, ErrContract) {
			t.Fatal("foreign view borrowed computed proof", err)
		}
		out, err := f.service.Anchors(context.Background(), view, spec.Validated)
		index, ok := out.Index()
		if err != nil || !ok || index == first || index.Key().view == first.Key().view {
			t.Fatal("views aliased", err, ok)
		}
	}
	for _, options := range []parser.ParseOptions{{}, {GFM: true}} {
		for _, style := range []string{"github", "explicit"} {
			other := newAnchorFixture(t, "# Same\n", style, options)
			otherSpec, index := readAnchorFixture(t, other)
			if index.Key().ParseOptions() != options || index.Key().AnchorPolicy() != style || index.Key().ParserRevision() != "anchors-v1" {
				t.Fatal("incomplete policy key", index.Key())
			}
			otherSpec.Index = first
			if _, err := other.op.Record(serviceRequest(t, other, "a.md", ParsedAnchors), otherSpec); !errors.Is(err, ErrContract) {
				t.Fatal("foreign operation/source proof accepted", err)
			}
		}
	}
}

func TestAnchorTargetWitnessReuseAndFiniteDerivedAdmission(t *testing.T) {
	f := newAnchorFixture(t, "# Resource\n", "github", parser.ParseOptions{})
	f.service.limits.Identities = 2
	first, err := f.service.TargetAnchors(context.Background(), f.view, "source.md")
	if err != nil {
		t.Fatal(err)
	}
	_, evidence, ok := first.Content()
	if !ok {
		t.Fatal("selected witness missing")
	}
	for range 4 {
		again, err := f.service.TargetAnchors(context.Background(), f.view, "source.md")
		_, repeated, ok := again.Content()
		if err != nil || !ok || repeated != evidence || len(f.service.validations) != 1 || len(f.service.targetEvidence) != 1 {
			t.Fatal("witness was reminted", err)
		}
	}
	candidate, err := f.op.NewView(Candidate, []Replacement{{TargetID: "source.md", Source: parser.NewSource(f.source.Bytes())}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.TargetAnchors(context.Background(), candidate, "source.md")
	if err != nil {
		t.Fatal(err)
	}
	one, _ := first.Index()
	two, _ := second.Index()
	if one == two || one.Source().Token() == two.Source().Token() {
		t.Fatal("equal bytes aliased selected owners")
	}
	saved, err := f.op.NewView(Saved, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.service.TargetAnchors(context.Background(), saved, "source.md")
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || limit.Name != "resource-derived-entries" || limit.Observed != 3 || limit.Allowed != 2 || out.Availability() != Interrupted || len(f.service.targetEvidence) != 2 {
		t.Fatal("unbounded derived admission", out.Availability(), err)
	}
}

func TestAnchorIndexConstructionStopWithholdsPartialAuthority(t *testing.T) {
	f := newAnchorFixture(t, strings.Repeat("# Heading\n\n", 20), "github", parser.ParseOptions{})
	content, err := f.service.Read(context.Background(), serviceRequest(t, f, "a.md", TextContent))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := f.service.Parse(context.Background(), f.view, content.Validated)
	if err != nil {
		t.Fatal(err)
	}
	key, err := f.service.ParseKey(f.view, content.Validated)
	if err != nil {
		t.Fatal(err)
	}
	// Parsing is finished, so this is an actual index-production quota stop.
	if err := f.op.budget.ChargeWork(f.op.budget.Limits().WorkUnits); err == nil {
		t.Fatal("quota not exhausted")
	}
	out, err := buildAnchorIndex(context.Background(), f.op, key, parsed)
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || out.Availability() != Interrupted {
		t.Fatal(out.Availability(), err)
	}
	if _, ok := out.Index(); ok {
		t.Fatal("partial index published")
	}
}

func TestAnchorParserFailureStaysInternal(t *testing.T) {
	f := newAnchorFixture(t, "# Heading\n", "github", parser.ParseOptions{})
	content, err := f.service.Read(context.Background(), serviceRequest(t, f, "a.md", TextContent))
	if err != nil {
		t.Fatal(err)
	}
	key, err := f.service.ParseKey(f.view, content.Validated)
	if err != nil {
		t.Fatal(err)
	}
	parsed := parser.ParseResult{Source: content.Source, EffectiveLimits: f.op.budget.Limits(), Failure: &parser.Failure{Code: parser.InternalFailure, Message: "test invariant"}}
	out, err := buildAnchorIndex(context.Background(), f.op, key, parsed)
	if err == nil || out.Availability() != Failed || out.Problem().Kind != InternalFailure {
		t.Fatal(out.Problem(), err)
	}
	var metadata *serviceReadError
	if !errors.As(err, &metadata) || len(metadata.causes) != 1 || metadata.causes[0].kind != InternalFailure {
		t.Fatal("internal failure mislabeled as IO", err)
	}
}

func TestAnchorForeignContextNeverProvesAbsenceOrGeneratedSuffix(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, style := range []string{"github", "explicit"} {
			for _, text := range []string{
				`<svg><title><span id=x>live</span></title></svg>`,
				"# Foo\n\n# Foo\n\n<svg><title><span id=foo-1>live</span></title></svg>\n",
				"# Foo\n\n# Foo\n\n<math><mtext><span id=foo-1>live</span></mtext></math>\n",
			} {
				f := newAnchorFixture(t, text, style, parser.ParseOptions{GFM: gfm})
				for _, phase := range []Phase{Original, Candidate, Saved} {
					view := f.view
					if phase != Original {
						var err error
						view, err = f.op.NewView(phase, nil)
						if err != nil {
							t.Fatal(err)
						}
					}
					q := serviceRequest(t, f, "a.md", ParsedAnchors, view)
					spec, err := f.service.Read(context.Background(), q)
					if err != nil || spec.Existence != File || spec.Content != Available || spec.Validated.Source() != spec.Source || spec.Source == (parser.Source{}) || spec.Anchors != Uncertain || spec.Problem.Kind != ParserUncertain {
						t.Fatal("foreign context erased content or granted complete anchors", gfm, style, phase, spec, err)
					}
					if _, err := spec.Index.Lookup(context.Background(), "foo-1"); !errors.Is(err, ErrContract) {
						t.Fatal("unknown occupied suffix granted negative authority", err)
					}
					observation, err := f.op.Record(q, spec)
					if err != nil || observation.Treatment() != ContextIncomplete {
						t.Fatal(observation, err)
					}
					if _, ok := observation.Index(); ok {
						t.Fatal("partial computed index published")
					}
				}
			}
		}
	}
}
