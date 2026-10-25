package resource

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestSelectedTargetParseIndependentOfAnchorsAndFullView(t *testing.T) {
	f := newServiceFixture(t, context.Background(), "", DefaultServiceLimits())
	if f.service.OwnsOperation(nil) || (*Service)(nil).OwnsOperation(f.op) || !f.service.OwnsOperation(f.op) {
		t.Fatal("service identity widened")
	}
	if err := (*Service)(nil).Checkpoint(context.Background(), 0); !errors.Is(err, ErrContract) {
		t.Fatal(err)
	}
	foreign := newServiceFixture(t, context.Background(), "", DefaultServiceLimits())
	if f.service.OwnsOperation(foreign.op) {
		t.Fatal("foreign operation owns service")
	}
	if _, err := f.service.TargetParse(context.Background(), foreign.view, "source.md"); !errors.Is(err, ErrContract) {
		t.Fatal(err)
	}
	for _, phase := range []Phase{Original, Candidate, Saved} {
		source := f.source
		var replacements []Replacement
		if phase != Original {
			source = parser.NewSource([]byte("<svg><title><span id=x>foreign</span></title></svg>\n\n```{r}\n#| label: value\n```\n"))
			replacements = []Replacement{{TargetID: "source.md", Source: source}}
		}
		view, err := f.op.NewView(phase, replacements)
		if err != nil {
			t.Fatal(err)
		}
		out, err := f.service.TargetParse(context.Background(), view, "source.md")
		if err != nil || out.Availability() != Available || out.Source().Token() != source.Token() {
			t.Fatal(out, err)
		}
		parsed, ok := out.Parsed()
		if !ok || parsed.Status != parser.Complete || parsed.Document.Source().Token() != source.Token() {
			t.Fatal(parsed, ok)
		}
		if _, err := parsed.Document.FencedBlocks(context.Background(), f.op.budget); err != nil {
			t.Fatal(err)
		}
		if phase != Original {
			anchors, err := f.service.TargetAnchors(context.Background(), view, "source.md")
			if err != nil || anchors.Availability() != Uncertain {
				t.Fatal("HTML uncertainty affected core syntax", anchors.Availability(), err)
			}
		}
	}
	saved, err := f.op.NewView(Saved, []Replacement{{TargetID: "source.md", Unavailable: true}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.service.TargetParse(context.Background(), saved, "source.md")
	if err != nil || out.Availability() != Unavailable || out.Source().Token() != (parser.SourceToken{}) {
		t.Fatal("saved fallback", out, err)
	}
	if _, ok := out.Parsed(); ok {
		t.Fatal("unavailable saved parsed original")
	}
}

func TestSelectedTargetParseCoalescesAndRetainsFactsAfterClose(t *testing.T) {
	f := newServiceFixture(t, context.Background(), "", DefaultServiceLimits())
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			out, err := f.service.TargetParse(context.Background(), f.view, "source.md")
			if err != nil || out.Availability() != Available {
				t.Error(out, err)
			}
		})
	}
	group.Wait()
	if len(f.service.parsed) != 1 || len(f.service.targetEvidence) != 1 || len(f.service.targetParseKeys) != 1 {
		t.Fatal("selected parse duplicated retention", len(f.service.parsed), len(f.service.targetEvidence), len(f.service.targetParseKeys))
	}
	if err := f.service.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := f.service.TargetParse(context.Background(), f.view, "source.md")
	if !errors.Is(err, context.Canceled) || out.Availability() != Available {
		t.Fatal("known parse lost or close hidden", out, err)
	}
	parsed, ok := out.Parsed()
	if !ok || parsed.Status != parser.Complete {
		t.Fatal(parsed, ok)
	}
	if err := f.service.Checkpoint(context.Background(), 0); !errors.Is(err, context.Canceled) {
		t.Fatal("closed service admitted new work", err)
	}
}

