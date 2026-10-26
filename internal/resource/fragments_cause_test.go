package resource

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func fragmentCauseFixture(t *testing.T, ctx context.Context, key, destination, raw string) serviceFixture {
	t.Helper()
	source := parser.NewSource([]byte("雪 [q](" + destination + ")\r\n\n# Target\n"))
	root, _ := NewRoot(SuppliedRoot)
	budget, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := NewSettings(parser.ParseOptions{}, "index.md", "github", "fragment-causes-v1")
	k, err := NewKey(key)
	if err != nil {
		t.Fatal(err)
	}
	op, err := NewOperation(ctx, budget, root, settings, []Target{{ID: "source.md", Source: source, ResourceKey: k}})
	if err != nil {
		t.Fatal(err)
	}
	input, err := DecodeSupplied(ctx, budget, DefaultInputLimits(), []byte(raw), []SuppliedFile{{Key: k, Source: source}})
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
	view, err := op.NewView(Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	return serviceFixture{service, reader, op, view, source}
}
func fragmentCauseInitial(path string, source parser.Source) result.Result {
	hash := sha256.Sum256(source.Bytes())
	snapshot := result.Snapshot{View: "original", SHA256: fmt.Sprintf("%x", hash), SizeBytes: source.Len()}
	out := result.NewLint("cli")
	out.CheckedFiles = []string{path}
	out.Files = []result.File{{Path: path, Analysis: "partial", WriteStatus: "not-requested", ReturnStatus: "not-requested", InputSnapshot: &snapshot, ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}}}
	return out
}

type fragmentCauseWant struct{ path, kind string }

// Independent path/cardinality/source oracle: equal reason sets cannot hide a
// second cause at a successful parent directory or an unregistered report name.
func assertFragmentCauseReport(t *testing.T, out result.Result, path string, source parser.Source, phase Phase, location result.Range, wants []fragmentCauseWant) {
	t.Helper()
	if len(out.Errors)+len(out.Interruptions) != len(wants) || len(out.Coverage) != 1 || len(out.Diagnostics) != 0 || len(out.Changes) != 0 {
		t.Fatal("wrong cause cardinality", out)
	}
	ids := map[string]bool{}
	seen := map[fragmentCauseWant]int{}
	for _, e := range out.Errors {
		if e.ID == "" || ids[e.ID] {
			t.Fatal("nonunique error ID", e)
		}
		ids[e.ID] = true
		seen[fragmentCauseWant{e.Path, e.Code}]++
	}
	for _, stop := range out.Interruptions {
		if stop.ID == "" || ids[stop.ID] {
			t.Fatal("nonunique stop ID", stop)
		}
		ids[stop.ID] = true
		seen[fragmentCauseWant{stop.Path, stop.Reason}]++
		if stop.Reason == "limit-exceeded" && (stop.Limit == nil || stop.Limit.Name != "work-units" || stop.Limit.Observed <= stop.Limit.Allowed) {
			t.Fatal("exact quota lost", stop)
		}
	}
	for _, want := range wants {
		if seen[want] != 1 {
			t.Fatal("cause resource/kind not unique", want, seen)
		}
	}
	coverage := out.Coverage[0]
	hash := sha256.Sum256(source.Bytes())
	snapshot := result.Snapshot{View: string(phase), SHA256: fmt.Sprintf("%x", hash), SizeBytes: source.Len()}
	if coverage.Path != path || coverage.Rule != "MD051" || coverage.Range == nil || *coverage.Range != location || coverage.Snapshot == nil || *coverage.Snapshot != snapshot || len(coverage.CauseIDs) != len(wants) {
		t.Fatal("source range/SHA/view/cause relation", coverage, location, snapshot)
	}
	linked := map[string]bool{}
	for _, id := range coverage.CauseIDs {
		if !ids[id] || linked[id] {
			t.Fatal("wrong/duplicate cause link", id, coverage, ids)
		}
		linked[id] = true
	}
}
func validateFragmentCauseSchema(t *testing.T, out result.Result) {
	t.Helper()
	python := os.Getenv("MDCHK_RESULT_VALIDATOR_PYTHON")
	if python == "" {
		return
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("missing test location")
	}
	validator := filepath.Join(filepath.Dir(file), "../../docs/contracts/results/validate.py")
	command := exec.Command(python, "-B", "-c", `import json,runpy,sys;v=runpy.run_path(sys.argv[1]);x=json.load(sys.stdin);v['validator'].validate(x);v['semantic'](x)`, validator) //nolint:gosec // Explicit test interpreter and fixed checked-in result schema.
	command.Stdin = bytes.NewReader(raw)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("cause result schema: %v\n%s\n%s", err, output, raw)
	}
}

