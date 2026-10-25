package resource

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func TestServiceReturnedErrorMutationCannotChangeCachedCause(t *testing.T) {
	f := newServiceFixture(t, context.Background(), `{"entries":[]}`, DefaultServiceLimits())
	syscall := &os.SyscallError{Syscall: "open", Err: os.ErrPermission}
	path := &os.PathError{Op: "read", Path: "read.md", Err: syscall}
	link := &os.LinkError{Op: "rename", Old: "before.md", New: "after.md", Err: path}
	fault := fmt.Errorf("outer: %w", errors.Join(link, path, syscall))
	calls := 0
	f.service.read = func(_ context.Context, q Request, p filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
		calls++
		if q.Key().String() == "bad.md" {
			return ObservationSpec{Existence: File, Content: Failed, Anchors: NotRequested, Problem: Problem{Kind: DecodeFailure, Message: "resource content is not UTF-8"}}, p, nil
		}
		return filesystemFailure(File, fault), p, &FilesystemReadError{IO: fault}
	}
	q := serviceRequest(t, f, "read.md", TextContent)
	_, first := f.service.Read(context.Background(), q)
	var exposedPath *os.PathError
	var exposedLink *os.LinkError
	var exposedSyscall *os.SyscallError
	if !errors.Is(first, os.ErrPermission) || !errors.As(first, &exposedPath) || !errors.As(first, &exposedLink) || !errors.As(first, &exposedSyscall) {
		t.Fatal("supported original inspection lost", first)
	}
	originalMessage := first.Error()
	_, decode := f.service.Read(context.Background(), serviceRequest(t, f, "bad.md", TextContent))
	if decode == nil {
		t.Fatal("independent decode failure missing")
	}
	// Both values are ordinary Service.Read errors. The consumer needs no private
	// type or unsafe access to mutate all standard returned metadata and causes.
	exposedPath.Op, exposedPath.Path, exposedPath.Err = "changed", "changed.md", decode
	exposedLink.Op, exposedLink.Old, exposedLink.New, exposedLink.Err = "changed", "changed-old", "changed-new", decode
	exposedSyscall.Syscall, exposedSyscall.Err = "changed", decode
	// The retained graph is also detached from the producer's original wrappers.
	path.Op, path.Path, path.Err = "producer changed", "producer.md", decode
	link.Op, link.Old, link.New, link.Err = "producer changed", "old changed", "new changed", decode
	syscall.Syscall, syscall.Err = "producer changed", decode
	candidateSource := parser.NewSource([]byte("# candidate source\r\n"))
	savedSource := parser.NewSource([]byte("# saved source\n"))
	candidate, viewErr := f.op.NewView(Candidate, []Replacement{{TargetID: "source.md", Source: candidateSource}})
	if viewErr != nil {
		t.Fatal(viewErr)
	}
	saved, viewErr := f.op.NewView(Saved, []Replacement{{TargetID: "source.md", Source: savedSource}})
	if viewErr != nil {
		t.Fatal(viewErr)
	}
	for _, row := range []struct {
		view   View
		source parser.Source
		phase  string
	}{{f.view, f.source, "original"}, {candidate, candidateSource, "candidate"}, {saved, savedSource, "saved"}} {
		q := serviceRequest(t, f, "read.md", TextContent, row.view)
		spec, later := f.service.Read(context.Background(), q)
		if !errors.Is(later, os.ErrPermission) || later.Error() != originalMessage || spec.Problem.Kind != ReadFailure || strings.Contains(spec.Problem.Message, "changed") {
			t.Fatal("caller rewrote retained provenance", spec, later)
		}
		var p *os.PathError
		var l *os.LinkError
		var s *os.SyscallError
		if !errors.As(later, &p) || p.Op != "read" || p.Path != "read.md" || !errors.As(later, &l) || l.Old != "before.md" || l.New != "after.md" || !errors.As(later, &s) || s.Syscall != "open" {
			t.Fatal("later inspection did not restore detached metadata", later)
		}
		o := serviceRecord(t, f, q, spec)
		owner, err := NewServiceReport(f.service, result.NewLint("project"))
		if err != nil {
			t.Fatal(err)
		}
		// Report classification stays canonical even if THIS caller changes its own
		// returned graph immediately before projection.
		p.Err = decode
		if err := owner.Append(row.view, []Observation{o}, []ServiceFailure{{Request: q, Err: later}}); err != nil {
			t.Fatal(err)
		}
		out, err := owner.Finish()
		if err != nil || len(out.Errors) != 1 || out.Errors[0].Code != "read-failure" || out.Errors[0].Path != "read.md" || len(out.Coverage) != 1 || len(out.Coverage[0].CauseIDs) != 1 || out.Coverage[0].CauseIDs[0] != out.Errors[0].ID {
			t.Fatal("invented another resource's decode cause", out, err)
		}
		hash := sha256.Sum256(row.source.Bytes())
		wantSnapshot := result.Snapshot{View: row.phase, SHA256: fmt.Sprintf("%x", hash), SizeBytes: row.source.Len()}
		wantRange := result.Range{Start: parser.Position{Line: 1, Column: 1}, End: parser.Position{Line: 1, Column: 2}}
		assertSource := func() {
			t.Helper()
			coverage := out.Coverage[0]
			if coverage.Path != "source.md" || coverage.Rule != "MD057" || coverage.Snapshot == nil || *coverage.Snapshot != wantSnapshot || coverage.Range == nil || *coverage.Range != wantRange {
				t.Fatal("mutated cause borrowed another source/view", coverage, wantSnapshot)
			}
		}
		assertSource()
		engine.FinalizeLintResult(&out)
		assertSource()
		if out.Errors[0].Code != "read-failure" || out.Coverage[0].CauseIDs[0] != out.Errors[0].ID {
			t.Fatal("finalization changed cause relationship", out)
		}
	}
	if calls != 2 {
		t.Fatal("mutation caused reacquisition", calls)
	}
}

