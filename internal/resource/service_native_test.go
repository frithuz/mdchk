//go:build linux || darwin || windows

package resource

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func filesystemTestService(t *testing.T, f *filesystemFixture) *Service {
	t.Helper()
	service, err := NewFilesystemService(f.reader, DefaultServiceLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})
	return service
}

func filesystemTestNextSession(t *testing.T, f *filesystemFixture) *filesystemFixture {
	t.Helper()
	budget, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	native, err := f.filesystem.Session(context.Background(), budget, DefaultFilesystemLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := native.Close(); err != nil {
			t.Error(err)
		}
	})
	data, err := os.ReadFile(filepath.Join(f.dir, "source.md"))
	if err != nil {
		t.Fatal(err)
	}
	source := parser.NewSource(data)
	proof, err := native.BindTarget(filepath.Join(f.dir, "source.md"), "report.md", source)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := NewOperation(context.Background(), budget, native.Root(), f.operation.settings, []Target{proof.Target()})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := native.Reader(operation, proof)
	if err != nil {
		t.Fatal(err)
	}
	view, err := operation.NewView(Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &filesystemFixture{dir: f.dir, filesystem: native, reader: reader, operation: operation, view: view, source: source, budget: budget, proof: proof}
}

func TestFilesystemServiceCachedMissAndFreshSession(t *testing.T) {
	f := newFilesystemFixture(t)
	service := filesystemTestService(t, f)
	calls := 0
	read := service.read
	service.read = func(ctx context.Context, q Request, receipt filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
		calls++
		return read(ctx, q, receipt)
	}
	request := f.request(t, "appeared.md", TextContent, RegularFile)
	first, err := service.Read(context.Background(), request)
	if err != nil || first.Existence != Missing || calls != 1 {
		t.Fatal("first actual native miss was not committed", first, calls, err)
	}
	f.write(t, "appeared.md", []byte("created after observation"))
	second, err := service.Read(context.Background(), f.request(t, "appeared.md", TextContent, RegularFile))
	if err != nil || second.Existence != Missing || calls != 1 {
		t.Fatal("cached miss was evicted or reacquired", second, calls, err)
	}
	next := filesystemTestNextSession(t, f)
	nextService := filesystemTestService(t, next)
	fresh, err := nextService.Read(context.Background(), next.request(t, "appeared.md", TextContent, RegularFile))
	if err != nil || fresh.Existence != File || fresh.Content != Available || string(fresh.Source.Bytes()) != "created after observation" {
		t.Fatal("new session reused a previous operation's missing fact", fresh, err)
	}
}

func TestFilesystemServiceFirstContentAcrossViewsAndSession(t *testing.T) {
	f := newFilesystemFixture(t)
	f.write(t, "asset.md", []byte("first\r\n"))
	service := filesystemTestService(t, f)
	calls := 0
	read := service.read
	service.read = func(ctx context.Context, q Request, receipt filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
		calls++
		return read(ctx, q, receipt)
	}
	first, err := service.Read(context.Background(), f.request(t, "asset.md", TextContent, RegularFile))
	if err != nil || first.Content != Available || string(first.Source.Bytes()) != "first\r\n" {
		t.Fatal(first, err)
	}
	f.write(t, "asset.md", []byte("mutated\n"))
	for _, phase := range []Phase{Original, Candidate, Saved} {
		view, err := f.operation.NewView(phase, nil)
		if err != nil {
			t.Fatal(err)
		}
		q := f.request(t, "asset.md", TextContent, RegularFile, view)
		out, err := service.Read(context.Background(), q)
		if err != nil || out.Source != first.Source || string(out.Source.Bytes()) != "first\r\n" || calls != 1 {
			t.Fatal("resource-only first content changed between views", phase, out, calls, err)
		}
		if observation, err := f.operation.Record(q, out); err != nil || !observation.Request().View().Same(view) {
			t.Fatal("cached fact did not bind to current view", observation, err)
		}
	}
	next := filesystemTestNextSession(t, f)
	fresh, err := filesystemTestService(t, next).Read(context.Background(), next.request(t, "asset.md", TextContent, RegularFile))
	if err != nil || string(fresh.Source.Bytes()) != "mutated\n" || fresh.Source == first.Source {
		t.Fatal("new operation shared stale content/source", fresh, err)
	}
}

