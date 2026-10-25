package resource_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	r "github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
)

func TestServiceCachedFactsLateStopsAndResultSchema(t *testing.T) {
	for _, row := range []struct {
		name      string
		exist     r.Existence
		content   r.Availability
		treatment r.Treatment
	}{
		{"directory", r.Directory, r.Inapplicable, r.MissingTarget},
		{"missing.md", r.Missing, r.Inapplicable, r.MissingTarget},
		{"file.md", r.File, r.Available, r.Complete},
		{"unknown.md", r.Unknown, r.Unavailable, r.ContextIncomplete},
	} {
		t.Run(row.name, func(t *testing.T) {
			f := newInventoryFixture(t, context.Background(), `{"entries":[{"path":"docs/directory","kind":"directory"},{"path":"docs/missing.md","kind":"missing"},{"path":"docs/file.md","kind":"file","content":"# content\r\n"}]}`, "雪 [q]("+row.name+")\r\n")
			service, err := r.NewSuppliedService(f.reader, r.DefaultServiceLimits())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := service.Close(); err != nil {
					t.Error(err)
				}
			})
			q := inventoryRequest(t, f.reportFixture, row.name, "MD057", r.TextContent, r.RegularFile)
			spec, err := service.Read(context.Background(), q)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			caller := &inventoryCancelContext{Context: ctx, cancel: cancel, at: 2}
			cached, stopped := service.Read(caller, q)
			if !errors.Is(stopped, context.Canceled) || cached.Existence != row.exist || cached.Content != row.content || cached.Source != spec.Source {
				t.Fatal("late caller stop erased cached fact", cached, stopped)
			}
			o, err := f.operation.Record(q, cached)
			if err != nil || o.Treatment() != row.treatment {
				t.Fatal(o, err)
			}
			out := f.result(t, false)
			owner, err := r.NewServiceReport(service, out)
			if err != nil {
				t.Fatal(err)
			}
			if err := owner.Append(f.view, []r.Observation{o}, []r.ServiceFailure{{Request: q, Err: stopped}}); err != nil {
				t.Fatal(err)
			}
			out, err = owner.Finish()
			if err != nil {
				t.Fatal(err)
			}
			engine.FinalizeLintResult(&out)
			if out.Status != "incomplete" || out.Complete || len(out.Interruptions) != 1 || len(out.Errors) != 0 {
				t.Fatal("actual stop became clean/IO", out)
			}
			expected := 0
			if row.treatment == r.ContextIncomplete {
				expected = 1
			}
			if len(out.Coverage) != expected {
				t.Fatal("satisfied known fact gained coverage", out.Coverage)
			}
			if expected == 1 && (len(out.Coverage[0].CauseIDs) != 1 || out.Coverage[0].CauseIDs[0] != out.Interruptions[0].ID) {
				t.Fatal("cached stop cause lost", out.Coverage)
			}
			assertResourceTargetsUnchanged(t, out)
			validateResourceResult(t, out)
		})
	}
}

