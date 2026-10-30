package inspect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules"
)

func md087Definition(on bool, targets ...string) string {
	enabled := map[string]bool{"MD087": on}
	for _, id := range targets {
		enabled[id] = true
	}
	var b strings.Builder
	b.WriteString("directives: {enabled: true}\nrules:\n")
	for _, m := range engine.Catalog() {
		fmt.Fprintf(&b, "  %s: {enabled: %v}\n", m.ID, enabled[m.ID])
	}
	return b.String()
}
func TestMD087PreparedReasonsAndFullBeforeFocus(t *testing.T) {
	source := "# A\n<!-- mdchk:disable MD001 -->\n<!-- mdchk:disable MD001 -->\n### B\n"
	s, _ := projectionFixture(t, source, md087Definition(true, "MD001"), rules.Registrations())
	full := analyze(t, s, false)
	if !full.Result.Complete || len(full.Result.Diagnostics) != 1 {
		t.Fatal(full.Result)
	}
	ds := *full.Projection.Directives.Data
	if len(ds) != 2 || ds[0].Effects[0].Reason != "suppression-unused" || ds[1].Effects[0].Reason != "suppression-used" {
		t.Fatal(ds)
	}
	for _, d := range ds {
		if d.State != "effective" || d.Effects[0].Effect != "disable" || d.Effects[0].Permission != "allowed" {
			t.Fatal(d)
		}
	}
	selected := filter(t, s, full, "4")
	if len(selected.Result.Diagnostics) != 0 || !selected.Result.Complete {
		t.Fatal(selected.Result)
	}
	for i, d := range *selected.Projection.Directives.Data {
		if !d.ContextOnly || d.Effects[0].Reason != ds[i].Effects[0].Reason {
			t.Fatal(d)
		}
	}
	validateProduced(t, full)
	validateProduced(t, selected)
}
func TestMD087MixedReasonsDeduplicationAndInactiveObservationReuse(t *testing.T) {
	source := "# A\n<!-- mdchk:disable MD047 MD001 MD047 -->\n### B\n"
	for _, observe := range []bool{false, true} {
		t.Run(fmt.Sprint(observe), func(t *testing.T) {
			calls := map[string]int{}
			regs := rules.Registrations()
			for i, registration := range regs {
				if registration.ID == "MD001" || registration.ID == "MD047" {
					id, check := registration.ID, registration.Check
					regs[i].Check = func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome { calls[id]++; return check(ctx, c) }
				}
			}
			s, _ := projectionFixture(t, source, md087Definition(true, "MD001", "MD047"), regs)
			a := analyze(t, s, observe)
			d := (*a.Projection.Directives.Data)[0]
			if !a.Result.Complete || calls["MD001"] != 1 || calls["MD047"] != 1 || len(d.NamedTokens) != 3 || len(d.Effects) != 2 || d.Effects[0].Reason != "suppression-unused" || d.Effects[1].Reason != "suppression-used" {
				t.Fatal(a.Result, d, calls)
			}
			validateProduced(t, a)
		})
	}
}
func TestMD087UncertainBlockedExcludedAndNotRequested(t *testing.T) {
	for _, tc := range []struct {
		name, source, definition string
		reason                   string
		complete                 bool
	}{
		{"unavailable", "<!-- mdchk:disable MD011 -->\n", strings.Replace(md087Definition(true), "  MD011: {enabled: false}\n", "", 1), "suppression-uncertain", false},
		{"blocked", "<!-- mdchk:disable MD001 -->\n", strings.Replace(md087Definition(true, "MD001"), "MD001: {enabled: true}", "MD001: {enabled: true, allow_directives: false}", 1), "", true},
		{"excluded", "<!-- mdchk:disable MD087 -->\n", md087Definition(true), "suppression-excluded", true},
		{"not-requested", "<!-- mdchk:disable MD001 -->\n", md087Definition(false, "MD001"), "suppression-not-requested", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := projectionFixture(t, tc.source, tc.definition, rules.Registrations())
			a := analyze(t, s, false)
			if a.Result.Complete != tc.complete || len(a.Result.Diagnostics) != 0 {
				t.Fatal(a.Result)
			}
			d := (*a.Projection.Directives.Data)[0]
			if d.Effects[0].Reason != tc.reason {
				t.Fatal(d)
			}
			if tc.name == "blocked" && (d.Effects[0].Permission != "denied" || d.Effects[0].Effect != "blocked" || len(d.Effects[0].Provenance) == 0) {
				t.Fatal(d)
			}
			validateProduced(t, a)
		})
	}
}
func TestMD087ProducerSchema(t *testing.T) {
	// The scoped ProducerSchema child process supplies the independent
	// validator interpreter. Keep these shapes under this entry so both the
	// schema and semantic mutation checks execute for the promised cases.
	t.Run("interrupted-independent-publication", TestMD087IndependentPublicationAfterInterruption)
	t.Run("two-owner-full-and-focused", TestMD087PreparedReasonsAndFullBeforeFocus)
	t.Run("mixed-dedup-and-shared-observation", TestMD087MixedReasonsDeduplicationAndInactiveObservationReuse)
	t.Run("uncertain-blocked-excluded-and-not-requested", TestMD087UncertainBlockedExcludedAndNotRequested)
	s, _ := projectionFixture(t, "<!-- mdchk:disable MD001 MD087 -->\n", md087Definition(true, "MD001"), rules.Registrations())
	a := analyze(t, s, false)
	validateProduced(t, a)
	// Typed restoration and filtering must preserve the optional per-effect field.
	raw, e := json.Marshal(a.Result)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(raw), `"reason":"suppression-unused"`) {
		t.Fatal(string(raw))
	}
	validateProduced(t, filter(t, s, a, "1"))
}