// Every nonempty subset of real quota/deadline/caller/Close controls must keep
// the same admitted full-policy parse, while denying further producer work.
func TestSelectedTargetParseReadyStopMatrix(t *testing.T) {
	for mask := 1; mask < 16; mask++ {
		t.Run(fmt.Sprintf("controls-%02d", mask), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				opctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				f := newServiceFixture(t, opctx, "", DefaultServiceLimits())
				var views []View
				var warm []TargetParseResult
				for _, phase := range []Phase{Original, Candidate, Saved} {
					var replacements []Replacement
					if phase != Original {
						replacements = []Replacement{{TargetID: "source.md", Source: parser.NewSource([]byte("```{r " + string(phase) + "}\n```\n"))}}
					}
					v, err := f.op.NewView(phase, replacements)
					if err != nil {
						t.Fatal(err)
					}
					out, err := f.service.TargetParse(context.Background(), v, "source.md")
					if err != nil || out.Availability() != Available {
						t.Fatal(out, err)
					}
					parsed, ok := out.Parsed()
					if !ok {
						t.Fatal("missing completed witness")
					}
					if _, err := parsed.Document.FencedBlocks(context.Background(), f.op.budget); err != nil {
						t.Fatal("unproved warm parse", err)
					}
					views = append(views, v)
					warm = append(warm, out)
				}
				// Unwarmed equal-byte replacements and unavailable Saved have distinct IDs.
				stale, err := f.op.NewView(Candidate, []Replacement{{TargetID: "source.md", Source: parser.NewSource(warm[1].Source().Bytes())}})
				if err != nil {
					t.Fatal(err)
				}
				saved, err := f.op.NewView(Saved, []Replacement{{TargetID: "source.md", Unavailable: true}})
				if err != nil {
					t.Fatal(err)
				}
				cold := newServiceFixture(t, opctx, "", DefaultServiceLimits())
				before := targetParseCounts(f.service)
				var want *parser.LimitExceeded
				if mask&1 != 0 {
					if err := f.op.budget.ChargeWork(f.op.budget.Limits().WorkUnits + 1); !errors.As(err, &want) {
						t.Fatal(err)
					}
				}
				if mask&8 != 0 {
					if err := f.service.Close(); err != nil {
						t.Fatal(err)
					}
				}
				if mask&2 != 0 {
					time.Sleep(2 * time.Second)
					synctest.Wait()
				}
				caller, end := context.WithCancel(context.Background())
				defer end()
				if mask&4 != 0 {
					end()
				}
				for i, v := range views {
					got, err := f.service.TargetParse(caller, v, strings.Clone("source.md"))
					parsed, ok := got.Parsed()
					original, _ := warm[i].Parsed()
					if got.Availability() != Available || got.Source().Token() != warm[i].Source().Token() || !ok || parsed.Status != parser.Complete || parsed.Document != original.Document || got.state.key != warm[i].state.key {
						t.Fatal("ready admitted parse erased by stop", mask, v.Phase(), got, err)
					}
					if err == nil || errors.Is(err, context.Canceled) != (mask&(4|8) != 0) || errors.Is(err, context.DeadlineExceeded) != (mask&2 != 0) {
						t.Fatal("lost independent stop", mask, err)
					}
					var actual *parser.LimitExceeded
					if errors.As(err, &actual) != (want != nil) || want != nil && *actual != *want {
						t.Fatal("lost first exact quota", mask, err)
					}
				}
				for _, probe := range []struct {
					v  View
					id string
				}{{views[0], "unknownxx"}, {views[0], strings.Repeat("x", 1<<16)}, {stale, "source.md"}, {saved, "source.md"}, {cold.view, "source.md"}} {
					got, err := f.service.TargetParse(caller, probe.v, probe.id)
					if err == nil || got.Source() != (parser.Source{}) {
						t.Fatal("stopped foreign/cold/stale lookup acquired evidence", got, err)
					}
					if _, ok := got.Parsed(); ok {
						t.Fatal("stopped miss parsed")
					}
				}
				if targetParseCounts(f.service) != before {
					t.Fatal("stopped lookup grew admission/cache/index")
				}
			})
		})
	}
}

func targetParseCounts(s *Service) [6]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, index := range s.targetParseReady {
		n += len(index.ready)
	}
	return [6]int{len(s.validations), len(s.targetEvidence), len(s.targetParseKeys), len(s.parsed), len(s.targetParseReady), n}
}

