//go:build linux || darwin || windows

package resource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func TestFilesystemLifecycleCancellationAuthorities(t *testing.T) {
	for _, authority := range []string{"root", "operation", "caller"} {
		t.Run(authority, func(t *testing.T) {
			rootContext, cancelRoot := context.WithCancel(context.Background())
			defer cancelRoot()
			operationContext, cancelOperation := context.WithCancel(context.Background())
			defer cancelOperation()
			callerContext, cancelCaller := context.WithCancel(context.Background())
			defer cancelCaller()
			f := newFilesystemFixtureOptions(t, rootContext, operationContext, parser.DefaultLimits(), DefaultFilesystemLimits())
			f.write(t, "asset.md", []byte("never read after cancellation"))
			request := f.request(t, "asset.md", TextContent, RegularFile)
			calls := 0
			open, metadata := f.filesystem.open, f.filesystem.metadata
			f.filesystem.open = func(parent *os.File, name string, directory bool) (*os.File, error) {
				calls++
				return open(parent, name, directory)
			}
			f.filesystem.metadata = func(parent *os.File, name string, directory bool) (*os.File, error) {
				calls++
				return metadata(parent, name, directory)
			}
			switch authority {
			case "root":
				cancelRoot()
			case "operation":
				cancelOperation()
			case "caller":
				cancelCaller()
			}
			spec, err := f.reader.Read(callerContext, request)
			if !errors.Is(err, context.Canceled) || calls != 0 || spec.Existence != Unknown || spec.Content != Interrupted || spec.Problem.Kind != Cancelled {
				t.Fatal("cancelled authority started native work or invented absence", authority, spec, err, calls)
			}
			observation, recordErr := f.operation.Record(request, spec)
			if recordErr != nil || observation.Treatment() != ContextIncomplete {
				t.Fatal("cancellation fact could not retain incomplete source coverage", observation, recordErr)
			}
		})
	}
}

func TestFilesystemLifecycleCancellationDuringAcquisition(t *testing.T) {
	for _, stage := range []string{"entry", "before-read"} {
		t.Run(stage, func(t *testing.T) {
			f := newFilesystemFixture(t)
			f.write(t, "asset.md", []byte("resource"))
			request := f.request(t, "asset.md", TextContent, RegularFile)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			visited, reads := 0, 0
			f.filesystem.hook = func(at, key string) error {
				if at == stage && key == "asset.md" {
					visited++
					cancel()
				}
				return nil
			}
			read := f.filesystem.read
			f.filesystem.read = func(file *os.File, b []byte) (int, error) {
				reads++
				return read(file, b)
			}
			spec, err := f.reader.Read(ctx, request)
			wantExistence := Unknown
			if stage == "before-read" {
				wantExistence = File
			}
			if visited == 0 || reads != 0 || !errors.Is(err, context.Canceled) || spec.Existence != wantExistence || spec.Content != Interrupted {
				t.Fatal("checkpoint lost known facts or performed a cancelled read", stage, spec, err, visited, reads)
			}
			if _, err := f.operation.Record(request, spec); err != nil {
				t.Fatal("interrupted native spec invalid", err)
			}
		})
	}
}

func TestFilesystemLifecycleCancelledOperationStopsExistenceOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := newFilesystemFixtureOptions(t, context.Background(), ctx, parser.DefaultLimits(), DefaultFilesystemLimits())
	request := f.request(t, "asset.md", ExistenceOnly, RegularFile)
	f.filesystem.open = func(*os.File, string, bool) (*os.File, error) {
		t.Fatal("cancelled operation opened native metadata")
		return nil, ErrContract
	}
	f.filesystem.metadata = f.filesystem.open
	cancel()
	spec, err := f.reader.Read(context.Background(), request)
	if !errors.Is(err, context.Canceled) || spec.Existence != Unknown || spec.Problem.Kind != Cancelled {
		t.Fatal("existence-only bypassed operation cancellation", spec, err)
	}
}