// CheckFragmentFinalIndexCauseIdentityAcrossViews retains private resource controls while the outer test supplies the real engine finalizer.
func CheckFragmentFinalIndexCauseIdentityAcrossViews(t *testing.T, finalize func(*result.Result)) {
	paths := []struct{ name, key, destination, final, raw string }{
		{"ordinary-directory", "source.md", "folder/#target", "folder/index.md", `{"entries":[{"path":"folder/index.md","kind":"file","content":"# Target\n"}]}`},
		{"root-index", "docs/source.md", "../#target", "index.md", `{"entries":[{"path":"index.md","kind":"file","content":"# Target\n"}]}`},
		{"selected-self-index", "docs/index.md", "./#target", "docs/index.md", `{"entries":[]}`},
	}
	for _, path := range paths {
		for _, phase := range []Phase{Original, Candidate, Saved} {
			for _, fault := range []string{"internal", "index-quota", "ready-combined"} {
				t.Run(path.name+"/"+string(phase)+"/"+fault, func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						opctx := context.Background()
						var end context.CancelFunc
						if fault == "ready-combined" {
							opctx, end = context.WithTimeout(opctx, time.Second)
							defer end()
						}
						f := fragmentCauseFixture(t, opctx, path.key, path.destination, path.raw)
						source := f.source
						view := f.view
						if phase != Original {
							source = parser.NewSource([]byte("雪雪 [q](" + path.destination + ")\r\n\n# Target\n"))
							var err error
							view, err = f.op.NewView(phase, []Replacement{{TargetID: "source.md", Source: source}})
							if err != nil {
								t.Fatal(err)
							}
						}
						r := fragmentTestResolver(t, f, view)
						spanStart := len("雪 [q](")
						column := 7
						if phase != Original {
							spanStart = len("雪雪 [q](")
							column = 8
						}
						span, _ := source.Span(spanStart, spanStart+len(path.destination))
						link, _ := NewLink("source.md", "MD051", path.destination, source, span)
						if fault == "ready-combined" {
							warm, err := r.Prepare(link)
							if err != nil {
								t.Fatal(err)
							}
							got, err := r.Resolve(context.Background(), warm, true)
							if err != nil || got.Outcome() != FragmentFound {
								t.Fatal(got.Outcome(), err)
							}
						}
						q, err := r.Prepare(link)
						if err != nil {
							t.Fatal(err)
						}
						build := f.service.buildAnchors
						if fault != "ready-combined" {
							f.service.buildAnchors = func(ctx context.Context, op *Operation, key ParseCacheKey, p parser.ParseResult) (AnchorResult, error) {
								if fault == "internal" {
									failure := &parser.Failure{Code: parser.InternalFailure, Message: "index producer failure"}
									return AnchorResult{&anchorResultState{operation: op, key: key, availability: Failed, problem: Problem{Kind: InternalFailure, Message: failure.Error()}}}, anchorInternalError(failure)
								}
								if err := op.budget.ChargeWork(op.budget.Limits().WorkUnits); err == nil {
									t.Fatal("quota not exhausted")
								}
								return build(ctx, op, key, p)
							}
						}
						ctx, cancel := context.WithCancel(context.Background())
						defer cancel()
						if fault == "ready-combined" {
							if err := f.op.budget.ChargeWork(f.op.budget.Limits().WorkUnits); err == nil {
								t.Fatal("quota not exhausted")
							}
							time.Sleep(time.Second)
							synctest.Wait()
							cancel()
						}
						got, err := r.Resolve(ctx, q, true)
						if err == nil || got.Outcome() != FragmentContextUnavailable {
							t.Fatal(got.Outcome(), err)
						}
						if _, _, ok := got.Anchors().Content(); !ok {
							t.Fatal("anchor failure erased admitted content")
						}
						if fault == "ready-combined" && got.Anchors().Availability() != Available {
							t.Fatal("ready index lost", got.Anchors().Availability())
						}
						wants := []fragmentCauseWant{{path.final, "internal-failure"}}
						if fault == "index-quota" {
							wants = []fragmentCauseWant{{path.final, "limit-exceeded"}}
						}
						if fault == "ready-combined" {
							wants = []fragmentCauseWant{{path.final, "cancelled"}, {path.final, "deadline-exceeded"}, {path.final, "limit-exceeded"}}
						}
						// A ready directory stage also encounters the real combined operation stop.
						// Preserve it independently; a successful cold directory has no such cause.
						if fault == "ready-combined" && path.name != "root-index" {
							directory := strings.TrimSuffix(path.final, "/index.md")
							wants = append(wants, fragmentCauseWant{directory, "cancelled"}, fragmentCauseWant{directory, "deadline-exceeded"}, fragmentCauseWant{directory, "limit-exceeded"})
						}
						if fault == "ready-combined" && path.name == "root-index" { // RootDirectory has no resource key; its real source-domain controls retain the source identity.
							wants = append(wants, fragmentCauseWant{"source.md", "cancelled"}, fragmentCauseWant{"source.md", "deadline-exceeded"})
						}
						owner, err := NewFragmentReport(r, fragmentCauseInitial("source.md", f.source))
						if err != nil {
							t.Fatal(err)
						}
						if err := owner.Append(view, []FragmentResolution{got}); err != nil {
							t.Fatal(err)
						}
						out, err := owner.Finish()
						if err != nil {
							t.Fatal(err)
						}
						location := result.Range{Start: parser.Position{Line: 1, Column: column}, End: parser.Position{Line: 1, Column: column + len(path.destination)}}
						assertFragmentCauseReport(t, out, "source.md", source, phase, location, wants)
						finalize(&out)
						assertFragmentCauseReport(t, out, "source.md", source, phase, location, wants)
						validateFragmentCauseSchema(t, out)
					})
				})
			}
		}
	}
}

