package resource_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	r "github.com/frithuz/mdchk/internal/resource"
)

func normalizationState(t testing.TB, ctx context.Context, limits parser.Limits, kind r.RootKind, key, index string) (*r.Operation, r.View, parser.Source) {
	t.Helper()
	root, err := r.NewRoot(kind)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := r.NewSettings(parser.ParseOptions{}, index, "github", "normalization-test-v1")
	if err != nil {
		t.Fatal(err)
	}
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	source := parser.NewSource([]byte("# selected snapshot\r\n"))
	var resourceKey r.Key
	if key != "" {
		resourceKey, err = r.NewKey(key)
		if err != nil {
			t.Fatal(err)
		}
	}
	o, err := r.NewOperation(ctx, budget, root, settings, []r.Target{{ID: "selected.md", Source: source, ResourceKey: resourceKey}})
	if err != nil {
		t.Fatal(err)
	}
	view, err := o.NewView(r.Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	return o, view, source
}
func normalizationLink(t testing.TB, source parser.Source, destination string) r.Link {
	t.Helper()
	span, err := source.Span(0, source.Len())
	if err != nil {
		t.Fatal(err)
	}
	link, err := r.NewLink("selected.md", "MD057", destination, source, span)
	if err != nil {
		t.Fatal(err)
	}
	return link
}
func normalizeStateLink(t testing.TB, n *r.Normalizer, source parser.Source, destination string) r.NormalizedLink {
	t.Helper()
	out, err := n.Normalize(normalizationLink(t, source, destination))
	if err != nil {
		t.Fatal(destination, err)
	}
	return out
}
func TestNormalizationViewAndSourceOwnership(t *testing.T) {
	o, original, source := normalizationState(t, context.Background(), parser.DefaultLimits(), r.SuppliedRoot, "docs/guide.md", "index.md")
	candidateSource := parser.NewSource([]byte("# candidate\r\n"))
	savedSource := parser.NewSource([]byte("# saved\r\n"))
	candidate, err := o.NewView(r.Candidate, []r.Replacement{{TargetID: "selected.md", Source: candidateSource}})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := o.NewView(r.Saved, []r.Replacement{{TargetID: "selected.md", Source: savedSource}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		view r.View
		src  parser.Source
	}{{original, source}, {candidate, candidateSource}, {saved, savedSource}} {
		n, err := r.NewNormalizer(tc.view, r.DefaultPathLimits(), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, destination := range []string{"", "?ignored=%", "#", "#part", "guide.md#part", "./other/../guide.md"} {
			link := normalizationLink(t, tc.src, destination)
			out, err := n.Normalize(link)
			if err != nil {
				t.Fatal(err)
			}
			same, ok := out.SameSource()
			key, keyed := out.Location().Key()
			if !ok || same != tc.src || !keyed || key.String() != "docs/guide.md" || out.Link() != link || out.Class() != r.Local {
				t.Fatal("selected view/source identity lost", tc.view.Phase(), destination, out)
			}
		}
		foreign := parser.NewSource(tc.src.Bytes())
		if _, err := n.Normalize(normalizationLink(t, foreign, "#part")); !errors.Is(err, r.ErrContract) {
			t.Fatal("equal foreign snapshot accepted", err)
		}
	}
	unavailable, err := o.NewView(r.Saved, []r.Replacement{{TargetID: "selected.md", Unavailable: true}})
	if err != nil {
		t.Fatal(err)
	}
	n, err := r.NewNormalizer(unavailable, r.DefaultPathLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.Normalize(normalizationLink(t, source, "#part")); !errors.Is(err, r.ErrContract) {
		t.Fatal("unavailable saved source presumed current", err)
	}
}

func TestNormalizationOutsideNamespaceSelfOnly(t *testing.T) {
	_, view, source := normalizationState(t, context.Background(), parser.DefaultLimits(), r.SuppliedRoot, "", "index.md")
	n, err := r.NewNormalizer(view, r.DefaultPathLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{"", "?query", "#heading", "#%FF"} {
		out := normalizeStateLink(t, n, source, destination)
		same, ok := out.SameSource()
		if !ok || same != source || out.Class() != r.Local || out.Location().IsRootDirectory() {
			t.Fatal("outside source self access lost", destination, out)
		}
		if _, ok := out.Location().Key(); ok || out.Location().Root().Kind() != "" {
			t.Fatal("invented namespace membership")
		}
	}
	for _, destination := range []string{"other.md", "selected.md", "./selected.md", ".", "../selected.md"} {
		out := normalizeStateLink(t, n, source, destination)
		if out.Class() != r.BlockedReference || out.Problem().Kind != r.Blocked {
			t.Fatal("report name became resource authority", destination, out)
		}
		if _, ok := out.SameSource(); ok {
			t.Fatal("unproved self alias accepted")
		}
	}
}

func TestNormalizationDirectoryAndIndexRequirements(t *testing.T) {
	o, view, source := normalizationState(t, context.Background(), parser.DefaultLimits(), r.SuppliedRoot, "docs/guide.md", "README.md")
	n, err := r.NewNormalizer(view, r.DefaultPathLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ destination, key string }{{"folder/", "docs/folder/README.md"}, {"folder/.", "docs/folder/README.md"}, {"..", "README.md"}, {".", "docs/README.md"}, {"folder", "docs/folder/README.md"}} {
		out := normalizeStateLink(t, n, source, tc.destination)
		plan, err := out.DirectoryIndex() // caller-established directory, no reads
		if err != nil {
			t.Fatal(err)
		}
		key, ok := plan.Location().Key()
		if !ok || key.String() != tc.key || plan.RequiredType() != r.RegularFile || plan.Class() != r.Local {
			t.Fatal("index fallback/recursion/type error", tc, plan)
		}
		// The index lookup itself exposes no recursive planner. Its known
		// directory evidence is a type mismatch rather than another index level.
		q, err := view.Request(r.RequestSpec{Key: key, Link: out.Link(), Class: plan.Class(), Capability: r.ExistenceOnly, Required: plan.RequiredType()})
		if err != nil {
			t.Fatal(err)
		}
		observation, err := o.Record(q, r.ObservationSpec{Existence: r.Directory, Content: r.Inapplicable, Anchors: r.Inapplicable})
		if err != nil || observation.Treatment() != r.MissingTarget {
			t.Fatal("directory index did not fail file requirement", observation, err)
		}
	}
	root := normalizeStateLink(t, n, source, "..")
	if !root.Location().IsRootDirectory() || !root.DirectoryHint() || root.RequiredType() != r.DirectoryType {
		t.Fatal("virtual root lost", root)
	}
	if _, ok := root.Location().Key(); ok {
		t.Fatal("root manufactured submitted key")
	}
	selfDirectory := normalizeStateLink(t, n, source, "guide.md/.")
	if !selfDirectory.DirectoryHint() || selfDirectory.RequiredType() != r.DirectoryType {
		t.Fatal("source-equal directory requirement lost")
	}
	if _, ok := selfDirectory.SameSource(); ok {
		t.Fatal("regular source file satisfied directory hint")
	}
	key, _ := selfDirectory.Location().Key()
	q, err := view.Request(r.RequestSpec{Key: key, Link: selfDirectory.Link(), Class: r.Local, Capability: r.ExistenceOnly, Required: selfDirectory.RequiredType()})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := o.Record(q, r.ObservationSpec{Existence: r.File, Content: r.NotRequested, Anchors: r.NotRequested})
	if err != nil || observation.Treatment() != r.MissingTarget {
		t.Fatal("regular source file not a type mismatch", observation, err)
	}
	// Completed normalized facts must not retain a mutable public handle.
	retained := normalizeStateLink(t, n, source, "folder/")
	*n = r.Normalizer{}
	plan, err := retained.DirectoryIndex()
	if err != nil {
		t.Fatal("normalizer handle replacement changed retained decision", err)
	}
	if key, ok := plan.Location().Key(); !ok || key.String() != "docs/folder/README.md" {
		t.Fatal(plan)
	}
}

func TestNormalizationRepresentabilityIsAdapterSpecific(t *testing.T) {
	for _, kind := range []r.RootKind{r.ProjectRoot, r.SuppliedRoot} {
		_, view, source := normalizationState(t, context.Background(), parser.DefaultLimits(), kind, "docs/guide.md", "README.md")
		calls := 0
		policy := func(key r.Key) bool {
			calls++
			for part := range strings.SplitSeq(key.String(), "/") {
				if part == "CON" || strings.Contains(part, ":") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || part == "README.md" {
					return false
				}
			}
			return true
		}
		n, err := r.NewNormalizer(view, r.DefaultPathLimits(), policy)
		if err != nil {
			t.Fatal(err)
		}
		for _, destination := range []string{"CON", "./name:stream", "name.", "name%20"} {
			out := normalizeStateLink(t, n, source, destination)
			if kind == r.ProjectRoot {
				if out.Class() != r.BlockedReference || out.Problem().Kind != r.Blocked {
					t.Fatal("native alias treated as ordinary path", destination, out)
				}
			} else if out.Class() != r.Local {
				t.Fatal("supplied namespace inherited native alias", destination, out)
			}
		}
		out := normalizeStateLink(t, n, source, "folder/")
		index, err := out.DirectoryIndex()
		if err != nil {
			t.Fatal(err)
		}
		if kind == r.ProjectRoot && index.Class() != r.BlockedReference || kind == r.SuppliedRoot && index.Class() != r.Local {
			t.Fatal("configured basename policy ignored", index)
		}
		if kind == r.SuppliedRoot && calls != 0 || kind == r.ProjectRoot && calls != 6 {
			t.Fatal("wrong policy authority/invocation", kind, calls)
		}
		if kind == r.ProjectRoot {
			if _, err := r.NewNormalizer(view, r.DefaultPathLimits(), nil); !errors.Is(err, r.ErrContract) {
				t.Fatal("project adapter policy silently omitted", err)
			}
		}
	}
}

func TestNormalizationParserSemanticProvenanceAndEffects(t *testing.T) {
	text := "雪\r\n[x](a\\*b&amp;c%23d.md#%)\r\n"
	f := newReportFixture(t, context.Background(), text)
	parsed := parser.ParseWithOptions(f.operation.Context(), f.source, f.operation.Budget(), f.operation.Settings().ParseOptions())
	if parsed.Status != parser.Complete || parsed.Document == nil {
		t.Fatal(parsed)
	}
	var found *parser.Node
	var visit func(*parser.Node)
	visit = func(node *parser.Node) {
		if node.Kind() == parser.Link {
			found = node
		}
		for _, child := range node.Children() {
			visit(child)
		}
	}
	visit(parsed.Document.Root())
	if found == nil {
		t.Fatal("parser did not expose link")
	}
	destination := found.Properties().Link.Destination
	if destination.Semantic != parser.Some("a*b&c%23d.md#%") || len(destination.Segments) < 3 {
		t.Fatal(destination)
	}
	span, err := f.source.Span(9, 28) // exact original destination envelope, including escape/entity bytes
	if err != nil {
		t.Fatal(err)
	}
	link, err := r.NewLink("docs/a.md", "MD057", destination.Semantic.Value, f.source, span)
	if err != nil {
		t.Fatal(err)
	}
	n, err := r.NewNormalizer(f.view, r.DefaultPathLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := n.Normalize(link)
	if err != nil {
		t.Fatal(err)
	}
	key, ok := out.Location().Key()
	if !ok || key.String() != "docs/a*b&c#d.md" || out.Class() != r.Local || out.Fragment().Status() != r.FragmentInvalid || out.Link() != link {
		t.Fatal("semantic decoding, fragment independence or provenance lost", out)
	}
	original, err := out.Link().Source().Text(out.Link().Span())
	if err != nil || original != "a\\*b&amp;c%23d.md#%" {
		t.Fatal("original source bytes changed", original, err)
	}
	q, err := f.view.Request(r.RequestSpec{Key: key, Link: out.Link(), Class: out.Class(), Capability: r.ExistenceOnly, Required: r.RegularFile})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := f.operation.Record(q, r.ObservationSpec{Existence: r.File, Content: r.NotRequested, Anchors: r.NotRequested})
	if err != nil || observation.Treatment() != r.Complete {
		t.Fatal("invalid fragment hid valid existence check", observation, err)
	}
	result := f.result(t, true)
	beforeFiles := append([]string(nil), result.CheckedFiles...)
	if err := f.view.AppendEffects(&result, []r.Observation{observation}); err != nil {
		t.Fatal(err)
	}
	engine.FinalizeLintResult(&result)
	if len(result.Coverage) != 0 || !reflect.DeepEqual(result.CheckedFiles, beforeFiles) {
		t.Fatal("existence query invented coverage or target", result)
	}
	// Blocked and invalid path decisions reuse the existing source-attributed
	// coverage path without asserting absence or adding resource-only targets.
	for _, destination := range []string{"../../escape.md", "bad%FF.md"} {
		newLink, err := r.NewLink("docs/a.md", "MD057", destination, f.source, span)
		if err != nil {
			t.Fatal(err)
		}
		decision, err := n.Normalize(newLink)
		if err != nil {
			t.Fatal(err)
		}
		q, err := f.view.Request(r.RequestSpec{Link: decision.Link(), Class: decision.Class(), Capability: r.ExistenceOnly, Required: r.RegularFile})
		if err != nil {
			t.Fatal(err)
		}
		o, err := f.operation.Record(q, r.ObservationSpec{Existence: r.Unknown, Content: r.Unavailable, Anchors: r.NotRequested, Problem: decision.Problem()})
		if err != nil {
			t.Fatal(err)
		}
		result := f.result(t, false)
		if err := f.view.AppendEffects(&result, []r.Observation{o}); err != nil {
			t.Fatal(err)
		}
		engine.FinalizeLintResult(&result)
		if len(result.Coverage) != 1 || result.Coverage[0].Range.Start.Line != 2 || result.Coverage[0].Range.Start.Column != 5 || result.Status != "incomplete" || len(result.Errors) != 0 {
			t.Fatal("unavailable path lost exact requesting range", result)
		}
		assertResourceTargetsUnchanged(t, result)
		validateResourceResult(t, result)
	}
}

// Actual cancellation at a bounded segment checkpoint, without sleeps or a
// goroutine scheduling assumption. The wrapper triggers a real child context.
type normalizationCancelContext struct {
	context.Context
	cancel context.CancelFunc
	checks int
	stopAt int
}

func (c *normalizationCancelContext) Err() error {
	c.checks++
	if c.stopAt > 0 && c.checks >= c.stopAt {
		c.cancel()
	}
	return c.Context.Err()
}

func TestNormalizationSharedLimitsAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controlled := &normalizationCancelContext{Context: ctx, cancel: cancel}
	o, view, source := normalizationState(t, controlled, parser.DefaultLimits(), r.SuppliedRoot, "docs/guide.md", "index.md")
	n, err := r.NewNormalizer(view, r.DefaultPathLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	retained := normalizeStateLink(t, n, source, "other.md#good")
	// Normalize performs its start/reserve checks, then bounded segment checks.
	controlled.stopAt = controlled.checks + 4
	if _, err := n.Normalize(normalizationLink(t, source, "a/b/c.md")); !errors.Is(err, context.Canceled) {
		t.Fatal("mid-normalization cancellation lost", err)
	}
	if err := o.Checkpoint(0); !errors.Is(err, context.Canceled) {
		t.Fatal("context reset", err)
	}
	key, ok := retained.Location().Key()
	if !ok || key.String() != "docs/other.md" || retained.Fragment().Text() != "good" {
		t.Fatal("previously completed facts lost")
	}
	if _, err := retained.DirectoryIndex(); !errors.Is(err, context.Canceled) {
		t.Fatal("post-stop planning started", err)
	}
	for _, limits := range []r.PathLimits{{Bytes: 15, Depth: 3}, {Bytes: 100, Depth: 2}} {
		_, view, source := normalizationState(t, context.Background(), parser.DefaultLimits(), r.SuppliedRoot, "docs/guide.md", "index.md")
		n, err := r.NewNormalizer(view, limits, nil)
		if err != nil {
			t.Fatal(err)
		}
		var expected string
		var destination string
		if limits.Bytes == 15 {
			expected, destination = "path-bytes", "longlongname.md"
		} else {
			expected, destination = "path-depth", "a/b.md"
		}
		_, err = n.Normalize(normalizationLink(t, source, destination))
		var limit *parser.LimitExceeded
		if !errors.As(err, &limit) || limit.Name != expected || limit.Observed <= limit.Allowed {
			t.Fatal("path bound not attributed", limits, err)
		}
	}
	limits := parser.DefaultLimits()
	limits.WorkUnits = 200
	o, view, source = normalizationState(t, context.Background(), limits, r.SuppliedRoot, "docs/guide.md", "index.md")
	n, err = r.NewNormalizer(view, r.DefaultPathLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.Normalize(normalizationLink(t, source, "other.md")); err == nil {
		t.Fatal("shared quota not charged")
	}
	if err := o.Checkpoint(0); err == nil {
		t.Fatal("quota reset")
	}
	limits = parser.DefaultLimits()
	limits.DocumentNodes, limits.TotalNodes = 12, 12
	_, view, source = normalizationState(t, context.Background(), limits, r.SuppliedRoot, "docs/guide.md", "index.md")
	n, err = r.NewNormalizer(view, r.DefaultPathLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = n.Normalize(normalizationLink(t, source, "a/b/c.md"))
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || limit.Name != "total-nodes" {
		t.Fatal("bounded construction ignored shared node quota", err)
	}
}

func TestNormalizationConcurrentImmutableReads(t *testing.T) {
	_, view, source := normalizationState(t, context.Background(), parser.DefaultLimits(), r.SuppliedRoot, "docs/guide.md", "index.md")
	n, err := r.NewNormalizer(view, r.DefaultPathLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	link := normalizationLink(t, source, "./guide.md#part")
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			out, err := n.Normalize(link)
			if err != nil {
				t.Error(err)
				return
			}
			if same, ok := out.SameSource(); !ok || same != source || out.Fragment().Text() != "part" || out.Link() != link {
				t.Error("shared immutable read changed identity", out)
			}
		})
	}
	wg.Wait()
}

func TestNormalizationZeroAndInvalidPlans(t *testing.T) {
	if _, err := (r.NormalizedLink{}).DirectoryIndex(); !errors.Is(err, r.ErrContract) {
		t.Fatal("zero plan accepted", err)
	}
	var nilNormalizer *r.Normalizer
	if _, err := nilNormalizer.Normalize(r.Link{}); !errors.Is(err, r.ErrContract) {
		t.Fatal("nil normalizer accepted", err)
	}
	if _, err := r.NewNormalizer(r.View{}, r.DefaultPathLimits(), nil); !errors.Is(err, r.ErrContract) {
		t.Fatal("zero view accepted", err)
	}
	_, view, source := normalizationState(t, context.Background(), parser.DefaultLimits(), r.SuppliedRoot, "docs/guide.md", "index.md")
	for _, limits := range []r.PathLimits{{}, {Bytes: -1, Depth: 1}, {Bytes: 1, Depth: 0}, {Bytes: 1 << 31, Depth: 1}, {Bytes: 1, Depth: 1 << 21}} {
		if _, err := r.NewNormalizer(view, limits, nil); !errors.Is(err, r.ErrContract) {
			t.Fatal("invalid operator profile accepted", limits, err)
		}
	}
	n, err := r.NewNormalizer(view, r.DefaultPathLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{"https://example.test/", "../../escape", "bad%FF"} {
		out := normalizeStateLink(t, n, source, destination)
		if _, err := out.DirectoryIndex(); !errors.Is(err, r.ErrContract) {
			t.Fatal("unavailable/nonlocal plan accepted", destination, err)
		}
	}
}

func TestNormalizationRejectsRemovedControlSegments(t *testing.T) {
	_, view, source := normalizationState(t, context.Background(), parser.DefaultLimits(), r.SuppliedRoot, "docs/guide.md", "index.md")
	n, err := r.NewNormalizer(view, r.DefaultPathLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{"bad%00/../guide.md", "a%0a/..", "\u0085/../other.md"} {
		out := normalizeStateLink(t, n, source, destination)
		if out.Class() != r.InvalidReference || out.Problem().Kind != r.MalformedReference {
			t.Fatal("removed invalid scalar was accepted", destination, out)
		}
	}
}

func TestNormalizationIndexCanReuseSelectedSnapshot(t *testing.T) {
	_, view, source := normalizationState(t, context.Background(), parser.DefaultLimits(), r.SuppliedRoot, "docs/README.md", "README.md")
	n, err := r.NewNormalizer(view, r.DefaultPathLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := normalizeStateLink(t, n, source, ".")
	if _, ok := out.SameSource(); ok {
		t.Fatal("directory assumed to be selected file")
	}
	index, err := out.DirectoryIndex()
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := index.SameSource(); !ok || got != source {
		t.Fatal("configured index did not preserve exact selected snapshot", index)
	}
	if limits, input := r.DefaultPathLimits(), r.DefaultInputLimits(); limits.Bytes != input.PathBytes || limits.Depth != input.PathDepth {
		t.Fatal("divergent path defaults", limits, input)
	}
}

func TestNormalizationLiteralSelfIdentityNeedsNoAdapterLookup(t *testing.T) {
	_, view, source := normalizationState(t, context.Background(), parser.DefaultLimits(), r.ProjectRoot, "docs/a%20b.md", "index.md")
	calls := 0
	n, err := r.NewNormalizer(view, r.DefaultPathLimits(), func(r.Key) bool { calls++; return false })
	if err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{"", "#part", "a%2520b.md#part", "./x/../a%2520b.md"} {
		out := normalizeStateLink(t, n, source, destination)
		key, present := out.Location().Key()
		same, reused := out.SameSource()
		if out.Class() != r.Local || !present || key.String() != "docs/a%20b.md" || !reused || same != source {
			t.Fatal("literal selected identity was decoded again or reacquired", destination, out)
		}
	}
	if calls != 0 {
		t.Fatal("native lookup policy applied to proved source identity", calls)
	}
	other := normalizeStateLink(t, n, source, "a%20b.md")
	if other.Class() != r.BlockedReference || calls != 1 {
		t.Fatal("different decoded name skipped adapter policy", other, calls)
	}
}

func TestNormalizationProjectIndexReusesExactViewBeforeNewLookup(t *testing.T) {
	o, original, originalSource := normalizationState(t, context.Background(), parser.DefaultLimits(), r.ProjectRoot, "docs/README.md", "README.md")
	candidateSource := parser.NewSource([]byte("# candidate index\r\n"))
	savedSource := parser.NewSource([]byte("# confirmed saved index\r\n"))
	candidate, err := o.NewView(r.Candidate, []r.Replacement{{TargetID: "selected.md", Source: candidateSource}})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := o.NewView(r.Saved, []r.Replacement{{TargetID: "selected.md", Source: savedSource}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		view   r.View
		source parser.Source
	}{{original, originalSource}, {candidate, candidateSource}, {saved, savedSource}} {
		t.Run(string(tc.view.Phase()), func(t *testing.T) {
			calls := map[string]int{}
			policy := func(key r.Key) bool {
				calls[key.String()]++
				return key.String() == "docs" || key.String() == "docs/other"
			}
			n, err := r.NewNormalizer(tc.view, r.DefaultPathLimits(), policy)
			if err != nil {
				t.Fatal(err)
			}
			direct := normalizeStateLink(t, n, tc.source, "README.md#part")
			if same, ok := direct.SameSource(); direct.Class() != r.Local || !ok || same != tc.source {
				t.Fatal("direct selected index identity lost", direct)
			}
			directory := normalizeStateLink(t, n, tc.source, ".#part")
			if directory.Class() != r.Local || !directory.DirectoryHint() || directory.RequiredType() != r.DirectoryType || directory.Fragment().Text() != "part" {
				t.Fatal("caller directory/type/fragment preconditions changed", directory)
			}
			if _, ok := directory.SameSource(); ok {
				t.Fatal("directory assumed to be regular selected source")
			}
			index, err := directory.DirectoryIndex() // caller-established docs directory
			if err != nil {
				t.Fatal(err)
			}
			key, present := index.Location().Key()
			same, reused := index.SameSource()
			if index.Class() != r.Local || index.RequiredType() != r.RegularFile || !present || key.String() != "docs/README.md" || !reused || same != tc.source {
				t.Fatal("configured selected index was reacquired or blocked", tc.view.Phase(), index)
			}
			// The same known selected file with a directory hint must retain
			// the established type mismatch instead of invoking fresh lookup.
			for _, hintedDestination := range []string{"README.md/.#part", "README.md/#part"} {
				for _, rule := range []string{"MD057", "MD051"} {
					base := normalizationLink(t, tc.source, hintedDestination)
					link, err := r.NewLink(base.Path(), rule, base.Destination(), base.Source(), base.Span())
					if err != nil {
						t.Fatal(err)
					}
					hinted, err := n.Normalize(link)
					if err != nil {
						t.Fatal(err)
					}
					hintedKey, present := hinted.Location().Key()
					if hinted.Class() != r.Local || !present || hintedKey.String() != "docs/README.md" || !hinted.DirectoryHint() || hinted.RequiredType() != r.DirectoryType || hinted.Fragment().Text() != "part" {
						t.Fatal("known selected type mismatch hidden by new lookup policy", hinted)
					}
					if _, ok := hinted.SameSource(); ok {
						t.Fatal("regular selected file satisfied directory hint")
					}
					capability := r.ExistenceOnly
					if rule == "MD051" {
						capability = r.ParsedAnchors
					}
					request, err := tc.view.Request(r.RequestSpec{Key: hintedKey, Link: hinted.Link(), Class: hinted.Class(), Capability: capability, Required: hinted.RequiredType()})
					if err != nil {
						t.Fatal(err)
					}
					observation, err := o.Record(request, r.ObservationSpec{Existence: r.File, Content: r.NotRequested, Anchors: r.NotRequested})
					want := r.MissingTarget
					if rule == "MD051" {
						want = r.InapplicableCheck
					}
					if err != nil || observation.Treatment() != want {
						t.Fatal("known file/directory mismatch ownership changed", rule, observation, err)
					}
				}
			}
			if calls["docs/README.md"] != 0 || calls["docs"] != 1 || len(calls) != 1 {
				t.Fatal("selected identity consulted new lookup policy", calls)
			}
			otherDirectory := normalizeStateLink(t, n, tc.source, "other/")
			otherIndex, err := otherDirectory.DirectoryIndex()
			if err != nil {
				t.Fatal(err)
			}
			if otherIndex.Class() != r.BlockedReference || otherIndex.Problem().Kind != r.Blocked || calls["docs/other"] != 1 || calls["docs/other/README.md"] != 1 {
				t.Fatal("distinct unrepresentable index bypassed new lookup policy", otherIndex, calls)
			}
			if _, ok := otherIndex.SameSource(); ok {
				t.Fatal("distinct index reused selected bytes")
			}
			if _, ok := otherIndex.Location().Key(); ok {
				t.Fatal("blocked index retained lookup location")
			}
		})
	}
}