func TestFilesystemLifecycleCancellationBeforeRetry(t *testing.T) {
	f := newFilesystemFixture(t)
	f.write(t, "asset.md", []byte("discard unstable acquisition"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts, retries := 0, 0
	f.filesystem.hook = func(stage, key string) error {
		if key != "asset.md" {
			return nil
		}
		if stage == "before-content-open" {
			attempts++
		}
		if stage == "before-verify" {
			return errFilesystemUnstable
		}
		if stage == "retry" {
			retries++
			cancel()
		}
		return nil
	}
	spec, err := f.reader.Read(ctx, f.request(t, "asset.md", TextContent, RegularFile))
	if attempts != 1 || retries != 1 || !errors.Is(err, context.Canceled) || spec.Existence != File || spec.Content != Interrupted || spec.Source != (parser.Source{}) {
		t.Fatal("cancelled retry performed another acquisition or returned unstable bytes", attempts, retries, spec, err)
	}
}

func TestFilesystemLifecycleGateWaitHonorsOperationCancellation(t *testing.T) {
	// The filesystem setup uses real native calls. Only the held gate and timer
	// wait are modeled by this bubble; it makes no claim about kernel I/O timing.
	synctest.Test(t, func(t *testing.T) {
		operationContext, cancel := context.WithCancel(context.Background())
		defer cancel()
		f := newFilesystemFixtureOptions(t, context.Background(), operationContext, parser.DefaultLimits(), DefaultFilesystemLimits())
		request := f.request(t, "asset.md", TextContent, RegularFile)
		<-f.filesystem.gate
		defer func() { f.filesystem.gate <- struct{}{} }()
		done := make(chan error, 1)
		go func() {
			_, err := f.reader.Read(context.Background(), request)
			done <- err
		}()
		synctest.Wait()
		cancel()
		// The wrapper preserves the separate operation's Err authority. Its
		// background caller has no Done channel, so the finite meter poll must
		// still release the waiter without releasing acquisition permission.
		time.Sleep(10 * time.Millisecond)
		synctest.Wait()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("operation cancellation lost while gate waited", err)
			}
		default:
			t.Fatal("operation cancellation could not release the gate waiter")
		}
	})
}

func TestFilesystemLifecycleCountersSurviveReaderReplacement(t *testing.T) {
	t.Run("same operation cannot reset through Session", func(t *testing.T) {
		f := newFilesystemFixture(t)
		if _, err := f.filesystem.Session(context.Background(), f.budget, DefaultFilesystemLimits()); !errors.Is(err, ErrContract) {
			t.Fatal("session reset an existing operation's counters", err)
		}
	})
	t.Run("directory entries", func(t *testing.T) {
		limits := DefaultFilesystemLimits()
		limits.DirectoryEntries = 3 // Two source-binding scans, then one miss.
		f := newFilesystemFixtureOptions(t, context.Background(), context.Background(), parser.DefaultLimits(), limits)
		request := f.request(t, "absent.md", ExistenceOnly, RegularFile)
		first, err := f.reader.Read(context.Background(), request)
		if err != nil || first.Existence != Missing {
			t.Fatal("complete first scan did not prove absence", first, err)
		}
		used := f.filesystem.entries
		reader, err := f.filesystem.Reader(f.operation, f.proof)
		if err != nil || f.filesystem.entries != used {
			t.Fatal("reader replacement changed operation counter", err, used, f.filesystem.entries)
		}
		second, err := reader.Read(context.Background(), request)
		var exceeded *parser.LimitExceeded
		if !errors.As(err, &exceeded) || exceeded.Name != "directory-entries" || second.Existence == Missing || second.Content != Interrupted {
			t.Fatal("partial repeated scan became absence/reset allowance", second, err)
		}
		if _, err := f.filesystem.Reader(f.operation, f.proof); !errors.As(err, &exceeded) {
			t.Fatal("a new reader cleared sticky operation exhaustion", err)
		}
	})
	t.Run("content bytes", func(t *testing.T) {
		parserLimits := parser.DefaultLimits()
		parserLimits.DocumentBytes = 64
		limits := DefaultFilesystemLimits()
		limits.TotalContentBytes = 96
		f := newFilesystemFixtureOptions(t, context.Background(), context.Background(), parserLimits, limits)
		f.write(t, "asset.md", []byte(strings.Repeat("x", 40)))
		request := f.request(t, "asset.md", TextContent, RegularFile)
		first, err := f.reader.Read(context.Background(), request)
		if err != nil || first.Content != Available {
			t.Fatal(first, err)
		}
		used := f.filesystem.bytes
		if used != int64(f.source.Len())+40 {
			t.Fatal("submitted target/actual resource bytes not counted", used)
		}
		reader, err := f.filesystem.Reader(f.operation, f.proof)
		if err != nil || f.filesystem.bytes != used {
			t.Fatal("reader reseeded or reset bytes", err, used, f.filesystem.bytes)
		}
		second, err := reader.Read(context.Background(), request)
		var exceeded *parser.LimitExceeded
		if !errors.As(err, &exceeded) || exceeded.Name != "total-content-bytes" || second.Existence != File || second.Content != Interrupted {
			t.Fatal("repeated native acquisition reset aggregate bytes or erased File", second, err)
		}
	})
}

