//go:build linux || darwin || windows

package resource

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func fragmentNativeFixture(t *testing.T, key, index string, destinations []string) *filesystemFixture {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	text := "# Part\n"
	for _, destination := range destinations {
		text += fmt.Sprintf("[query](%s)\n", destination)
	}
	source := parser.NewSource([]byte(text))
	name := filepath.Join(dir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, source.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	budget, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	native, err := OpenFilesystemRoot(context.Background(), budget, dir, DefaultFilesystemLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := native.Close(); err != nil {
			t.Error(err)
		}
	})
	proof, err := native.BindTarget(name, "report.md", source)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := NewSettings(parser.ParseOptions{}, index, "github", "fragment-native-v1")
	if err != nil {
		t.Fatal(err)
	}
	op, err := NewOperation(context.Background(), budget, native.Root(), settings, []Target{proof.Target()})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := native.Reader(op, proof)
	if err != nil {
		t.Fatal(err)
	}
	view, err := op.NewView(Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &filesystemFixture{dir, native, reader, op, view, source, budget, proof}
}
func fragmentNativeResolver(t *testing.T, service *Service, view View) *FragmentResolver {
	t.Helper()
	normalizer, err := NewNormalizer(view, DefaultPathLimits(), NativeNamePolicy)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewFragmentResolver(service, normalizer)
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func TestFilesystemFragmentFirstFactsAndFreshOperation(t *testing.T) {
	destinations := []string{"appeared.md#part", "asset.md#part", "asset.md#changed", "image.bin#", "image.bin#part", "other.md#part", "source.md#part", "#part"}
	f := fragmentNativeFixture(t, "source.md", "index.md", destinations)
	f.write(t, "asset.md", []byte("# Part\n"))
	f.write(t, "image.bin", []byte{0xff, 0, 0xfe})
	service := filesystemTestService(t, f)
	resolver := fragmentNativeResolver(t, service, f.view)
	dataReads := 0
	read := f.filesystem.read
	f.filesystem.read = func(file *os.File, buffer []byte) (int, error) { dataReads++; return read(file, buffer) }
	resolve := func(destination string, cross bool) FragmentResolution {
		return fragmentIntegrationResolve(t, resolver, f.source, "report.md", destination, cross)
	}
	for _, destination := range []string{"other.md#part", "image.bin#", "source.md#part", "#part"} {
		got := resolve(destination, false)
		want := FragmentFound
		if destination == "other.md#part" {
			want = FragmentSkipped
		}
		if destination == "image.bin#" {
			want = DocumentReference
		}
		if got.Outcome() != want || dataReads != 0 {
			t.Fatalf("selected/skip/document path read native bytes: %q outcome=%q reads=%d", destination, got.Outcome(), dataReads)
		}
	}
	if got := resolve("image.bin#part", true); got.Outcome() != FragmentContextUnavailable || dataReads != 0 {
		t.Fatal("binary fragment interpreted or payload read", got.Outcome(), dataReads)
	}
	if got := resolve("appeared.md#part", true); got.Outcome() != FragmentInapplicable {
		t.Fatal("actual missing target not inapplicable", got.Outcome())
	}
	f.write(t, "appeared.md", []byte("# Part\n"))
	if got := resolve("appeared.md#part", true); got.Outcome() != FragmentInapplicable {
		t.Fatal("cached first miss was replaced after appearance", got.Outcome())
	}
	first := resolve("asset.md#part", true)
	firstIndex, ok := first.Anchors().Index()
	if first.Outcome() != FragmentFound || !ok || dataReads == 0 {
		t.Fatal("actual content/index missing", first.Outcome(), ok, dataReads)
	}
	firstReads := dataReads
	f.write(t, "asset.md", []byte("# Changed\n"))
	for _, row := range []struct {
		destination string
		want        FragmentOutcome
	}{{"asset.md#part", FragmentFound}, {"asset.md#changed", FragmentAbsentTarget}} {
		got := resolve(row.destination, true)
		index, ok := got.Anchors().Index()
		if got.Outcome() != row.want || !ok || index.Source() != firstIndex.Source() || dataReads != firstReads {
			t.Fatalf("first bytes reopened after mutation: %q %q reads %d/%d", row.destination, got.Outcome(), dataReads, firstReads)
		}
	}
	next := filesystemTestNextSession(t, f)
	nextService := filesystemTestService(t, next)
	nextResolver := fragmentNativeResolver(t, nextService, next.view)
	for _, row := range []struct {
		destination string
		want        FragmentOutcome
	}{{"appeared.md#part", FragmentFound}, {"asset.md#part", FragmentAbsentTarget}, {"asset.md#changed", FragmentFound}} {
		got := fragmentIntegrationResolve(t, nextResolver, next.source, "report.md", row.destination, true)
		if got.Outcome() != row.want {
			t.Fatalf("fresh operation did not reacquire %q: %q want %q", row.destination, got.Outcome(), row.want)
		}
	}
}

func TestFilesystemFragmentFinalSelectedIndexAndSingleAttempt(t *testing.T) {
	for _, key := range []string{"README.md", "docs/README.md"} {
		destinations := []string{"./#part", ".#part", "README.md#part", "elsewhere/#part", "README.md/#part"}
		if strings.HasPrefix(key, "docs/") {
			destinations = append(destinations, "../docs#part", "../docs/#part")
		}
		f := fragmentNativeFixture(t, key, "README.md", destinations)
		f.write(t, "elsewhere/index.md", []byte("# Part\n"))
		service := filesystemTestService(t, f)
		resolver := fragmentNativeResolver(t, service, f.view)
		var acquired []string
		read := service.read
		service.read = func(ctx context.Context, q Request, receipt filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
			acquired = append(acquired, q.Key().String())
			return read(ctx, q, receipt)
		}
		dataReads := 0
		dataRead := f.filesystem.read
		f.filesystem.read = func(file *os.File, buffer []byte) (int, error) { dataReads++; return dataRead(file, buffer) }
		for _, destination := range destinations {
			before := len(acquired)
			got := fragmentIntegrationResolve(t, resolver, f.source, "report.md", destination, false)
			want := FragmentFound
			if destination == "elsewhere/#part" {
				want = FragmentSkipped
			}
			if destination == "README.md/#part" {
				want = FragmentInapplicable
			}
			if got.Outcome() != want || dataReads != 0 {
				t.Fatalf("native final identity %q/%q outcome=%q want=%q reads=%d", key, destination, got.Outcome(), want, dataReads)
			}
			if destination == "elsewhere/#part" && len(acquired) != before {
				t.Fatal("distinct prospective index performed native acquisition", acquired)
			}
			for _, name := range acquired {
				if name != "docs" && name != key {
					t.Fatalf("selected index reopened or guessed index: %v", acquired)
				}
			}
		}
	}
	// A directory at the sole configured index is not a reason to recurse or
	// choose an alternative README, even though both would offer the wanted ID.
	f := fragmentNativeFixture(t, "source.md", "index.md", []string{"nested/#part"})
	f.write(t, "nested/index.md/index.md", []byte("# Part\n"))
	f.write(t, "nested/README.md", []byte("# Part\n"))
	service := filesystemTestService(t, f)
	resolver := fragmentNativeResolver(t, service, f.view)
	var keys []string
	read := service.read
	service.read = func(ctx context.Context, q Request, receipt filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
		keys = append(keys, q.Key().String())
		return read(ctx, q, receipt)
	}
	got := fragmentIntegrationResolve(t, resolver, f.source, "report.md", "nested/#part", true)
	if got.Outcome() != FragmentInapplicable || strings.Join(keys, "|") != "nested|nested/index.md" {
		t.Fatal("native index recursed/fell back", got.Outcome(), keys)
	}
}

// CheckFilesystemFragmentNoKeySelfCandidateAndStoppedQueryReport retains private resource controls while the outer test supplies the real engine finalizer.
func CheckFilesystemFragmentNoKeySelfCandidateAndStoppedQueryReport(t *testing.T, finalize func(*result.Result)) {
	for _, metadata := range []bool{false, true} {
		t.Run(fmt.Sprint("metadata=", metadata), func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			outside, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(outside, "outside.md")
			if metadata {
				name = filepath.Join(dir, ".git", "selected.md")
				if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
					t.Fatal(err)
				}
			}
			originalSource := parser.NewSource([]byte("# Original\r\n[x](#original) [y](secret.md#inside)\r\n"))
			if err := os.WriteFile(name, originalSource.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "secret.md"), []byte("# Inside\n"), 0600); err != nil {
				t.Fatal(err)
			}
			budget, err := parser.NewOperationBudget(parser.DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			native, err := OpenFilesystemRoot(context.Background(), budget, dir, DefaultFilesystemLimits())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := native.Close(); err != nil {
					t.Error(err)
				}
			})
			proof, err := native.BindTarget(name, "outside-report", originalSource)
			if err != nil || proof.Target().ResourceKey != (Key{}) {
				t.Fatal("NoKey authority changed", proof.Target(), err)
			}
			settings, err := NewSettings(parser.ParseOptions{}, "index.md", "github", "native-fragment-nokey-v1")
			if err != nil {
				t.Fatal(err)
			}
			op, err := NewOperation(context.Background(), budget, native.Root(), settings, []Target{proof.Target()})
			if err != nil {
				t.Fatal(err)
			}
			reader, err := native.Reader(op, proof)
			if err != nil {
				t.Fatal(err)
			}
			service, err := NewFilesystemService(reader, DefaultServiceLimits())
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
			resolver := fragmentNativeResolver(t, service, original)
			dataReads := 0
			dataRead := native.read
			native.read = func(file *os.File, buffer []byte) (int, error) { dataReads++; return dataRead(file, buffer) }
			found := fragmentIntegrationResolve(t, resolver, originalSource, "outside-report", "#original", false)
			if found.Outcome() != FragmentFound || len(found.Observations()) != 0 || dataReads != 0 {
				t.Fatal("NoKey self used fabricated resource request", found.Outcome(), found.Observations(), dataReads)
			}
			blocked := fragmentIntegrationResolve(t, resolver, originalSource, "outside-report", "secret.md#inside", true)
			if blocked.Outcome() != FragmentContextUnavailable || blocked.Normalized().Class() != BlockedReference || len(blocked.Observations()) != 0 || dataReads != 0 {
				t.Fatal("NoKey self widened root", blocked.Outcome(), dataReads)
			}
			candidateSource := parser.NewSource([]byte("雪\r\n# Candidate\r\n[x](#candidate) [y](#original)\r\n"))
			candidate, err := op.NewView(Candidate, []Replacement{{TargetID: "outside-report", Source: candidateSource}})
			if err != nil {
				t.Fatal(err)
			}
			candidateResolver := fragmentNativeResolver(t, service, candidate)
			for _, row := range []struct {
				destination string
				want        FragmentOutcome
			}{{"#candidate", FragmentFound}, {"#original", FragmentAbsentTarget}} {
				got := fragmentIntegrationResolve(t, candidateResolver, candidateSource, "outside-report", row.destination, false)
				index, ok := got.Anchors().Index()
				if got.Outcome() != row.want || !ok || index.Source() != candidateSource || dataReads != 0 {
					t.Fatal("candidate recovered original/native bytes", got.Outcome(), ok, dataReads)
				}
			}
			start := strings.Index(string(candidateSource.Bytes()), "#candidate")
			span, err := candidateSource.Span(start, start+len("#candidate"))
			if err != nil {
				t.Fatal(err)
			}
			link, err := NewLink("outside-report", "MD051", "#candidate", candidateSource, span)
			if err != nil {
				t.Fatal(err)
			}
			query, err := candidateResolver.Prepare(link)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			stopped, err := candidateResolver.Resolve(ctx, query, false)
			if !errors.Is(err, context.Canceled) || stopped.Outcome() != FragmentContextUnavailable {
				t.Fatal("cancelled named query appeared complete", stopped.Outcome(), err)
			}
			if index, ok := stopped.Anchors().Index(); !ok || index.Source() != candidateSource {
				t.Fatal("stopped query erased available index", ok)
			}
			if content, _, ok := stopped.Anchors().Content(); !ok || content != candidateSource {
				t.Fatal("stopped query erased content", ok)
			}
			owner, err := NewFragmentReport(candidateResolver, result.Result{})
			if err != nil {
				t.Fatal(err)
			}
			if err := owner.Append(candidate, []FragmentResolution{stopped}); err != nil {
				t.Fatal(err)
			}
			out, err := owner.Finish()
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Coverage) != 1 || len(out.Interruptions) != 1 || len(out.Errors) != 0 || len(out.Files) != 0 || len(out.Diagnostics) != 0 {
				t.Fatal("NoKey query projection effects", out)
			}
			coverage := out.Coverage[0]
			digest := sha256.Sum256(candidateSource.Bytes())
			if coverage.Path != "outside-report" || coverage.Rule != "MD051" || coverage.Range == nil || coverage.Range.Start.Line != 3 || coverage.Range.Start.Column != 5 || coverage.Range.End.Line != 3 || coverage.Range.End.Column != 15 || coverage.Snapshot == nil || coverage.Snapshot.View != "candidate" || coverage.Snapshot.SizeBytes != candidateSource.Len() || coverage.Snapshot.SHA256 != fmt.Sprintf("%x", digest) {
				t.Fatal("NoKey source/range/SHA/view attribution", coverage)
			}
			if len(coverage.CauseIDs) != 1 || coverage.CauseIDs[0] != out.Interruptions[0].ID || out.Interruptions[0].Reason != "cancelled" {
				t.Fatal("NoKey stopped query cause relation", coverage, out.Interruptions)
			}
			// Every selected view keeps the real NoKey reporting identity;
			// source metadata remains independent of cause-resource identity.
			savedSource := parser.NewSource([]byte("雪雪\r\n# Saved\r\n[x](#saved)\r\n"))
			savedView, err := op.NewView(Saved, []Replacement{{TargetID: "outside-report", Source: savedSource}})
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range []struct {
				view        View
				source      parser.Source
				destination string
				line        int
			}{{original, originalSource, "#original", 2}, {candidate, candidateSource, "#candidate", 3}, {savedView, savedSource, "#saved", 3}} {
				local := fragmentNativeResolver(t, service, row.view)
				warm, err := service.TargetAnchors(context.Background(), row.view, "outside-report")
				if err != nil || warm.Availability() != Available {
					t.Fatal(warm.Availability(), err)
				}
				start := strings.Index(string(row.source.Bytes()), row.destination)
				span, _ := row.source.Span(start, start+len(row.destination))
				link, _ := NewLink("outside-report", "MD051", row.destination, row.source, span)
				query, err := local.Prepare(link)
				if err != nil {
					t.Fatal(err)
				}
				stopped, err := local.Resolve(ctx, query, false)
				if !errors.Is(err, context.Canceled) || stopped.Anchors().Availability() != Available {
					t.Fatal(stopped.Outcome(), err)
				}
				owner, err := NewFragmentReport(local, fragmentCauseInitial("outside-report", originalSource))
				if err != nil {
					t.Fatal(err)
				}
				if err := owner.Append(row.view, []FragmentResolution{stopped}); err != nil {
					t.Fatal(err)
				}
				out, err := owner.Finish()
				if err != nil {
					t.Fatal(err)
				}
				location := result.Range{Start: parser.Position{Line: row.line, Column: 5}, End: parser.Position{Line: row.line, Column: 5 + len(row.destination)}}
				wants := []fragmentCauseWant{{"outside-report", "cancelled"}}
				assertFragmentCauseReport(t, out, "outside-report", row.source, row.view.Phase(), location, wants)
				finalize(&out)
				assertFragmentCauseReport(t, out, "outside-report", row.source, row.view.Phase(), location, wants)
				validateFragmentCauseSchema(t, out)
			}
			unavailable, err := op.NewView(Saved, []Replacement{{TargetID: "outside-report", Unavailable: true}})
			if err != nil {
				t.Fatal(err)
			}
			unavailableResolver := fragmentNativeResolver(t, service, unavailable)
			if _, err := unavailableResolver.Prepare(link); !errors.Is(err, ErrContract) {
				t.Fatal("unavailable Saved accepted stale candidate Link", err)
			}
			if op.Root() != native.Root() || op.Targets()[0].ResourceKey != (Key{}) || dataReads != 0 {
				t.Fatal("NoKey report acquired root/key or reopened content", dataReads)
			}
		})
	}
}