func TestSelectedTargetParseReadyTinyResidualAndColdStop(t *testing.T) {
	// Supplied-only work belongs to this bubble. While its test goroutine is
	// runnable the fake clock cannot advance: neither warm operation receives
	// schedule-dependent real polling ticks. Quiesce the owned worker after each
	// completed parse before measuring/transferring the exact work count.
	synctest.Test(t, func(t *testing.T) {
		prepare := func() (serviceFixture, View, TargetParseResult) {
			f := newServiceFixture(t, context.Background(), "", DefaultServiceLimits())
			source := parser.NewSource([]byte("```{r}\n" + strings.Repeat("opaque code\n", 8192) + "```\n"))
			v, err := f.op.NewView(Candidate, []Replacement{{TargetID: "source.md", Source: source}})
			if err != nil {
				t.Fatal(err)
			}
			got, err := f.service.TargetParse(context.Background(), v, "source.md")
			if err != nil {
				t.Fatal(err)
			}
			synctest.Wait()
			return f, v, got
		}
		measured, _, _ := prepare()
		units := measured.op.budget.Limits().WorkUnits + 1
		var first *parser.LimitExceeded
		if err := measured.op.budget.ChargeWork(units); !errors.As(err, &first) {
			t.Fatal(err)
		}
		used := first.Observed - units
		f, v, warm := prepare()
		if err := f.op.budget.ChargeWork(f.op.budget.Limits().WorkUnits - used - 1); err != nil {
			t.Fatal("warm work oracle differs", err)
		}
		before := targetParseCounts(f.service)
		caller, cancel := context.WithCancel(context.Background())
		cancel()
		for _, id := range []string{strings.Clone("source.md"), "unknownxx", strings.Repeat("x", 1<<20)} {
			got, err := f.service.TargetParse(caller, v, id)
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if id == "source.md" && (got.Source().Token() != warm.Source().Token() || got.Availability() != Available) {
				t.Fatal("ready facts lost with one work unit left")
			}
		}
		if targetParseCounts(f.service) != before {
			t.Fatal("misses grew admission/cache/index")
		}
		if err := f.op.budget.ChargeWork(1); err != nil {
			t.Fatal("stopped reconciliation consumed live work", err)
		}
		var actual *parser.LimitExceeded
		if err := f.op.budget.ChargeWork(1); !errors.As(err, &actual) || actual.Observed != actual.Allowed+1 {
			t.Fatal("residual oracle", err)
		}
		cold := newServiceFixture(t, context.Background(), "", DefaultServiceLimits())
		before = targetParseCounts(cold.service)
		if err := cold.op.budget.ChargeWork(cold.op.budget.Limits().WorkUnits + 1); err == nil {
			t.Fatal("quota not exhausted")
		}
		got, err := cold.service.TargetParse(context.Background(), cold.view, "source.md")
		if err == nil || got.Source() != (parser.Source{}) || targetParseCounts(cold.service) != before {
			t.Fatal("cold stopped query admitted data", got, err)
		}
	})
}

func TestSelectedTargetParseSharedSourceIDsKeepDistinctReadySlots(t *testing.T) {
	ctx := context.Background()
	root, _ := NewRoot(SuppliedRoot)
	budget, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := NewSettings(parser.ParseOptions{}, "index.md", "github", "ready-alias-v1")
	source := parser.NewSource([]byte("```{r shared}\n```\n"))
	a, _ := NewKey("a.md")
	b, _ := NewKey("b.md")
	op, err := NewOperation(ctx, budget, root, settings, []Target{{ID: "a.md", Source: source, ResourceKey: a}, {ID: "b.md", Source: source, ResourceKey: b}})
	if err != nil {
		t.Fatal(err)
	}
	input, err := DecodeSupplied(ctx, budget, DefaultInputLimits(), nil, []SuppliedFile{{Key: a, Source: source}, {Key: b, Source: source}})
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
	defer func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	}()
	view, err := op.NewView(Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a.md", "b.md"} {
		if got, err := service.TargetParse(ctx, view, id); err != nil || got.Availability() != Available {
			t.Fatal(id, got, err)
		}
	}
	counts := targetParseCounts(service)
	if counts[1] != 1 || counts[2] != 1 || counts[3] != 1 || counts[5] != 2 {
		t.Fatal("source aliases duplicated parse or lost ID reservation", counts)
	}
	if err := budget.ChargeWork(budget.Limits().WorkUnits + 1); err == nil {
		t.Fatal("quota not exhausted")
	}
	for _, id := range []string{"a.md", "b.md"} {
		got, err := service.TargetParse(ctx, view, id)
		parsed, ok := got.Parsed()
		if err == nil || got.Source().Token() != source.Token() || got.Availability() != Available || !ok || parsed.Status != parser.Complete {
			t.Fatal("shared source lost exact ready ID", id, got, err)
		}
	}
	if targetParseCounts(service) != counts {
		t.Fatal("stopped aliases grew state")
	}
}