func TestFilesystemLifecycleReaderConstructionRequiresBudget(t *testing.T) {
	f := newFilesystemFixture(t)
	limits := parser.DefaultLimits()
	limits.DocumentNodes, limits.TotalNodes = 1, 7
	budget := rootPolicyBudget(t, limits)
	session, err := f.filesystem.Session(context.Background(), budget, DefaultFilesystemLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Error(err)
		}
	})
	// This budget admits the session and one outside-source operation, but
	// cannot retain a further reader's records. No native membership is forged.
	operation, err := NewOperation(context.Background(), budget, session.Root(), f.operation.Settings(), []Target{{ID: "outside.md", Source: parser.NewSource([]byte("x"))}})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := session.Reader(operation)
	var exceeded *parser.LimitExceeded
	if reader != nil || !errors.As(err, &exceeded) || exceeded.Name != "total-nodes" || session.operation != nil || session.bytes != 0 {
		t.Fatal("reader was constructed/published without record allowance", reader, err)
	}
}

func TestFilesystemLifecycleStableRetryAndExhaustion(t *testing.T) {
	for _, continued := range []bool{false, true} {
		t.Run(fmt.Sprintf("continued=%v", continued), func(t *testing.T) {
			f := newFilesystemFixture(t)
			f.write(t, "asset.md", []byte("first"))
			request := f.request(t, "asset.md", TextContent, RegularFile)
			attempts := 0
			f.filesystem.hook = func(stage, key string) error {
				if stage == "before-verify" && key == "asset.md" {
					attempts++
					if continued || attempts == 1 {
						if err := os.Rename(filepath.Join(f.dir, "asset.md"), filepath.Join(f.dir, fmt.Sprintf("history-%d.md", attempts))); err != nil {
							return err
						}
						f.write(t, "asset.md", []byte("replacement"))
					}
				}
				return nil
			}
			spec, err := f.reader.Read(context.Background(), request)
			if continued {
				if attempts != f.filesystem.limits.SnapshotRetries+1 || !errors.Is(err, errFilesystemUnstable) || spec.Existence != File || spec.Content != Failed || spec.Source != (parser.Source{}) {
					t.Fatal("unstable data escaped or retries were unbounded", attempts, spec, err)
				}
			} else if err != nil || attempts != 2 || spec.Content != Available || string(spec.Source.Bytes()) != "replacement" {
				t.Fatal("retry returned stale/mixed data", attempts, spec, err)
			}
			if f.filesystem.bytes < int64(f.source.Len())+int64(len("first"))+int64(len("replacement")) {
				t.Fatal("failed-attempt bytes were not cumulative", f.filesystem.bytes)
			}
		})
	}
}