type customServiceError struct {
	message string
	child   error
}

func (e *customServiceError) Error() string { return e.message }
func (e *customServiceError) Unwrap() error { return e.child }

func TestServiceErrorSnapshotKnownGraphsAreBoundedAndDetached(t *testing.T) {
	for _, row := range []struct {
		name       string
		make       func() error
		permission bool
	}{
		{"cycle", func() error { p := &os.PathError{Op: "read", Path: "cycle.md"}; p.Err = errors.Join(p); return p }, false},
		{"shared", func() error {
			var e error = &os.PathError{Op: "read", Path: "shared.md", Err: os.ErrPermission}
			for range 28 {
				e = errors.Join(e, e)
			}
			return e
		}, true},
		{"depth", func() error {
			var e error = os.ErrPermission
			for range 100 {
				e = &os.PathError{Op: "read", Path: "deep.md", Err: e}
			}
			return e
		}, false},
		{"fanout", func() error {
			values := make([]error, 4096)
			for i := range values {
				values[i] = os.ErrPermission
			}
			return errors.Join(values...)
		}, true},
	} {
		t.Run(row.name, func(t *testing.T) {
			original := row.make()
			nodes, work, copy := snapshotServiceError(original)
			if nodes > serviceErrorNodes || work > serviceErrorBytes+serviceErrorNodes+serviceErrorEdges+128 {
				t.Fatal("unbounded metadata traversal", nodes, work)
			}
			if errors.Is(copy, os.ErrPermission) != row.permission {
				t.Fatal("supported sentinel provenance changed", copy)
			}
			var absent *os.SyscallError
			if errors.As(copy, &absent) {
				t.Fatal("invented wrapper")
			}
			if copy.Error() == "" {
				t.Fatal("lost failure evidence")
			}
		})
	}
	opaque := &customServiceError{message: "custom original", child: os.ErrPermission}
	_, _, copy := snapshotServiceError(opaque)
	opaque.message = "changed"
	opaque.child = opaque
	if copy.Error() != "custom original" || !errors.Is(copy, opaque) || errors.Is(copy, os.ErrPermission) {
		t.Fatal("unsupported custom graph was not opaque", copy)
	}
	var exposed *customServiceError
	if errors.As(copy, &exposed) {
		t.Fatal("custom mutable object was exposed")
	}
}

