//go:build linux || darwin || windows

package resource

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func existenceNativeResolver(t *testing.T, service *Service, view View) *ExistenceResolver {
	t.Helper()
	normalizer, err := NewNormalizer(view, DefaultPathLimits(), NativeNamePolicy)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewExistenceResolver(service, normalizer)
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func existenceResolve(t *testing.T, resolver *ExistenceResolver, source parser.Source, destination string) ExistenceResolution {
	t.Helper()
	whole, _ := source.Span(0, source.Len())
	text, _ := source.Text(whole)
	start := strings.Index(text, "("+destination+")") + 1
	if start <= 0 {
		t.Fatalf("destination %q absent from fixture source", destination)
	}
	span, err := source.Span(start, start+len(destination))
	if err != nil {
		t.Fatal(err)
	}
	link, err := NewLink("report.md", "MD057", destination, source, span)
	if err != nil {
		t.Fatal(err)
	}
	query, err := resolver.Prepare(link)
	if err != nil {
		t.Fatal(err)
	}
	// A read failure completes with context-unavailable and returns its cause.
	resolution, err := resolver.Resolve(context.Background(), query)
	if resolution.Outcome() == "" || err != nil && resolution.Outcome() != ExistenceContextUnavailable {
		t.Fatal(destination, err)
	}
	return resolution
}

func TestExistenceResolverOutcomes(t *testing.T) {
	rows := []struct {
		destination string
		want        ExistenceOutcome
		reads       int
	}{
		{"guide.md", ExistenceFound, 1},
		{"guide.md#%ZZ", ExistenceFound, 1},
		{"logo.png", ExistenceFound, 1},
		{"missing.md", ExistenceMissing, 1},
		{"missing.md#intro", ExistenceMissing, 1},
		{"guide.md/", ExistenceFileWithHint, 1},
		{"sub/", ExistenceFound, 2},
		{"sub", ExistenceFound, 2},
		{"bare/", ExistenceIndexMissing, 2},
		{"sub2/", ExistenceIndexIsDirectory, 2},
		{"./", ExistenceIndexMissing, 1},
		{"source.md", ExistenceFound, 0},
		{"./source.md?v=1", ExistenceFound, 0},
		{"https://example.test/a.md", ExistenceSkipped, 0},
		{"//host/a.md", ExistenceSkipped, 0},
		{"../outside.md", ExistenceContextUnavailable, 0},
		{"C:/x.md", ExistenceContextUnavailable, 0},
	}
	var destinations []string
	for _, row := range rows {
		destinations = append(destinations, row.destination)
	}
	f := fragmentNativeFixture(t, "source.md", "index.md", destinations)
	f.write(t, "guide.md", []byte("# Guide\n"))
	f.write(t, "logo.png", []byte{0xff, 0, 0xfe})
	f.write(t, "sub/index.md", []byte("# Sub\n"))
	f.write(t, "bare/README.md", []byte("# Bare\n"))
	f.write(t, "sub2/index.md/index.md", []byte("# Nested\n"))
	service := filesystemTestService(t, f)
	resolver := existenceNativeResolver(t, service, f.view)
	dataReads := 0
	read := f.filesystem.read
	f.filesystem.read = func(file *os.File, buffer []byte) (int, error) { dataReads++; return read(file, buffer) }
	for _, row := range rows {
		got := existenceResolve(t, resolver, f.source, row.destination)
		observations := got.Observations()
		if got.Outcome() != row.want || len(observations) != row.reads || got.Err() != nil {
			t.Fatalf("%q: outcome=%q reads=%d err=%v", row.destination, got.Outcome(), len(observations), got.Err())
		}
		for _, o := range observations {
			if o.Request().Capability() != ExistenceOnly {
				t.Fatal("content requested", row.destination, o.Request().Capability())
			}
		}
		if row.want.Absent() != (len(observations) != 0 && observations[len(observations)-1].Treatment() == MissingTarget) && row.destination != "./" {
			t.Fatal("outcome disagrees with Treatment", row.destination, observations)
		}
		if len(observations) == 2 && observations[1].Request().RequiredType() != RegularFile {
			t.Fatal("index request is not a regular-file request", observations[1].Request())
		}
	}
	if dataReads != 0 {
		t.Fatal("existence lookups read content", dataReads)
	}
}

func TestExistenceResolverConfiguredIndexOnly(t *testing.T) {
	f := fragmentNativeFixture(t, "source.md", "README.md", []string{"bare/", "sub/"})
	f.write(t, "bare/README.md", []byte("# Bare\n"))
	f.write(t, "sub/index.md", []byte("# Sub\n"))
	resolver := existenceNativeResolver(t, filesystemTestService(t, f), f.view)
	if got := existenceResolve(t, resolver, f.source, "bare/"); got.Outcome() != ExistenceFound {
		t.Fatal("configured index not used", got.Outcome())
	}
	if got := existenceResolve(t, resolver, f.source, "sub/"); got.Outcome() != ExistenceIndexMissing || len(got.Observations()) != 2 {
		t.Fatal("default index name was tried", got.Outcome())
	}
}

func TestExistenceResolverSelectedIndexIsSelf(t *testing.T) {
	f := fragmentNativeFixture(t, "docs/index.md", "index.md", []string{"./", "../docs/"})
	resolver := existenceNativeResolver(t, filesystemTestService(t, f), f.view)
	for _, destination := range []string{"./", "../docs/"} {
		if got := existenceResolve(t, resolver, f.source, destination); got.Outcome() != ExistenceFound || len(got.Observations()) != 1 {
			t.Fatal(destination, got.Outcome(), got.Observations())
		}
	}
}

func TestExistenceReportProjection(t *testing.T) {
	destinations := []string{"missing.md", "../outside.md", "linked.md", "locked/a.md", "locked/a.md#x"}
	f := fragmentNativeFixture(t, "source.md", "index.md", destinations)
	f.write(t, "target.md", []byte("# Target\n"))
	symlinks := runtime.GOOS != "windows"
	if symlinks {
		if err := os.Symlink("target.md", filepath.Join(f.dir, "linked.md")); err != nil {
			t.Fatal(err)
		}
	}
	locked := runtime.GOOS != "windows" && os.Geteuid() != 0
	if locked {
		if err := os.Mkdir(filepath.Join(f.dir, "locked"), 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(f.dir, "locked"), 0o700) }) //nolint:gosec // Restore the synthetic directory so cleanup can remove it.
	}
	service := filesystemTestService(t, f)
	resolver := existenceNativeResolver(t, service, f.view)
	var resolutions []ExistenceResolution
	for _, destination := range destinations {
		resolutions = append(resolutions, existenceResolve(t, resolver, f.source, destination))
	}
	report, err := NewExistenceReport(resolver, result.Result{})
	if err != nil {
		t.Fatal(err)
	}
	if err := report.Append(f.view, resolutions); err != nil {
		t.Fatal(err)
	}
	out, err := report.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if resolutions[0].Outcome() != ExistenceMissing {
		t.Fatal(resolutions[0].Outcome())
	}
	coverage := map[string]result.Coverage{}
	whole, _ := f.source.Span(0, f.source.Len())
	text, _ := f.source.Text(whole)
	for _, c := range out.Coverage {
		if c.Rule != "MD057" || c.Path != "report.md" || c.Reason != result.ContextUnavailable || c.Range == nil || c.Snapshot == nil {
			t.Fatal(c)
		}
		line := strings.Split(text, "\n")[c.Range.Start.Line-1]
		coverage[line] = c
	}
	want := 1
	if symlinks {
		want++
		if resolutions[2].Outcome() != ExistenceContextUnavailable {
			t.Fatal("symlink followed", resolutions[2].Outcome())
		}
	}
	if locked {
		want += 2
		if len(out.Errors) != 1 || out.Errors[0].Category != result.IO || out.Errors[0].Path != "locked/a.md" {
			t.Fatal("read failure not one attributed io error", out.Errors)
		}
		for _, line := range []string{"[query](locked/a.md)", "[query](locked/a.md#x)"} {
			if c := coverage[line]; len(c.CauseIDs) == 0 {
				t.Fatal("read failure coverage has no cause", line, c)
			}
		}
	}
	if len(out.Coverage) != want {
		t.Fatal(out.Coverage, out.Errors)
	}
	if _, found := coverage["[query](../outside.md)"]; !found {
		t.Fatal("root escape is not located coverage", out.Coverage)
	}
	if err := report.Append(f.view, resolutions); err == nil {
		t.Fatal("finished report accepted a batch")
	}
}