func TestFilesystemLifecycleInstabilityCannotEraseCloseFailure(t *testing.T) {
	f := newFilesystemFixture(t)
	f.write(t, "asset.md", []byte("original"))
	var dataHandle *os.File
	open := f.filesystem.open
	f.filesystem.open = func(parent *os.File, name string, directory bool) (*os.File, error) {
		file, err := open(parent, name, directory)
		if name == "asset.md" && !directory {
			dataHandle = file
		}
		return file, err
	}
	closeFailure := errors.New("injected data-handle close failure")
	closeFile := f.filesystem.close
	f.filesystem.close = func(file *os.File) error {
		err := closeFile(file)
		if file == dataHandle {
			return errors.Join(err, closeFailure)
		}
		return err
	}
	attempts := 0
	f.filesystem.hook = func(stage, key string) error {
		if stage == "before-verify" && key == "asset.md" {
			attempts++
			if err := os.Rename(filepath.Join(f.dir, key), filepath.Join(f.dir, "old.md")); err != nil {
				return err
			}
			f.write(t, key, []byte("replacement"))
		}
		return nil
	}
	spec, err := f.reader.Read(context.Background(), f.request(t, "asset.md", TextContent, RegularFile))
	if attempts != 1 || !errors.Is(err, closeFailure) || !errors.Is(err, errFilesystemUnstable) || spec.Existence != File || spec.Source != (parser.Source{}) {
		t.Fatal("retry erased independent close failure", attempts, spec, err)
	}
}

func TestFilesystemLifecycleNativeFailureRetainsConcurrentCancellation(t *testing.T) {
	for _, stage := range []string{"metadata", "read", "close"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := newFilesystemFixtureOptions(t, context.Background(), ctx, parser.DefaultLimits(), DefaultFilesystemLimits())
			f.write(t, "asset.md", []byte("complete native data"))
			failure := errors.New("injected native failure during cancellation")
			switch stage {
			case "metadata":
				metadata := f.filesystem.metadata
				f.filesystem.metadata = func(parent *os.File, name string, directory bool) (*os.File, error) {
					if name == "asset.md" {
						cancel()
						return nil, failure
					}
					return metadata(parent, name, directory)
				}
			case "read":
				f.filesystem.read = func(*os.File, []byte) (int, error) { cancel(); return 0, failure }
			case "close":
				closeFile := f.filesystem.close
				f.filesystem.close = func(file *os.File) error {
					err := closeFile(file)
					if file.Name() == "asset.md" {
						cancel()
						return errors.Join(err, failure)
					}
					return err
				}
			}
			request := f.request(t, "asset.md", TextContent, RegularFile)
			spec, err := f.reader.Read(context.Background(), request)
			var fault *FilesystemReadError
			if !errors.As(err, &fault) || !errors.Is(fault.IO, failure) || !errors.Is(fault.Stop, context.Canceled) || spec.Existence == Missing {
				t.Fatal("native failure hid simultaneous stop", spec, err)
			}
		})
	}
}