func TestServiceErrorSnapshotLimitsAndSharedCopyWork(t *testing.T) {
	limit := &parser.LimitExceeded{Name: "work-units", Observed: 10, Allowed: 9}
	original := fmt.Errorf("outer: %w", errors.Join(&os.PathError{Op: "read", Path: "a.md", Err: os.ErrPermission}, limit, context.Canceled))
	_, _, copy := snapshotServiceError(original)
	var exposed *parser.LimitExceeded
	if !errors.As(copy, &exposed) || !errors.Is(copy, context.Canceled) || !errors.Is(copy, os.ErrPermission) {
		t.Fatal(copy)
	}
	exposed.Name = "changed"
	exposed.Allowed = 100
	_, _, again := snapshotServiceError(original)
	if !errors.As(again, &exposed) || exposed.Name != "work-units" || exposed.Allowed != 9 {
		t.Fatal("mutable limit alias retained", again)
	}
	for _, count := range []int{4, 16, 40} {
		entries := make([]error, count)
		for i := range entries {
			entries[i] = &os.PathError{Op: "read", Path: fmt.Sprintf("%d.md", i), Err: os.ErrPermission}
		}
		nodes, work, _ := snapshotServiceError(errors.Join(entries...))
		if nodes > int64(3*count+2) || work > int64(256*count+32) {
			t.Fatal("copy work grew beyond graph metadata", count, nodes, work)
		}
	}
}