func TestServiceCachedUnknownAcrossViewsKeepsSourceCauseRelations(t *testing.T) {
	f := newInventoryFixture(t, context.Background(), "", "雪 [q](absent.md)\r\n")
	service, err := r.NewSuppliedService(f.reader, r.DefaultServiceLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})
	out := f.result(t, false)
	// Force collision with a preexisting sparse ID without borrowing its reason.
	out.Interruptions = append(out.Interruptions, result.Interruption{ID: "resource-stop-2", Reason: "cancelled", Phase: "analysis", Message: "independent actual cancellation"})
	owner, err := r.NewServiceReport(service, out)
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		location result.Range
		snapshot result.Snapshot
	}
	var expected []want
	for i, phase := range []r.Phase{r.Original, r.Candidate, r.Saved} {
		text := strings.Repeat("雪", i+1) + " [q](absent.md)\r\n"
		source := f.source
		var replacements []r.Replacement
		if phase != r.Original {
			source = parser.NewSource([]byte(text))
			replacements = []r.Replacement{{TargetID: "docs/a.md", Source: source}}
		}
		view, err := f.operation.NewView(phase, replacements)
		if err != nil {
			t.Fatal(err)
		}
		f.view, f.source, f.text = view, source, text
		q := inventoryRequest(t, f.reportFixture, "absent.md", "MD051", r.TextContent, r.RegularFile)
		// Warm the actual original fact, then cancel only this request's hit.
		if _, err := service.Read(context.Background(), q); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		caller := &inventoryCancelContext{Context: ctx, cancel: cancel, at: 2}
		spec, stop := service.Read(caller, q)
		cancel()
		if !errors.Is(stop, context.Canceled) {
			t.Fatal(stop)
		}
		o, err := f.operation.Record(q, spec)
		if err != nil {
			t.Fatal(err)
		}
		if err := owner.Append(view, []r.Observation{o}, []r.ServiceFailure{{Request: q, Err: stop}}); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(source.Bytes())
		col := utf8.RuneCountInString(text[:strings.Index(text, "absent.md")]) + 1
		expected = append(expected, want{location: result.Range{Start: parser.Position{Line: 1, Column: col}, End: parser.Position{Line: 1, Column: col + len("absent.md")}}, snapshot: result.Snapshot{View: string(phase), SHA256: fmt.Sprintf("%x", hash), SizeBytes: len(text)}})
	}
	out, err = owner.Finish()
	if err != nil {
		t.Fatal(err)
	}
	assert := func() {
		t.Helper()
		if len(out.Coverage) != 3 || len(out.Interruptions) != 2 || len(out.Errors) != 0 {
			t.Fatal(out)
		}
		causes := map[string]result.Interruption{}
		for _, cause := range out.Interruptions {
			if cause.ID == "" {
				t.Fatal(cause)
			}
			if _, duplicate := causes[cause.ID]; duplicate {
				t.Fatal("cause collision")
			}
			causes[cause.ID] = cause
		}
		for _, want := range expected {
			found := false
			for _, coverage := range out.Coverage {
				if coverage.Path == "docs/a.md" && coverage.Rule == "MD051" && coverage.Range != nil && *coverage.Range == want.location && coverage.Snapshot != nil && *coverage.Snapshot == want.snapshot {
					if len(coverage.CauseIDs) != 1 {
						t.Fatal(coverage)
					}
					cause := causes[coverage.CauseIDs[0]]
					found = cause.Reason == "cancelled" && cause.Phase == "acquisition"
				}
			}
			if !found {
				t.Fatal("cached fact borrowed another request's provenance", want, out.Coverage)
			}
		}
	}
	assert()
	engine.FinalizeLintResult(&out)
	assert()
	if out.Status != "incomplete" || out.Complete {
		t.Fatal(out)
	}
	assertResourceTargetsUnchanged(t, out)
	validateResourceResult(t, out)
}

func TestServiceEffectsRejectForeignInvalidAndDuplicateBeforeMutation(t *testing.T) {
	f := newInventoryFixture(t, context.Background(), "", "[q](absent.md)\n")
	s, err := r.NewSuppliedService(f.reader, r.DefaultServiceLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	q := inventoryRequest(t, f.reportFixture, "absent.md", "MD051", r.TextContent, r.RegularFile)
	spec, err := s.Read(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	o, err := f.operation.Record(q, spec)
	if err != nil {
		t.Fatal(err)
	}
	out := f.result(t, false)
	before, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, failures := range [][]r.ServiceFailure{{{Request: q, Err: errors.New("not service evidence")}}, {{Request: r.Request{}, Err: context.Canceled}}, {{Request: q, Err: nil}}, {{Request: q, Err: context.Canceled}, {Request: q, Err: context.Canceled}}} {
		owner, err := r.NewServiceReport(s, out)
		if err != nil {
			t.Fatal(err)
		}
		if err := owner.Append(f.view, []r.Observation{o}, failures); !errors.Is(err, r.ErrContract) {
			t.Fatal("invalid evidence admitted", err)
		}
		afterResult, err := owner.Finish()
		if err != nil {
			t.Fatal(err)
		}
		after, err := json.Marshal(afterResult)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("rejected batch mutated result")
		}
	}
	owner, err := r.NewServiceReport(s, out)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Append(f.view, []r.Observation{o, o}, nil); !errors.Is(err, r.ErrContract) {
		t.Fatal("duplicate observations admitted", err)
	}
	alias := *owner
	if _, err := owner.Finish(); err != nil {
		t.Fatal(err)
	}
	if _, err := alias.Finish(); !errors.Is(err, r.ErrContract) {
		t.Fatal("copy escaped consumption", err)
	}
	if err := alias.Append(f.view, []r.Observation{o}, nil); !errors.Is(err, r.ErrContract) {
		t.Fatal("finished owner accepted append", err)
	}
	if _, err := new(r.ServiceReport).Finish(); !errors.Is(err, r.ErrContract) {
		t.Fatal("zero owner accepted")
	}
}