func TestFilesystemLifecycleIOAndQuotaRetainBothCauses(t *testing.T) {
	parserLimits := parser.DefaultLimits()
	parserLimits.DocumentBytes = 64
	limits := DefaultFilesystemLimits()
	limits.TotalContentBytes = 64
	f := newFilesystemFixtureOptions(t, context.Background(), context.Background(), parserLimits, limits)
	f.write(t, "asset.md", []byte("x"))
	request := f.request(t, "asset.md", TextContent, RegularFile)
	grew := false
	f.filesystem.hook = func(stage, key string) error {
		if stage == "before-read" && key == "asset.md" && !grew {
			grew = true
			f.write(t, "asset.md", []byte(strings.Repeat("g", 80)))
		}
		return nil
	}
	readFailure := errors.New("injected read failure alongside actual bytes")
	read := f.filesystem.read
	f.filesystem.read = func(file *os.File, b []byte) (int, error) {
		n, _ := read(file, b)
		return n, readFailure
	}
	spec, readErr := f.reader.Read(context.Background(), request)
	var fault *FilesystemReadError
	var exceeded *parser.LimitExceeded
	if !grew || !errors.As(readErr, &fault) || !errors.Is(fault.IO, readFailure) || !errors.As(fault.Stop, &exceeded) || exceeded.Name != "total-content-bytes" || spec.Existence != File || spec.Content != Failed {
		t.Fatal("simultaneous real bytes, I/O and quota were flattened", spec, readErr)
	}
	observation, err := f.operation.Record(request, spec)
	if err != nil {
		t.Fatal(err)
	}
	out := result.NewLint("project")
	report, err := NewFilesystemReport(f.operation, out)
	if err != nil {
		t.Fatal(err)
	}
	if err := report.Append(f.view, []Observation{observation}, []FilesystemFailure{{Reader: f.reader, Request: request, Err: readErr}}); err != nil {
		t.Fatal(err)
	}
	out, err = report.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Errors) != 1 || out.Errors[0].Category != result.IO || len(out.Interruptions) != 1 || out.Interruptions[0].Reason != string(Limited) || len(out.Coverage) != 1 || len(out.Coverage[0].CauseIDs) != 2 {
		t.Fatal("shared cause/coverage projection lost an actual outcome", out)
	}
	if len(out.CheckedFiles)+len(out.Files)+len(out.Changes)+len(out.Documents) != 0 {
		t.Fatal("resource error registered output/target authority")
	}
}

func TestFilesystemLifecycleLateClosePreservesAcquiredSource(t *testing.T) {
	f := newFilesystemFixture(t)
	f.write(t, "asset.md", []byte("verified resource\r\n"))
	request := f.request(t, "asset.md", TextContent, RegularFile)
	var dataHandle *os.File
	open := f.filesystem.open
	f.filesystem.open = func(parent *os.File, name string, directory bool) (*os.File, error) {
		file, err := open(parent, name, directory)
		if err == nil && name == "asset.md" && !directory {
			dataHandle = file
		}
		return file, err
	}
	closeFailure := errors.New("late data close failed")
	closeFile := f.filesystem.close
	f.filesystem.close = func(file *os.File) error {
		err := closeFile(file)
		if file == dataHandle {
			return errors.Join(err, closeFailure)
		}
		return err
	}
	spec, readErr := f.reader.Read(context.Background(), request)
	if !errors.Is(readErr, closeFailure) || spec.Existence != File || spec.Content != Available || string(spec.Source.Bytes()) != "verified resource\r\n" || dataHandle == nil {
		t.Fatal("late cleanup error erased completed content", spec, readErr)
	}
	if _, err := dataHandle.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("real close was not performed before fault injection", err)
	}
	observation, err := f.operation.Record(request, spec)
	if err != nil || observation.Treatment() != Complete {
		t.Fatal("satisfied content acquired false incomplete coverage", observation, err)
	}
	out := result.NewLint("project")
	report, err := NewFilesystemReport(f.operation, out)
	if err != nil {
		t.Fatal(err)
	}
	if err := report.Append(f.view, []Observation{observation}, []FilesystemFailure{{Reader: f.reader, Request: request, Err: readErr}}); err != nil {
		t.Fatal(err)
	}
	out, err = report.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Errors) != 1 || out.Errors[0].Category != result.IO || len(out.Coverage) != 0 {
		t.Fatal("satisfied capability hid encountered I/O or gained false coverage", out)
	}
}