// Optional inspection metadata must never replace the independently acquired
// IO/control records, including when a consumer makes a standard carrier invalid.
func TestServiceReturnedMetadataCannotEraseCanonicalStops(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*os.PathError)
	}{
		{"nil-path-child", func(p *os.PathError) { p.Err = nil }},
		{"nil-link-child", func(p *os.PathError) { p.Err = &os.LinkError{Op: "rename", Old: "a", New: "b"} }},
		{"nil-syscall-child", func(p *os.PathError) { p.Err = &os.SyscallError{Syscall: "open"} }},
		{"typed-nil-path", func(p *os.PathError) { var e *os.PathError; p.Err = errors.Join(e) }},
		{"typed-nil-link", func(p *os.PathError) { var e *os.LinkError; p.Err = errors.Join(e) }},
		{"typed-nil-syscall", func(p *os.PathError) { var e *os.SyscallError; p.Err = errors.Join(e) }},
		{"wide", func(p *os.PathError) {
			entries := make([]error, 256)
			for i := range entries {
				entries[i] = &os.PathError{Op: "read", Path: "optional.md", Err: os.ErrPermission}
			}
			p.Err = errors.Join(entries...)
		}},
		{"deep", func(p *os.PathError) {
			var e error = os.ErrPermission
			for range 100 {
				e = &os.PathError{Op: "read", Path: "optional.md", Err: e}
			}
			p.Err = e
		}},
		{"shared", func(p *os.PathError) {
			var e error = os.ErrPermission
			for range 28 {
				e = errors.Join(e, e)
			}
			p.Err = e
		}},
		{"cycle", func(p *os.PathError) { p.Err = errors.Join(p) }},
		{"text", func(p *os.PathError) { p.Op = strings.Repeat("x", serviceErrorBytes*2); p.Path = p.Op }},
	}
	for _, stop := range []string{"none", "cancel", "deadline", "limit"} {
		for _, mutation := range mutations {
			t.Run(stop+"/"+mutation.name, func(t *testing.T) {
				f := newServiceFixture(t, context.Background(), `{"entries":[]}`, DefaultServiceLimits())
				calls := 0
				f.service.read = func(_ context.Context, _ Request, p filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
					calls++
					fault := &os.PathError{Op: "read", Path: "read.md", Err: os.ErrPermission}
					return filesystemFailure(File, fault), p, &FilesystemReadError{IO: fault}
				}
				sources := []parser.Source{f.source, parser.NewSource([]byte("# candidate\r\n")), parser.NewSource([]byte("# saved\n"))}
				candidate, err := f.op.NewView(Candidate, []Replacement{{TargetID: "source.md", Source: sources[1]}})
				if err != nil {
					t.Fatal(err)
				}
				saved, err := f.op.NewView(Saved, []Replacement{{TargetID: "source.md", Source: sources[2]}})
				if err != nil {
					t.Fatal(err)
				}
				views := []View{f.view, candidate, saved}
				phases := []string{"original", "candidate", "saved"}
				requests := make([]Request, 3)
				for i, v := range views {
					requests[i] = serviceRequest(t, f, "read.md", TextContent, v)
				}
				_, first := f.service.Read(context.Background(), requests[0])
				if !errors.Is(first, os.ErrPermission) {
					t.Fatal(first)
				}
				message := first.Error()
				caller := context.Background()
				var expectedLimit parser.LimitExceeded
				switch stop {
				case "cancel":
					ctx, cancel := context.WithCancel(caller)
					cancel()
					caller = ctx
				case "deadline":
					ctx, cancel := context.WithDeadline(caller, time.Unix(0, 0))
					defer cancel()
					caller = ctx
				case "limit":
					exceeded := f.op.budget.ChargeWork(f.op.budget.Limits().WorkUnits + 1)
					var limit *parser.LimitExceeded
					if !errors.As(exceeded, &limit) {
						t.Fatal("real budget did not stop", exceeded)
					}
					expectedLimit = *limit
				}
				for i, q := range requests {
					spec, returned := f.service.Read(caller, q)
					if spec.Existence != File || returned == nil {
						t.Fatal("known File/IO lost", spec, returned)
					}
					switch stop {
					case "cancel":
						if !errors.Is(returned, context.Canceled) {
							t.Fatal(returned)
						}
					case "deadline":
						if !errors.Is(returned, context.DeadlineExceeded) {
							t.Fatal(returned)
						}
					case "limit":
						var limit *parser.LimitExceeded
						if !errors.As(returned, &limit) || *limit != expectedLimit {
							t.Fatal("actual limit lost", returned)
						}
						limit.Name = "mutated"
						limit.Allowed = 999
						limit.Observed = 999
					}
					var path *os.PathError
					if !errors.As(returned, &path) {
						t.Fatal(returned)
					}
					mutation.mutate(path)
					nodes, work, detached := snapshotServiceError(returned)
					if nodes > serviceErrorNodes || work > serviceErrorBytes+serviceErrorNodes+serviceErrorEdges+128 || detached == nil {
						t.Fatal("optional copy unbounded", nodes, work)
					}
					o := serviceRecord(t, f, q, spec)
					owner, err := NewServiceReport(f.service, result.NewLint("project"))
					if err != nil {
						t.Fatal(err)
					}
					// A genuinely foreign batch must still reject without any output mutation.
					foreign := newServiceFixture(t, context.Background(), `{"entries":[]}`, DefaultServiceLimits())
					foreignQ := serviceRequest(t, foreign, "read.md", TextContent)
					if err := owner.Append(views[i], []Observation{o}, []ServiceFailure{{Request: q, Err: returned}, {Request: foreignQ, Err: returned}}); !errors.Is(err, ErrContract) {
						t.Fatal("foreign batch accepted", err)
					}
					if len(owner.state.out.Errors) != 0 || len(owner.state.out.Interruptions) != 0 || len(owner.state.out.Coverage) != 0 {
						t.Fatal("invalid batch partially mutated output")
					}
					if err := owner.Append(views[i], []Observation{o}, []ServiceFailure{{Request: q, Err: returned}}); err != nil {
						t.Fatal("optional metadata erased canonical evidence", err)
					}
					out, err := owner.Finish()
					if err != nil {
						t.Fatal(err)
					}
					check := func() {
						t.Helper()
						stops := 0
						if stop != "none" {
							stops = 1
						}
						if len(out.Errors) != 1 || out.Errors[0].Code != "read-failure" || out.Errors[0].Path != "read.md" || len(out.Interruptions) != stops || len(out.Coverage) != 1 {
							t.Fatal("canonical evidence changed", out)
						}
						coverage := out.Coverage[0]
						hash := sha256.Sum256(sources[i].Bytes())
						snapshot := result.Snapshot{View: phases[i], SHA256: fmt.Sprintf("%x", hash), SizeBytes: sources[i].Len()}
						span := result.Range{Start: parser.Position{Line: 1, Column: 1}, End: parser.Position{Line: 1, Column: 2}}
						if coverage.Path != "source.md" || coverage.Rule != "MD057" || coverage.Snapshot == nil || *coverage.Snapshot != snapshot || coverage.Range == nil || *coverage.Range != span || len(coverage.CauseIDs) != 1+stops || coverage.CauseIDs[0] != out.Errors[0].ID {
							t.Fatal("source cause relation changed", coverage)
						}
						if stops != 0 {
							interruption := out.Interruptions[0]
							reason := map[string]string{"cancel": "cancelled", "deadline": "deadline-exceeded", "limit": "limit-exceeded"}[stop]
							if interruption.Reason != reason || coverage.CauseIDs[1] != interruption.ID {
								t.Fatal("real stop lost", out)
							}
							if stop == "limit" && (interruption.Limit == nil || interruption.Limit.Name != expectedLimit.Name || interruption.Limit.Observed != expectedLimit.Observed || interruption.Limit.Allowed != expectedLimit.Allowed) {
								t.Fatal("canonical limit was mutable", interruption)
							}
						}
					}
					check()
					engine.FinalizeLintResult(&out)
					check()
					if out.Status != "error" || out.Complete {
						t.Fatal("IO precedence lost", out)
					}
					_, fresh := f.service.Read(context.Background(), q)
					if !errors.Is(fresh, os.ErrPermission) || !strings.HasPrefix(fresh.Error(), message) {
						t.Fatal("cached IO changed", fresh)
					}
				}
				if calls != 1 {
					t.Fatal("metadata caused reacquisition", calls)
				}
			})
		}
	}
}

