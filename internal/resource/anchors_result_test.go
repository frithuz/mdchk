package resource_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	r "github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
)

func TestAnchorUncertaintyAndIndexStopsKeepContentAndSourceRelations(t *testing.T) {
	foreign := "# Foo\n\n# Foo\n\n<svg><title><span id=foo-1>live</span></title></svg>\n"
	for _, test := range []struct {
		name, text string
		options    parser.ParseOptions
		stopped    bool
	}{
		{"uncertain-html", "<div id='unterminated>\n", parser.ParseOptions{}, false},
		{"actual-quota-and-cancel", foreign, parser.ParseOptions{}, true},
		{"foreign-commonmark", foreign, parser.ParseOptions{}, false},
		{"foreign-gfm", foreign, parser.ParseOptions{GFM: true}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			stopped := test.stopped
			base := newReportFixture(t, context.Background(), "雪 [one](asset.md) [two](asset.md)\r\n", test.options)
			data := []byte(fmt.Sprintf(`{"entries":[{"path":"docs/asset.md","kind":"file","content":%q}]}`, test.text))
			input, err := r.DecodeSupplied(context.Background(), base.operation.Budget(), r.DefaultInputLimits(), data, []r.SuppliedFile{{Key: mustKey(t, "docs/a.md"), Source: base.source}})
			if err != nil {
				t.Fatal(err)
			}
			reader, err := r.NewSuppliedReader(context.Background(), base.operation, input, r.DefaultInputLimits())
			if err != nil {
				t.Fatal(err)
			}
			f := inventoryFixture{base, reader, input}
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
			owner, err := r.NewServiceReport(service, out)
			if err != nil {
				t.Fatal(err)
			}
			type row struct {
				view     r.View
				requests []r.Request
				text     string
				source   parser.Source
			}
			var rows []row
			for n, phase := range []r.Phase{r.Original, r.Candidate, r.Saved} {
				text := strings.Repeat("雪", n+1) + " [one](asset.md) [two](asset.md)\r\n"
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
				local := reportFixture{operation: f.operation, view: view, source: source, text: text}
				requests := []r.Request{local.request(t, "asset.md", false, r.ParsedAnchors), local.request(t, "asset.md", true, r.ParsedAnchors)}
				// The fixture's literal destination and root-relative spelling differ.
				for i, request := range requests {
					requests[i], err = view.Request(r.RequestSpec{Key: mustKey(t, "docs/asset.md"), Link: request.Link(), Class: r.Local, Capability: r.ParsedAnchors, Required: r.RegularFile})
					if err != nil {
						t.Fatal(err)
					}
				}
				rows = append(rows, row{view, requests, text, source})
			}
			if stopped {
				warm := inventoryRequest(t, f.reportFixture, "asset.md", "MD057", r.TextContent, r.RegularFile)
				content, err := service.Read(context.Background(), warm)
				if err != nil || content.Content != r.Available {
					t.Fatal(content, err)
				}
				if err := f.operation.Budget().ChargeWork(f.operation.Budget().Limits().WorkUnits); err == nil {
					t.Fatal("actual work quota was not exhausted")
				}
			}
			type expected struct {
				phase    r.Phase
				snapshot result.Snapshot
				range_   result.Range
			}
			var wants []expected
			for _, row := range rows {
				var observations []r.Observation
				var failures []r.ServiceFailure
				ctx := context.Background()
				if stopped {
					cancelled, cancel := context.WithCancel(ctx)
					cancel()
					ctx = cancelled
				}
				for i, request := range row.requests {
					spec, readErr := service.Read(ctx, request)
					wantAvailability := r.Uncertain
					if stopped {
						wantAvailability = r.Interrupted
						var limit *parser.LimitExceeded
						if !errors.Is(readErr, context.Canceled) || !errors.As(readErr, &limit) || limit.Name != "work-units" {
							t.Fatal("index work lost actual compound stops", readErr)
						}
					} else if readErr != nil {
						t.Fatal("syntax uncertainty invented operational error", readErr)
					}
					if spec.Existence != r.File || spec.Content != r.Available || spec.Anchors != wantAvailability || spec.Validated.Source() != spec.Source || spec.Source == (parser.Source{}) {
						t.Fatal("index deficit erased valid content", spec)
					}
					if _, err := spec.Index.Lookup(context.Background(), "not-a-proved-miss"); !errors.Is(err, r.ErrContract) {
						t.Fatal("incomplete index supplied negative authority", err)
					}
					observation, err := f.operation.Record(request, spec)
					if err != nil || observation.Treatment() != r.ContextIncomplete {
						t.Fatal(observation, err)
					}
					observations = append(observations, observation)
					if readErr != nil {
						failures = append(failures, r.ServiceFailure{Request: request, Err: readErr})
					}
					start := strings.Index(row.text, "asset.md")
					if i == 1 {
						start = strings.LastIndex(row.text, "asset.md")
					}
					column := utf8.RuneCountInString(row.text[:start]) + 1
					hash := sha256.Sum256(row.source.Bytes())
					wants = append(wants, expected{row.view.Phase(), result.Snapshot{View: string(row.view.Phase()), SHA256: fmt.Sprintf("%x", hash), SizeBytes: row.source.Len()}, result.Range{Start: parser.Position{Line: 1, Column: column}, End: parser.Position{Line: 1, Column: column + len("asset.md")}}})
				}
				if err := owner.Append(row.view, observations, failures); err != nil {
					t.Fatal(err)
				}
			}
			out, err = owner.Finish()
			if err != nil {
				t.Fatal(err)
			}
			assert := func() {
				t.Helper()
				if len(out.Errors) != 0 || len(out.Diagnostics) != 0 || len(out.Coverage) != len(wants) {
					t.Fatal("invented or lost outcome", out)
				}
				causes := map[string]string{}
				for _, stop := range out.Interruptions {
					causes[stop.ID] = stop.Reason
					if stop.Reason == "limit-exceeded" && (stop.Limit == nil || stop.Limit.Name != "work-units" || stop.Limit.Observed <= stop.Limit.Allowed) {
						t.Fatal("exact limit evidence lost", stop)
					}
				}
				for _, want := range wants {
					matches := 0
					for _, coverage := range out.Coverage {
						if coverage.Snapshot != nil && *coverage.Snapshot == want.snapshot && coverage.Range != nil && *coverage.Range == want.range_ {
							matches++
							if stopped {
								reasons := map[string]bool{}
								for _, id := range coverage.CauseIDs {
									reasons[causes[id]] = true
								}
								if len(reasons) != 2 || !reasons["cancelled"] || !reasons["limit-exceeded"] {
									t.Fatal("wrong cause/source relation", want.phase, coverage, causes)
								}
							} else if len(coverage.CauseIDs) != 0 {
								t.Fatal("uncertain syntax borrowed a failure cause", coverage)
							}
						}
					}
					if matches != 1 {
						t.Fatal("source/view/range relation lost", want, out.Coverage)
					}
				}
			}
			assert()
			engine.FinalizeLintResult(&out)
			assert()
			if out.Status != "incomplete" || out.Complete {
				t.Fatal("anchor deficit became clean", out)
			}
			assertResourceTargetsUnchanged(t, out)
			validateResourceResult(t, out)
		})
	}
}