func TestFilesystemLifecycleOwnedHandlesCloseOnEveryExit(t *testing.T) {
	for _, outcome := range []string{"success", "read failure", "cancelled", "unstable"} {
		t.Run(outcome, func(t *testing.T) {
			f := newFilesystemFixture(t)
			f.write(t, "directory/asset.md", []byte("resource"))
			request := f.request(t, "directory/asset.md", TextContent, RegularFile)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opened := map[*os.File]bool{}
			closed := map[*os.File]int{}
			wrap := func(underlying func(*os.File, string, bool) (*os.File, error)) func(*os.File, string, bool) (*os.File, error) {
				return func(parent *os.File, name string, directory bool) (*os.File, error) {
					file, err := underlying(parent, name, directory)
					if file != nil {
						opened[file] = true
					}
					return file, err
				}
			}
			f.filesystem.open = wrap(f.filesystem.open)
			f.filesystem.metadata = wrap(f.filesystem.metadata)
			closeFile := f.filesystem.close
			f.filesystem.close = func(file *os.File) error {
				closed[file]++
				return closeFile(file)
			}
			switch outcome {
			case "read failure":
				read := f.filesystem.read
				f.filesystem.read = func(file *os.File, b []byte) (int, error) {
					n, err := read(file, b)
					return n, errors.Join(err, io.ErrUnexpectedEOF)
				}
			case "cancelled":
				f.filesystem.hook = func(stage, _ string) error {
					if stage == "before-read" {
						cancel()
					}
					return nil
				}
			case "unstable":
				f.filesystem.hook = func(stage, _ string) error {
					if stage == "before-verify" {
						return errFilesystemUnstable
					}
					return nil
				}
			}
			_, err := f.reader.Read(ctx, request)
			if outcome == "success" && err != nil || outcome != "success" && err == nil {
				t.Fatal("test did not reach intended exit", outcome, err)
			}
			if len(opened) == 0 {
				t.Fatal("no real owned handles were observed")
			}
			for file := range opened {
				if closed[file] != 1 {
					t.Fatal("owned native handle was not closed exactly once", file.Name(), closed[file])
				}
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Fatal("an owned native handle escaped its read lifetime", file.Name(), err)
				}
			}
		})
	}
}

func TestFilesystemLifecycleConcurrentReadsAndIndependentSession(t *testing.T) {
	f := newFilesystemFixture(t)
	f.write(t, "asset.md", []byte("original"))
	request := f.request(t, "asset.md", TextContent, RegularFile)
	const readers = 8
	var group sync.WaitGroup
	results := make(chan ObservationSpec, readers)
	failures := make(chan error, readers)
	for range readers {
		group.Go(func() {
			spec, err := f.reader.Read(context.Background(), request)
			results <- spec
			failures <- err
		})
	}
	group.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	for spec := range results {
		if spec.Content != Available || string(spec.Source.Bytes()) != "original" {
			t.Fatal("concurrent reader returned inconsistent bytes", spec)
		}
	}
	if f.filesystem.bytes != int64(f.source.Len())+readers*int64(len("original")) {
		t.Fatal("concurrent uncached acquisition lost cumulative bytes", f.filesystem.bytes)
	}

	budget := rootPolicyBudget(t, parser.DefaultLimits())
	session, err := f.filesystem.Session(context.Background(), budget, DefaultFilesystemLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Error(err)
		}
	})
	proof, err := session.BindTarget(filepath.Join(f.dir, "source.md"), "report.md", f.source)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := NewOperation(context.Background(), budget, session.Root(), f.operation.Settings(), []Target{proof.Target()})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := session.Reader(operation, proof)
	if err != nil {
		t.Fatal(err)
	}
	view, err := operation.NewView(Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	other := &filesystemFixture{dir: f.dir, filesystem: session, reader: reader, operation: operation, view: view, source: f.source, budget: budget, proof: proof}
	if session.Root() == f.filesystem.Root() || session.bytes != int64(f.source.Len()) {
		t.Fatal("session shared old operation identity/accounting", session.bytes)
	}
	f.write(t, "asset.md", []byte("new call bytes"))
	if err := f.filesystem.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read(context.Background(), request); !errors.Is(err, ErrContract) {
		t.Fatal("new session accepted old operation request", err)
	}
	observation, err := other.observe(t, other.request(t, "asset.md", TextContent, RegularFile))
	if err != nil {
		t.Fatal(err)
	}
	source, ok := observation.Content()
	if !ok || string(source.Bytes()) != "new call bytes" {
		t.Fatal("new call inherited old contents or old close invalidated its handle", observation)
	}
	if _, err := f.reader.Read(context.Background(), request); !errors.Is(err, os.ErrClosed) {
		t.Fatal("closed original root admitted a native acquisition", err)
	}
}