func TestFilesystemServiceLazyContentAndReplacement(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-object", true: "replacement"}[replace], func(t *testing.T) {
			f := newFilesystemFixture(t)
			f.write(t, "asset.md", []byte("before"))
			service := filesystemTestService(t, f)
			reads := 0
			read := f.filesystem.read
			f.filesystem.read = func(file *os.File, data []byte) (int, error) {
				reads++
				return read(file, data)
			}
			first, err := service.Read(context.Background(), f.request(t, "asset.md", ExistenceOnly, RegularFile))
			if err != nil || first.Existence != File || first.Content != NotRequested || reads != 0 {
				t.Fatal("existence query read unrequested bytes", first, reads, err)
			}
			if replace {
				if err := os.Rename(filepath.Join(f.dir, "asset.md"), filepath.Join(f.dir, "retained-old.md")); err != nil {
					t.Fatal(err)
				}
			}
			f.write(t, "asset.md", []byte("later longer bytes"))
			content, err := service.Read(context.Background(), f.request(t, "asset.md", TextContent, RegularFile))
			if replace {
				if !errors.Is(err, errFilesystemReceiptChanged) || content.Existence != File || content.Content != Failed || reads != 0 {
					t.Fatal("content upgrade silently changed object", content, reads, err)
				}
			} else if err != nil || content.Content != Available || string(content.Source.Bytes()) != "later longer bytes" || reads == 0 {
				t.Fatal("first content incorrectly claimed historical unseen bytes", content, reads, err)
			}
			beforeHit := reads
			known, hitErr := service.Read(context.Background(), f.request(t, "asset.md", ExistenceOnly, RegularFile))
			if known.Existence != File || known.Content != NotRequested || reads != beforeHit || replace && !errors.Is(hitErr, errFilesystemReceiptChanged) || !replace && hitErr != nil {
				t.Fatal("later existence lost its first fact or encountered failure", known, reads, hitErr)
			}
		})
	}
}

func TestFilesystemServiceImageExistenceNeverReadsData(t *testing.T) {
	f := newFilesystemFixture(t)
	f.write(t, "image.png", []byte{0xff, 0xfe, 0x00, 0x81})
	service := filesystemTestService(t, f)
	reads := 0
	f.filesystem.read = func(_ *os.File, _ []byte) (int, error) {
		reads++
		return 0, errors.New("image payload must not be read")
	}
	for range 3 {
		out, err := service.Read(context.Background(), f.request(t, "image.png", ExistenceOnly, RegularFile))
		if err != nil || out.Existence != File || out.Content != NotRequested || reads != 0 {
			t.Fatal("existence-only image required binary loading", out, reads, err)
		}
	}
}

func TestFilesystemServicePartialEnumerationNeverProvesMissing(t *testing.T) {
	f := newFilesystemFixture(t)
	f.write(t, "present-a.md", []byte("a"))
	f.write(t, "present-b.md", []byte("b"))
	service := filesystemTestService(t, f)
	// Real pinned enumeration has more than one entry. Allow one more entry
	// after target binding, then interrupt the scan before it can prove absence.
	f.filesystem.limits.DirectoryEntries = f.filesystem.entries + 1
	seen := 0
	f.filesystem.hook = func(stage, _ string) error {
		if stage == "entry" {
			seen++
		}
		return nil
	}
	q := f.request(t, "missing.md", ExistenceOnly, RegularFile)
	first, err := service.Read(context.Background(), q)
	var limited *parser.LimitExceeded
	if !errors.As(err, &limited) || limited.Name != "directory-entries" || first.Existence != Unknown || first.Content != Interrupted || seen != 1 {
		t.Fatal("partial enumeration became authoritative absence", first, seen, err)
	}
	entries := f.filesystem.entries
	initialLimit := *limited
	second, err := service.Read(context.Background(), f.request(t, "missing.md", ExistenceOnly, RegularFile))
	if !errors.As(err, &limited) || *limited != initialLimit || second.Existence != Unknown || second.Content != Interrupted || seen != 1 || f.filesystem.entries != entries {
		t.Fatal("cached incomplete scan was reread or made missing", second, seen, err)
	}
}

