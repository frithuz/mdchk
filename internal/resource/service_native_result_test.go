//go:build linux || darwin || windows

package resource_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	r "github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
)

func TestFilesystemServiceCachedDecodeIOAndLateStopsKeepAllSourceRelations(t *testing.T) {
	f := newReportFixture(t, context.Background(), "雪 [one](bad.md) [two](bad.md)\r\n")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "a.md"), f.source.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "bad.md"), []byte{0xff, '\n'}, 0600); err != nil {
		t.Fatal(err)
	}
	budget, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	native, err := r.OpenFilesystemRoot(context.Background(), budget, dir, r.DefaultFilesystemLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := native.Close(); err != nil {
			t.Error(err)
		}
	})
	proof, err := native.BindTarget(filepath.Join(dir, "docs", "a.md"), "docs/a.md", f.source)
	if err != nil {
		t.Fatal(err)
	}
	op, err := r.NewOperation(context.Background(), budget, native.Root(), f.operation.Settings(), []r.Target{proof.Target()})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := native.Reader(op, proof)
	if err != nil {
		t.Fatal(err)
	}
	service, err := r.NewFilesystemService(reader, r.DefaultServiceLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})
	secondReader, err := native.Reader(op, proof)
	if err != nil {
		t.Fatal(err)
	}
	secondService, err := r.NewFilesystemService(secondReader, r.DefaultServiceLimits())
	if err != nil || secondService != service {
		t.Fatal("native wrapper reset operation cache", err)
	}
	f.operation = op
	originalSource := f.source
	out := f.result(t, false)
	out.Errors = append(out.Errors, result.Error{ID: "resource-native-error-1", Category: result.IO, Scope: "operation", Code: "independent", Message: "independent prior IO"})
	owner, err := r.NewServiceReport(service, out)
	if err != nil {
		t.Fatal(err)
	}
	type expected struct {
		location result.Range
		snapshot result.Snapshot
	}
	var wants []expected
	for i, phase := range []r.Phase{r.Original, r.Candidate, r.Saved} {
		text := strings.Repeat("雪", i+1) + " [one](bad.md) [two](bad.md)\r\n"
		source := originalSource
		var replacements []r.Replacement
		if phase != r.Original {
			source = parser.NewSource([]byte(text))
			replacements = []r.Replacement{{TargetID: "docs/a.md", Source: source}}
		}
		view, err := op.NewView(phase, replacements)
		if err != nil {
			t.Fatal(err)
		}
		f.view, f.source, f.text = view, source, text
		var observations []r.Observation
		var failures []r.ServiceFailure
		for _, last := range []bool{false, true} {
			q := filesystemReportNamedRequest(t, f, "bad.md", last)
			if _, err := service.Read(context.Background(), q); err == nil {
				t.Fatal("actual invalid resource did not report IO")
			}
			ctx, cancel := context.WithCancel(context.Background())
			caller := &inventoryCancelContext{Context: ctx, cancel: cancel, at: 2}
			spec, err := secondService.Read(caller, q)
			cancel()
			if !errors.Is(err, context.Canceled) || spec.Existence != r.File || spec.Content != r.Failed || spec.Problem.Kind != r.DecodeFailure {
				t.Fatal("cached IO/actual stop/fact lost", spec, err)
			}
			o, recordErr := op.Record(q, spec)
			if recordErr != nil {
				t.Fatal(recordErr)
			}
			observations = append(observations, o)
			failures = append(failures, r.ServiceFailure{Request: q, Err: err})
			offset := strings.Index(text, "bad.md")
			if last {
				offset = strings.LastIndex(text, "bad.md")
			}
			column := utf8.RuneCountInString(text[:offset]) + 1
			hash := sha256.Sum256(source.Bytes())
			wants = append(wants, expected{location: result.Range{Start: parser.Position{Line: 1, Column: column}, End: parser.Position{Line: 1, Column: column + len("bad.md")}}, snapshot: result.Snapshot{View: string(phase), SHA256: fmt.Sprintf("%x", hash), SizeBytes: len(text)}})
		}
		if err := owner.Append(view, observations, failures); err != nil {
			t.Fatal(err)
		}
	}
	out, err = owner.Finish()
	if err != nil {
		t.Fatal(err)
	}
	assert := func() {
		t.Helper()
		if len(out.Errors) != 2 || len(out.Interruptions) != 1 || len(out.Coverage) != 6 {
			t.Fatal("cached causes were lost or multiplied across views", out)
		}
		causeKinds := map[string]string{}
		for _, e := range out.Errors {
			causeKinds[e.ID] = e.Code
		}
		for _, stop := range out.Interruptions {
			causeKinds[stop.ID] = stop.Reason
		}
		for _, want := range wants {
			found := false
			for _, coverage := range out.Coverage {
				if coverage.Path == "docs/a.md" && coverage.Rule == "MD051" && coverage.Range != nil && *coverage.Range == want.location && coverage.Snapshot != nil && *coverage.Snapshot == want.snapshot {
					if len(coverage.CauseIDs) != 2 {
						t.Fatal(coverage)
					}
					seen := map[string]bool{}
					for _, id := range coverage.CauseIDs {
						seen[causeKinds[id]] = true
					}
					found = seen["decode-failure"] && seen["cancelled"] && !seen["independent"]
				}
			}
			if !found {
				t.Fatal("IO/stop linked to wrong source/resource/view", want, out.Coverage)
			}
		}
	}
	assert()
	engine.FinalizeLintResult(&out)
	assert()
	if out.Status != "error" || out.Complete {
		t.Fatal("IO precedence lost", out)
	}
	assertResourceTargetsUnchanged(t, out)
	validateResourceResult(t, out)
	// Fresh logical reports in the same caller variable own independent indexes.
	owner, err = r.NewServiceReport(service, f.result(t, false))
	if err != nil {
		t.Fatal(err)
	}
	q := filesystemReportNamedRequest(t, f, "bad.md", false)
	spec, err := service.Read(context.Background(), q)
	if err == nil {
		t.Fatal("cached IO vanished")
	}
	o, recordErr := op.Record(q, spec)
	if recordErr != nil {
		t.Fatal(recordErr)
	}
	if err := owner.Append(f.view, []r.Observation{o}, []r.ServiceFailure{{Request: q, Err: err}}); err != nil {
		t.Fatal(err)
	}
	out, err = owner.Finish()
	if err != nil || len(out.Errors) != 1 || len(out.Interruptions) != 0 {
		t.Fatal("prior result owner contaminated fresh report", out, err)
	}
}