// A callback can publish an independently reportable positive without admitting
// new audit evidence after the original execution authority has stopped.
func TestMD087IndependentPublicationAfterInterruption(t *testing.T) {
	for _, control := range []string{"cancel", "elapsed"} {
		for _, observe := range []bool{false, true} {
			for _, publication := range []string{"reliable", "foreign", "atomic"} {
				t.Run(fmt.Sprintf("%s/observe=%v/%s", control, observe, publication), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						ctx, cancel := context.WithCancel(context.Background())
						defer cancel()
						calls := 0
						regs := []engine.Registration{{ID: "MD087", Check: engine.CheckUnusedSuppressions}, {ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
							calls++
							span, _ := c.Source().Span(33, 39)
							if publication == "foreign" {
								span, _ = parser.NewSource([]byte("foreign")).Span(0, 0)
							}
							if control == "cancel" {
								cancel()
							} else {
								time.Sleep(parser.DefaultLimits().OperationTime + time.Nanosecond)
							}
							if publication == "atomic" {
								return engine.RuleOutcome{}
							}
							return engine.RuleOutcome{Findings: []engine.Finding{{Span: span, Message: "published target finding"}}}
						}}}
						s, _ := projectionFixture(t, "# A\n<!-- mdchk:disable MD001 -->\n### B\n", md087Definition(true, "MD001"), regs)
						a, err := Analyze(ctx, s, AnalyzeOptions{ObserveInactive: observe})
						if control == "cancel" {
							if !errors.Is(err, context.Canceled) {
								t.Fatal(err)
							}
						} else {
							var limit *parser.LimitExceeded
							if !errors.As(err, &limit) || limit.Name != "operation-time" || ctx.Err() != nil {
								t.Fatal(err)
							}
						}
						if calls != 1 || a.Result.Complete || len(a.Result.Diagnostics) != 0 || len(a.Result.Interruptions) != 1 {
							t.Fatal(calls, a.Result)
						}
						if a.Projection.Directives.Data == nil {
							t.Fatal("lost prepared directives")
						}
						d := (*a.Projection.Directives.Data)[0]
						if d.Effects[0].Reason != "suppression-uncertain" {
							t.Fatal("presentation credited audit proof", d)
						}
						r := rule(t, a, "MD001")
						if r.ActiveEvaluation != "partial" && r.ActiveEvaluation != "failed" || r.SourceCoverage.Active.Availability != "unknown" || len(r.SourceCoverage.Active.ByteRanges) != 0 || len(r.SourceCoverage.Active.Ranges) != 0 {
							t.Fatal(r)
						}
						want := 0
						if publication == "reliable" {
							want = 1
						}
						if len(a.Result.SuppressedDiagnostics) != want {
							t.Fatal("published finding retention", a.Result)
						}
						if publication == "foreign" && (len(a.Result.Errors) != 1 || a.Result.Errors[0].Code != "invalid-finding" || r.ActiveEvaluation != "failed") {
							t.Fatal("invalid publication accepted", a.Result, r)
						}
						validateProduced(t, a)
					})
				})
			}
		}
	}
}