// CheckFragmentEarlierDirectoryCauseRemainsIndependent retains private resource controls while the outer test supplies the real engine finalizer.
func CheckFragmentEarlierDirectoryCauseRemainsIndependent(t *testing.T, finalize func(*result.Result)) {
	for _, phase := range []Phase{Original, Candidate, Saved} {
		for _, indexKind := range []ProblemKind{ReadFailure, InternalFailure} {
			t.Run(string(phase)+"/"+string(indexKind), func(t *testing.T) {
				f := fragmentCauseFixture(t, context.Background(), "source.md", "folder/#target", `{"entries":[{"path":"folder/index.md","kind":"file","content":"# Target\n"}]}`)
				target := f.source
				view := f.view
				if phase != Original {
					target = parser.NewSource(f.source.Bytes())
					var err error
					view, err = f.op.NewView(phase, []Replacement{{TargetID: "source.md", Source: target}})
					if err != nil {
						t.Fatal(err)
					}
				}
				r := fragmentTestResolver(t, f, view)
				read := f.service.read
				f.service.read = func(ctx context.Context, q Request, receipt filesystemReceipt) (ObservationSpec, filesystemReceipt, error) {
					if indexKind == ReadFailure && q.Key().String() == "folder/index.md" && q.Capability() != ExistenceOnly {
						return ObservationSpec{Existence: File, Content: Failed, Anchors: Unavailable, Problem: Problem{Kind: ReadFailure, Message: "index data IO"}}, filesystemReceipt{}, &FilesystemReadError{IO: errors.New("index data IO")}
					}
					spec, receipt, err := read(ctx, q, receipt)
					if q.Key().String() == "folder" {
						return spec, receipt, errors.Join(err, &FilesystemReadError{IO: errors.New("directory-stage IO")})
					}
					return spec, receipt, err
				}
				if indexKind == InternalFailure {
					f.service.buildAnchors = func(_ context.Context, op *Operation, key ParseCacheKey, _ parser.ParseResult) (AnchorResult, error) {
						failure := &parser.Failure{Code: parser.InternalFailure, Message: "index failure"}
						return AnchorResult{&anchorResultState{operation: op, key: key, availability: Failed, problem: Problem{Kind: InternalFailure, Message: failure.Error()}}}, anchorInternalError(failure)
					}
				}
				start := len("雪 [q](")
				span, _ := target.Span(start, start+len("folder/#target"))
				link, _ := NewLink("source.md", "MD051", "folder/#target", target, span)
				q, err := r.Prepare(link)
				if err != nil {
					t.Fatal(err)
				}
				got, err := r.Resolve(context.Background(), q, true)
				if err == nil || got.Outcome() != FragmentContextUnavailable {
					t.Fatal(got.Outcome(), err)
				}
				owner, _ := NewFragmentReport(r, fragmentCauseInitial("source.md", f.source))
				if err := owner.Append(view, []FragmentResolution{got}); err != nil {
					t.Fatal(err)
				}
				out, _ := owner.Finish()
				location := result.Range{Start: parser.Position{Line: 1, Column: 7}, End: parser.Position{Line: 1, Column: 21}}
				wants := []fragmentCauseWant{{"folder", "read-failure"}, {"folder/index.md", string(indexKind)}}
				assertFragmentCauseReport(t, out, "source.md", target, phase, location, wants)
				finalize(&out)
				assertFragmentCauseReport(t, out, "source.md", target, phase, location, wants)
				validateFragmentCauseSchema(t, out)
			})
		}
	}
}