func TestServiceErrorSnapshotNilKnownCarriers(t *testing.T) {
	var path *os.PathError
	var link *os.LinkError
	var syscall *os.SyscallError
	var limit *parser.LimitExceeded
	for i, err := range []error{path, link, syscall, limit, &os.PathError{}, &os.LinkError{}, &os.SyscallError{}, errors.Join(&os.PathError{Err: link}, &os.LinkError{Err: syscall}, &os.SyscallError{Err: path})} {
		nodes, work, copy := snapshotServiceError(err)
		if copy == nil || copy.Error() == "" || nodes > serviceErrorNodes || work > serviceErrorBytes+serviceErrorNodes+serviceErrorEdges+128 {
			t.Fatal("nil known carrier unsafe", i, nodes, work)
		}
	}
}

func TestServiceCombinedCanonicalStopsAreAllProjected(t *testing.T) {
	for _, mode := range []struct {
		name                    string
		cancel, deadline, limit bool
	}{
		{"quota-cancel", true, false, true}, {"deadline-cancel", true, true, false}, {"deadline-quota", false, true, true}, {"all-three", true, true, true},
	} {
		for _, fact := range []string{"file", "directory", "missing", "unknown", "io"} {
			t.Run(mode.name+"/"+fact, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					opctx := context.Background()
					if mode.deadline {
						ctx, cancel := context.WithTimeout(opctx, time.Second)
						defer cancel()
						opctx = ctx
					}
					raw := `{"entries":[{"path":"file","kind":"file","content":"# warm\n"},{"path":"directory","kind":"directory"},{"path":"missing","kind":"missing"}]}`
					f := newServiceFixture(t, opctx, raw, DefaultServiceLimits())
					if fact == "io" {
						f.service.read = func(_ context.Context, _ Request, p filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
							fault := &os.PathError{Op: "read", Path: "io", Err: os.ErrPermission}
							return filesystemFailure(File, fault), p, &FilesystemReadError{IO: fault}
						}
					}
					sources := []parser.Source{f.source, parser.NewSource([]byte("# candidate\r\n")), parser.NewSource([]byte("# saved\n"))}
					candidate, err := f.op.NewView(Candidate, []Replacement{{TargetID: "source.md", Source: sources[1]}})
					if err != nil {
						t.Fatal(err)
					}
					saved, err := f.op.NewView(Saved, []Replacement{{TargetID: "source.md", Source: sources[2]}})
					if err != nil {
						t.Fatal(err)
					}
					views := []View{f.view, candidate, saved}
					phases := []string{"original", "candidate", "saved"}
					warm := serviceRequest(t, f, fact, TextContent)
					first, readErr := f.service.Read(context.Background(), warm)
					if fact != "io" && readErr != nil {
						t.Fatal(readErr)
					}
					wantExist := map[string]Existence{"file": File, "directory": Directory, "missing": Missing, "unknown": Unknown, "io": File}[fact]
					if first.Existence != wantExist {
						t.Fatal("warm fact missing", first)
					}
					capability := ExistenceOnly
					if fact == "unknown" || fact == "io" {
						capability = TextContent
					}
					requests := make([]Request, 3)
					for i, v := range views {
						requests[i] = serviceRequest(t, f, fact, capability, v)
					}
					var expectedLimit parser.LimitExceeded
					if mode.limit {
						exceeded := f.op.budget.ChargeWork(f.op.budget.Limits().WorkUnits + 1)
						var limit *parser.LimitExceeded
						if !errors.As(exceeded, &limit) {
							t.Fatal("real quota missing", exceeded)
						}
						expectedLimit = *limit
					}
					if mode.deadline {
						time.Sleep(2 * time.Second)
					}
					<-f.service.done // join the actual stopped producer, without adding a Close cancellation
					caller := context.Background()
					if mode.cancel {
						ctx, cancel := context.WithCancel(caller)
						cancel()
						caller = ctx
					}
					owner, err := NewServiceReport(f.service, result.NewLint("project"))
					if err != nil {
						t.Fatal(err)
					}
					expectedReasons := map[string]bool{}
					if mode.cancel {
						expectedReasons[string(Cancelled)] = true
					}
					if mode.deadline {
						expectedReasons[string(Deadline)] = true
					}
					if mode.limit {
						expectedReasons[string(Limited)] = true
					}
					for i, q := range requests {
						spec, returned := f.service.Read(caller, q)
						if spec.Existence != wantExist || returned == nil {
							t.Fatal("combined stop erased fact", spec, returned)
						}
						if errors.Is(returned, context.Canceled) != mode.cancel || errors.Is(returned, context.DeadlineExceeded) != mode.deadline {
							t.Fatal("actual contexts not retained", returned)
						}
						if mode.limit {
							var limit *parser.LimitExceeded
							if !errors.As(returned, &limit) || *limit != expectedLimit {
								t.Fatal("actual quota not retained", returned)
							}
							limit.Name = "changed"
							limit.Observed = 999
							limit.Allowed = 999
						}
						if fact == "io" {
							var path *os.PathError
							if !errors.As(returned, &path) {
								t.Fatal(returned)
							}
							switch i {
							case 0:
								path.Err = nil
							case 1:
								entries := make([]error, 256)
								for j := range entries {
									entries[j] = &os.PathError{Op: "optional", Err: os.ErrPermission}
								}
								path.Err = errors.Join(entries...)
							case 2:
								path.Err = errors.Join(path)
							}
						}
						nodes, work, _ := snapshotServiceError(returned)
						if nodes > serviceErrorNodes || work > serviceErrorBytes+serviceErrorNodes+serviceErrorEdges+128 {
							t.Fatal("combined metadata copy unbounded", nodes, work)
						}
						o := serviceRecord(t, f, q, spec)
						if i == 0 {
							if err := owner.Append(views[i], []Observation{o}, []ServiceFailure{{Request: q, Err: returned}, {Request: requests[1], Err: returned}}); !errors.Is(err, ErrContract) {
								t.Fatal("foreign-view batch accepted", err)
							}
							if len(owner.state.out.Errors) != 0 || len(owner.state.out.Interruptions) != 0 || len(owner.state.out.Coverage) != 0 {
								t.Fatal("invalid batch partially mutated")
							}
						}
						if err := owner.Append(views[i], []Observation{o}, []ServiceFailure{{Request: q, Err: returned}}); err != nil {
							t.Fatal(err)
						}
					}
					out, err := owner.Finish()
					if err != nil {
						t.Fatal(err)
					}
					check := func() {
						t.Helper()
						if len(out.Interruptions) != len(expectedReasons) {
							t.Fatal("combined canonical stops were collapsed or duplicated", out.Interruptions)
						}
						ids := []string{}
						seen := map[string]bool{}
						for _, stop := range out.Interruptions {
							if !expectedReasons[stop.Reason] || seen[stop.Reason] || stop.Path != fact {
								t.Fatal("wrong control cause", stop)
							}
							seen[stop.Reason] = true
							ids = append(ids, stop.ID)
							if stop.Reason == string(Limited) && (stop.Limit == nil || stop.Limit.Name != expectedLimit.Name || stop.Limit.Observed != expectedLimit.Observed || stop.Limit.Allowed != expectedLimit.Allowed || stop.Limit.Unit != result.LimitUnit(expectedLimit.Name)) {
								t.Fatal("combined exact limit lost", stop)
							}
						}
						errorsCount := 0
						if fact == "io" {
							errorsCount = 1
						}
						if len(out.Errors) != errorsCount {
							t.Fatal("invented IO", out.Errors)
						}
						if fact == "io" {
							if out.Errors[0].Code != "read-failure" || out.Errors[0].Path != fact || out.Errors[0].Message != first.Problem.Message {
								t.Fatal("original IO changed", out.Errors)
							}
							ids = append(ids, out.Errors[0].ID)
						}
						count := 0
						if fact == "io" || fact == "unknown" {
							count = 3
						}
						if len(out.Coverage) != count {
							t.Fatal("satisfied fact gained coverage", out.Coverage)
						}
						seenViews := map[string]bool{}
						for _, c := range out.Coverage {
							if c.Snapshot == nil {
								t.Fatal("missing source snapshot", c)
							}
							i, known := map[string]int{"original": 0, "candidate": 1, "saved": 2}[c.Snapshot.View]
							if !known || seenViews[c.Snapshot.View] {
								t.Fatal("wrong/duplicate source view", c)
							}
							seenViews[c.Snapshot.View] = true
							hash := sha256.Sum256(sources[i].Bytes())
							snapshot := result.Snapshot{View: phases[i], SHA256: fmt.Sprintf("%x", hash), SizeBytes: sources[i].Len()}
							span := result.Range{Start: parser.Position{Line: 1, Column: 1}, End: parser.Position{Line: 1, Column: 2}}
							if c.Path != "source.md" || c.Rule != "MD057" || c.Snapshot == nil || *c.Snapshot != snapshot || c.Range == nil || *c.Range != span || len(c.CauseIDs) != len(ids) {
								t.Fatal("combined source cause binding lost", c, "snapshot", c.Snapshot, snapshot, "range", c.Range, span, "ids", ids)
							}
							for _, id := range ids {
								if !slices.Contains(c.CauseIDs, id) {
									t.Fatal("coverage omitted actual cause", id, c)
								}
							}
						}
					}
					check()
					engine.FinalizeLintResult(&out)
					check()
					wantStatus := "incomplete"
					if fact == "io" {
						wantStatus = "error"
					}
					if out.Status != wantStatus || out.Complete {
						t.Fatal("combined status precedence changed", out)
					}
				})
			})
		}
	}
}
