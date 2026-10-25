package resource_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	r "github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
)

func TestFragmentQueryStopsKeepAvailableIndexAndExactResultRelations(t *testing.T) {
	for _, reason := range []string{"quota", "cancel", "deadline", "combined"} {
		t.Run(reason, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				opctx := context.Background()
				var deadlineCancel context.CancelFunc
				if reason == "deadline" || reason == "combined" {
					opctx, deadlineCancel = context.WithTimeout(opctx, time.Second)
					defer deadlineCancel()
				}
				f := newInventoryFixture(t, opctx, `{"entries":[]}`, "雪 [query](#source)\r\n\n# Source\n")
				service, err := r.NewSuppliedService(f.reader, r.DefaultServiceLimits())
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := service.Close(); err != nil {
						t.Error(err)
					}
				}()
				type row struct {
					resolver *r.FragmentResolver
					view     r.View
					query    r.FragmentQuery
					snapshot result.Snapshot
					range_   result.Range
				}
				var rows []row
				for n, phase := range []r.Phase{r.Original, r.Candidate, r.Saved} {
					text := strings.Repeat("雪", n+1) + " [query](#source)\r\n\n# Source\n"
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
					normalizer, err := r.NewNormalizer(view, r.DefaultPathLimits(), nil)
					if err != nil {
						t.Fatal(err)
					}
					resolver, err := r.NewFragmentResolver(service, normalizer)
					if err != nil {
						t.Fatal(err)
					}
					start := strings.Index(text, "#source")
					span, _ := source.Span(start, start+len("#source"))
					link, _ := r.NewLink("docs/a.md", "MD051", "#source", source, span)
					query, err := resolver.Prepare(link)
					if err != nil {
						t.Fatal(err)
					}
					warm, err := service.TargetAnchors(context.Background(), view, "docs/a.md")
					if err != nil || warm.Availability() != r.Available {
						t.Fatal(warm.Availability(), err)
					}
					hash := sha256.Sum256(source.Bytes())
					column := n + 1 + len(" [query](") + 1
					rows = append(rows, row{resolver, view, query, result.Snapshot{View: string(phase), SHA256: fmt.Sprintf("%x", hash), SizeBytes: len(text)}, result.Range{Start: parser.Position{Line: 1, Column: column}, End: parser.Position{Line: 1, Column: column + len("#source")}}})
				}
				owner, err := r.NewFragmentReport(rows[0].resolver, f.result(t, false))
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if reason == "quota" || reason == "combined" {
					if err := f.operation.Budget().ChargeWork(f.operation.Budget().Limits().WorkUnits); err == nil {
						t.Fatal("quota not exhausted")
					}
				}
				if reason == "deadline" || reason == "combined" {
					time.Sleep(time.Second)
					synctest.Wait()
				}
				if reason == "cancel" || reason == "combined" {
					cancel()
				}
				for _, row := range rows {
					got, err := row.resolver.Resolve(ctx, row.query, false)
					if err == nil || got.Outcome() != r.FragmentContextUnavailable || got.Anchors().Availability() != r.Available {
						t.Fatal(reason, got.Outcome(), got.Anchors().Availability(), err)
					}
					if _, _, ok := got.Anchors().Content(); !ok {
						t.Fatal("query stop erased acquired content")
					}
					if reason == "combined" {
						var limit *parser.LimitExceeded
						if !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &limit) {
							t.Fatal("compound reasons lost", err)
						}
					}
					if err := owner.Append(row.view, []r.FragmentResolution{got}); err != nil {
						t.Fatal(err)
					}
				}
				out, err := owner.Finish()
				if err != nil {
					t.Fatal(err)
				}
				assert := func() {
					t.Helper()
					if len(out.Coverage) != 3 || len(out.Errors) != 0 || len(out.Diagnostics) != 0 {
						t.Fatal("query deficit lost or invented finding", out)
					}
					causes := map[string]string{}
					for _, stop := range out.Interruptions {
						causes[stop.ID] = stop.Reason
						if stop.Reason == "limit-exceeded" && (stop.Limit == nil || stop.Limit.Name != "work-units" || stop.Limit.Observed <= stop.Limit.Allowed) {
							t.Fatal(stop)
						}
					}
					for _, row := range rows {
						count := 0
						for _, coverage := range out.Coverage {
							if coverage.Snapshot != nil && *coverage.Snapshot == row.snapshot && coverage.Range != nil && *coverage.Range == row.range_ {
								count++
								if coverage.Path != "docs/a.md" || coverage.Rule != "MD051" {
									t.Fatal(coverage)
								}
								reasons := map[string]bool{}
								for _, id := range coverage.CauseIDs {
									reasons[causes[id]] = true
								}
								want := map[string]string{"quota": "limit-exceeded", "cancel": "cancelled", "deadline": "deadline-exceeded"}[reason]
								if reason == "combined" {
									if len(reasons) != 3 || !reasons["limit-exceeded"] || !reasons["cancelled"] || !reasons["deadline-exceeded"] {
										t.Fatal("wrong compound cause links", coverage, causes)
									}
								} else if len(reasons) != 1 || !reasons[want] {
									t.Fatal("wrong cause link", coverage, causes)
								}
							}
						}
						if count != 1 {
							t.Fatal("source snapshot/range relation lost", row, out.Coverage)
						}
					}
				}
				assert()
				engine.FinalizeLintResult(&out)
				assert()
				if out.Status != "incomplete" || out.Complete {
					t.Fatal(out.Status, out.Complete)
				}
				assertResourceTargetsUnchanged(t, out)
				validateResourceResult(t, out)
			})
		})
	}
}