// CheckFragmentDirectoryLookupQuotaKeepsFinalCauseIdentity retains private resource controls while the outer test supplies the real engine finalizer.
func CheckFragmentDirectoryLookupQuotaKeepsFinalCauseIdentity(t *testing.T, finalize func(*result.Result)) {
	for _, phase := range []Phase{Original, Candidate, Saved} {
		t.Run(string(phase), func(t *testing.T) {
			f := fragmentCauseFixture(t, context.Background(), "source.md", "folder/#target", `{"entries":[{"path":"folder/index.md","kind":"file","content":"# Target\n"}]}`)
			source := f.source
			view := f.view
			if phase != Original {
				source = parser.NewSource(f.source.Bytes())
				var err error
				view, err = f.op.NewView(phase, []Replacement{{TargetID: "source.md", Source: source}})
				if err != nil {
					t.Fatal(err)
				}
			}
			r := fragmentTestResolver(t, f, view)
			span, _ := source.Span(len("雪 [q]("), len("雪 [q](")+len("folder/#target"))
			link, _ := NewLink("source.md", "MD051", "folder/#target", source, span)
			warm, err := r.Prepare(link)
			if err != nil {
				t.Fatal(err)
			}
			got, err := r.Resolve(context.Background(), warm, true)
			if err != nil || got.Outcome() != FragmentFound {
				t.Fatal(got.Outcome(), err)
			}
			id := strings.Repeat("x", 100000)
			link, _ = NewLink("source.md", "MD051", "folder/#"+id, source, span)
			q, err := r.Prepare(link)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.op.budget.ChargeWork(f.op.budget.Limits().WorkUnits - 17*int64(len(id)) - 10000); err != nil {
				t.Fatal("quota already exhausted before Lookup", err)
			}
			got, err = r.Resolve(context.Background(), q, true)
			var limit *parser.LimitExceeded
			if !errors.As(err, &limit) || limit.Observed-limit.Allowed < int64(len(id))/2 || got.Outcome() != FragmentContextUnavailable || got.Anchors().Availability() != Available {
				t.Fatal("actual Lookup charge not reached", got.Outcome(), err)
			}
			if _, _, ok := got.Anchors().Content(); !ok {
				t.Fatal("Lookup erased content")
			}
			owner, _ := NewFragmentReport(r, fragmentCauseInitial("source.md", f.source))
			if err := owner.Append(view, []FragmentResolution{got}); err != nil {
				t.Fatal(err)
			}
			out, _ := owner.Finish()
			location := result.Range{Start: parser.Position{Line: 1, Column: 7}, End: parser.Position{Line: 1, Column: 21}}
			wants := []fragmentCauseWant{{"folder/index.md", "limit-exceeded"}}
			assertFragmentCauseReport(t, out, "source.md", source, phase, location, wants)
			finalize(&out)
			assertFragmentCauseReport(t, out, "source.md", source, phase, location, wants)
			validateFragmentCauseSchema(t, out)
		})
	}
}