func TestFilesystemServiceContentFailureKeepsExistence(t *testing.T) {
	f := newFilesystemFixture(t)
	f.write(t, "asset.md", []byte("unreadable through data seam"))
	service := filesystemTestService(t, f)
	first, err := service.Read(context.Background(), f.request(t, "asset.md", ExistenceOnly, RegularFile))
	if err != nil || first.Existence != File || first.Content != NotRequested {
		t.Fatal(first, err)
	}
	failure := errors.New("actual data read failure")
	reads := 0
	f.filesystem.read = func(_ *os.File, _ []byte) (int, error) {
		reads++
		return 0, failure
	}
	out, err := service.Read(context.Background(), f.request(t, "asset.md", TextContent, RegularFile))
	if !errors.Is(err, failure) || out.Existence != File || out.Content != Failed || reads != 1 {
		t.Fatal("failed content lost independently known existence", out, reads, err)
	}
	for _, capability := range []Capability{ExistenceOnly, TextContent} {
		request := f.request(t, "asset.md", capability, RegularFile)
		out, err := service.Read(context.Background(), request)
		if !errors.Is(err, failure) || out.Existence != File || reads != 1 {
			t.Fatal("encountered read failure was erased or retried", out, reads, err)
		}
		observation, recordErr := f.operation.Record(request, out)
		if recordErr != nil || capability == ExistenceOnly && observation.Treatment() != Complete || capability == TextContent && observation.Treatment() != ContextIncomplete {
			t.Fatal("cache mixed existence and failed-content capability", observation, recordErr)
		}
	}
}

func TestFilesystemServiceRefusesContentAfterUnprovedFirstFile(t *testing.T) {
	f := newFilesystemFixture(t)
	f.write(t, "asset.md", []byte("first"))
	service := filesystemTestService(t, f)
	changes := 0
	f.filesystem.hook = func(stage, key string) error {
		if stage == "before-verify" && key == "asset.md" {
			changes++
			if err := os.Rename(filepath.Join(f.dir, key), filepath.Join(f.dir, fmt.Sprintf("history-%d.md", changes))); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(f.dir, key), []byte("replacement"), 0600)
		}
		return nil
	}
	first, err := service.Read(context.Background(), f.request(t, "asset.md", ExistenceOnly, RegularFile))
	if !errors.Is(err, errFilesystemUnstable) || first.Existence != File || changes != 3 {
		t.Fatal("fixture did not exhaust actual stable metadata attempts", first, err, changes)
	}
	key, _ := NewKey("asset.md")
	entry := service.entries[serviceKey{key: key}]
	if entry.receipt != (filesystemReceipt{}) {
		t.Fatal("unstable first observation minted identity proof")
	}
	f.filesystem.hook = nil
	if err := os.Rename(filepath.Join(f.dir, "asset.md"), filepath.Join(f.dir, "last-old.md")); err != nil {
		t.Fatal(err)
	}
	f.write(t, "asset.md", []byte("unrelated final object"))
	reads := 0
	read := f.filesystem.read
	f.filesystem.read = func(file *os.File, b []byte) (int, error) { reads++; return read(file, b) }
	witnesses := len(service.validations)
	caller, cancel := context.WithCancel(context.Background())
	cancel()
	stopped, stopErr := service.Read(caller, f.request(t, "asset.md", TextContent, RegularFile))
	if stopped.Existence != File || !errors.Is(stopErr, context.Canceled) || !errors.Is(stopErr, errFilesystemUnstable) {
		t.Fatal("caller stop erased the unproved first File's actual IO", stopped, stopErr)
	}
	for range 3 {
		q := f.request(t, "asset.md", TextContent, RegularFile)
		spec, err := service.Read(context.Background(), q)
		if spec.Existence != File || spec.Content == Available || spec.Source != (parser.Source{}) || spec.Validated != (ContentEvidence{}) || !errors.Is(err, errFilesystemUnstable) || reads != 0 {
			t.Fatal("unproved first identity upgraded to replacement content", spec, err, reads)
		}
		observation, recordErr := f.operation.Record(q, spec)
		if recordErr != nil || observation.Treatment() != ContextIncomplete {
			t.Fatal("refusal lost known File or context deficit", observation, recordErr)
		}
		if _, err := service.ParseKey(f.view, spec.Validated); !errors.Is(err, ErrContract) {
			t.Fatal("unproved replacement gained parse permission", err)
		}
		existence, err := service.Read(context.Background(), f.request(t, "asset.md", ExistenceOnly, RegularFile))
		if existence.Existence != File || !errors.Is(err, errFilesystemUnstable) {
			t.Fatal("refusal erased first File or IO", existence, err)
		}
	}
	if len(service.validations) != witnesses || !entry.hasContent || reads != 0 {
		t.Fatal("failed capability was retried/admitted", reads, len(service.validations), witnesses)
	}
}
