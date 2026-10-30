package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func auditTestSettings(ids ...string) Settings {
	off, on := false, true
	s := Settings{}
	for _, m := range Catalog() {
		s[m.ID] = RuleSetting{Enabled: &off}
	}
	s["MD087"] = RuleSetting{Enabled: &on}
	for _, id := range ids {
		s[id] = RuleSetting{Enabled: &on}
	}
	return s
}
func auditTestResult(t *testing.T, ctx context.Context, source string, settings Settings, check Check, options RunOptions) (result.Result, []SuppressionDecision) {
	t.Helper()
	r, err := NewCatalogRegistry([]Registration{{ID: "MD087", Check: CheckUnusedSuppressions}, {ID: "MD001", Check: check}})
	if err != nil {
		t.Fatal(err)
	}
	in, err := NewInput("audit.md", []byte(source), settings, DocumentContext{Flavor: "commonmark"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Lint(ctx, []Input{in}, options)
	if err != nil {
		t.Fatal(err)
	}
	var decisions []SuppressionDecision
	for _, record := range out.AnalysisRecords() {
		if a, ok := record.(DirectiveAnalysis); ok {
			decisions = append(decisions, a.audits...)
		}
	}
	return out, decisions
}
func TestSuppressionAuditValidatedPublicationAndOwners(t *testing.T) {
	const source = "# A\n<!-- mdchk:disable MD001 -->\n<!-- mdchk:disable MD001 -->\n### B\n"
	sentinel := errors.New("provider fault")
	for _, tc := range []struct {
		name       string
		start, end int
		partial    bool
		fault      error
		foreign    bool
		want       [2]string
		complete   bool
	}{
		{"complete", 62, 68, false, nil, false, [2]string{"unused", "used"}, true},
		{"partial-positive", 62, 68, true, nil, false, [2]string{"uncertain", "used"}, false},
		{"partial-empty", -1, 0, true, nil, false, [2]string{"uncertain", "uncertain"}, false},
		{"error-positive", 62, 68, false, sentinel, false, [2]string{"uncertain", "used"}, false},
		{"error-empty", -1, 0, false, sentinel, false, [2]string{"uncertain", "uncertain"}, false},
		{"crossing", 60, 63, false, nil, false, [2]string{"used", "unused"}, true},
		{"owner-boundary", 61, 61, false, nil, false, [2]string{"unused", "used"}, true},
		{"byte-zero", 0, 0, false, nil, false, [2]string{"unused", "unused"}, true},
		{"foreign", 62, 68, false, nil, true, [2]string{"uncertain", "uncertain"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			out, decisions := auditTestResult(t, context.Background(), source, auditTestSettings("MD001"), func(_ context.Context, c RuleContext) RuleOutcome {
				calls++
				out := RuleOutcome{Err: tc.fault}
				if tc.partial {
					out.IncompleteReason = "required context unavailable"
				}
				if tc.start >= 0 {
					src := c.Source()
					if tc.foreign {
						src = parser.NewSource([]byte(source))
					}
					span, e := src.Span(tc.start, tc.end)
					if e != nil {
						t.Fatal(e)
					}
					out.Findings = []Finding{{Span: span, Message: "published evidence"}}
				}
				return out
			}, RunOptions{})
			if calls != 1 || out.Complete != tc.complete || len(decisions) != 2 {
				t.Fatal(out, decisions, calls)
			}
			for i, w := range tc.want {
				if decisions[i].Reason != "suppression-"+w {
					t.Fatal(decisions, tc.want)
				}
			}
			if tc.foreign && (len(out.Errors) == 0 || out.Errors[0].Code != "invalid-finding") {
				t.Fatal(out)
			}
		})
	}
}
func TestSuppressionAuditSharedLaterTargetAndNoDemand(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			calls := 0
			r, err := NewCatalogRegistry([]Registration{{ID: "MD087", Check: CheckUnusedSuppressions}, {ID: "MD093", Check: func(context.Context, RuleContext) RuleOutcome { calls++; return RuleOutcome{} }}})
			if err != nil {
				t.Fatal(err)
			}
			s := auditTestSettings("MD093")
			v := s["MD087"]
			v.Enabled = &enabled
			s["MD087"] = v
			in, _ := NewInput("later.md", []byte("<!-- mdchk:disable MD093 -->\n<!-- mdchk:disable MD093 -->\n"), s, DocumentContext{})
			out, err := r.Lint(context.Background(), []Input{in}, RunOptions{})
			want := 0
			if enabled {
				want = 2
			}
			if err != nil || calls != 1 || len(out.Diagnostics) != want || !out.Complete {
				t.Fatal(out, err, calls)
			}
		})
	}
	for _, registered := range []bool{true, false} {
		t.Run("incapable/"+fmt.Sprint(registered), func(t *testing.T) {
			check := func(context.Context, RuleContext) RuleOutcome {
				t.Fatal("incapable target demanded")
				return RuleOutcome{}
			}
			regs := []Registration{{ID: "MD087", Check: CheckUnusedSuppressions}}
			if registered {
				regs = append(regs, Registration{ID: "MD001", Check: check})
			}
			r, _ := NewCatalogRegistry(regs)
			in, _ := NewInput("off.md", []byte("<!-- mdchk:disable MD001 -->\n"), auditTestSettings(), DocumentContext{})
			out, err := r.Lint(context.Background(), []Input{in}, RunOptions{})
			if err != nil || !out.Complete || len(out.Diagnostics) != 1 {
				t.Fatal(out, err)
			}
		})
	}
}
func TestSuppressionAuditInterruptDoesNotCreditReturnedArrays(t *testing.T) {
	for _, kind := range []string{"caller", "work", "nodes", "elapsed"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				limits := parser.DefaultLimits()
				budget, _ := parser.NewOperationBudget(limits)
				out, decisions := auditTestResult(t, ctx, "# A\n<!-- mdchk:disable MD001 -->\n### B\n", auditTestSettings("MD001"), func(_ context.Context, c RuleContext) RuleOutcome {
					span, _ := c.Source().Span(33, 39)
					var cause error
					switch kind {
					case "caller":
						cancel()
						cause = context.Canceled
					case "work":
						cause = budget.ChargeWork(limits.WorkUnits)
					case "nodes":
						cause = budget.ChargeNodes(limits.TotalNodes)
					case "elapsed":
						time.Sleep(limits.OperationTime + time.Nanosecond)
						cause = budget.ChargeWork(0)
					}
					if cause == nil {
						t.Fatal("control did not interrupt")
					}
					return RuleOutcome{Findings: []Finding{{Span: span, Message: "unvalidated"}}, Err: cause}
				}, RunOptions{Budget: budget})
				if len(out.Interruptions) == 0 || len(out.Diagnostics) != 0 || len(out.SuppressedDiagnostics) != 1 || len(decisions) != 1 || decisions[0].Reason != "suppression-uncertain" {
					t.Fatal(out, decisions)
				}
			})
		})
	}
}
func TestSuppressionAuditEarlierConclusionSurvivesLaterInterruption(t *testing.T) {
	for _, kind := range []string{"cancel", "work", "nodes"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			calls := 0
			r, err := NewCatalogRegistry([]Registration{{ID: "MD087", Check: CheckUnusedSuppressions}, {ID: "MD001", Check: func(context.Context, RuleContext) RuleOutcome { return RuleOutcome{} }}, {ID: "MD093", Check: func(_ context.Context, c RuleContext) RuleOutcome {
				calls++
				switch kind {
				case "cancel":
					cancel()
					return RuleOutcome{Err: context.Canceled}
				case "work":
					return RuleOutcome{Err: budget.ChargeWork(budget.Limits().WorkUnits)}
				default:
					return RuleOutcome{Err: budget.ChargeNodes(budget.Limits().TotalNodes)}
				}
			}}})
			if err != nil {
				t.Fatal(err)
			}
			in, _ := NewInput("prior.md", []byte("<!-- mdchk:disable MD001 MD093 -->\n"), auditTestSettings("MD001", "MD093"), DocumentContext{})
			out, err := r.Lint(ctx, []Input{in}, RunOptions{Budget: budget})
			if err != nil || calls != 1 || len(out.Diagnostics) != 1 || out.Diagnostics[0].Message != "Suppression for MD001 hides no findings." || len(out.Interruptions) == 0 {
				t.Fatal(out, err, calls)
			}
		})
	}
}

// Each row follows the literal charged addends in auditCharge and
// reserveAuditSlice. No operation counter, parser execution or elapsed fitting
// supplies an expected quota. Root reviews this file only after independent
// derivation from held production source.
func TestSuppressionAuditOriginalControlsBeforeRetention(t *testing.T) {
	for _, origin := range []bool{false, true} {
		for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(fmt.Sprintf("origin-%v/%v", origin, cause), func(t *testing.T) {
				caller, original := context.Background(), context.Background()
				if origin {
					original = auditStoppedContext{context.Background(), cause}
				} else {
					caller = auditStoppedContext{context.Background(), cause}
				}
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				c := RuleContext{operation: original, budget: budget}
				_, err := reserveAuditSlice(caller, c, []int(nil))
				if !errors.Is(err, cause) {
					t.Fatal(err)
				}
			})
		}
	}
	for _, tc := range []struct {
		name        string
		work, nodes int64
		want        string
	}{
		{"work-before-copy", 1, 100, "work-units"},
		{"nodes-before-copy", 100, 1, "total-nodes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := parser.DefaultLimits()
			limits.WorkUnits = tc.work
			limits.TotalNodes = tc.nodes
			limits.DocumentNodes = min(limits.DocumentNodes, tc.nodes)
			budget, _ := parser.NewOperationBudget(limits)
			c := RuleContext{operation: context.Background(), budget: budget}
			// len=cap=1 -> capacity2, copy1 + capacity2 + checkpoint1.
			original := []int{7}
			next, err := reserveAuditSlice(context.Background(), c, original)
			var limit *parser.LimitExceeded
			if !errors.As(err, &limit) || limit.Name != tc.want || next != nil || original[0] != 7 {
				t.Fatal(next, err)
			}
			if err := budget.ChargeWork(0); err == nil {
				t.Fatal("failure not sticky")
			}
		})
	}
}

type auditStoppedContext struct {
	context.Context
	cause error
}

func (c auditStoppedContext) Err() error { return c.cause }

func TestSuppressionAuditSourceDerivedPlanningLimits(t *testing.T) {
	// Expected attempted charges are handwritten from held production addends:
	// entry; candidate count; effect count; bulk owner/output retention; scheduling
	// scans; target indexing. No parser or operation counter supplies these rows.
	for _, tc := range []struct {
		name        string
		work, nodes int64
		want        string
		observed    int64
	}{
		{"candidate-work", 1, 100, "work-units", 3},
		{"candidate-nodes", 1000, 1, "total-nodes", 2},
		{"effect-work", 4, 100, "work-units", 5},
		{"bulk-work", 37, 100, "work-units", 38},
		{"bulk-nodes", 1000, 11, "total-nodes", 12},
		{"catalog-work", 215, 100, "work-units", 216},
		{"target-index-nodes", 1000, 18, "total-nodes", 19},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			limits := parser.DefaultLimits()
			limits.WorkUnits = tc.work
			limits.TotalNodes = tc.nodes
			limits.DocumentNodes = min(limits.DocumentNodes, tc.nodes)
			budget, err := parser.NewOperationBudget(limits)
			if err != nil {
				t.Fatal(err)
			}
			r, err := NewCatalogRegistry([]Registration{{ID: "MD087", Check: CheckUnusedSuppressions}})
			if err != nil {
				t.Fatal(err)
			}
			source := parser.NewSource([]byte("<!-- mdchk:disable MD001 -->\n"))
			token, _ := source.Span(19, 24)
			comment, _ := source.Span(0, 28)
			closeMarker, _ := source.Span(25, 28)
			state := DirectiveResult{operation: budget.Token(), source: source, complete: true,
				rules:      map[string]ResolvedRule{"MD001": {Enabled: true}, "MD087": {Enabled: true}},
				candidates: []DirectiveCandidate{{Operation: DirectiveDisable, Span: comment, Closing: closeMarker, Disposition: DirectiveApplied, Effects: []DirectiveEffect{{Rule: "MD001", Token: token, Disposition: DirectiveApplied}}}},
				intervals: map[string][]DirectiveInterval{
					"MD001": {{Start: 0, End: 28, Enabled: true, Owner: -1}, {Start: 28, End: 29, Owner: 0, Token: token}},
					"MD087": {{Start: 0, End: 29, Enabled: true, Owner: -1}},
				},
			}
			index := parser.NewLineIndex(source)
			c := RuleContext{input: Input{source: source, path: "literal.md"}, index: index, operation: ctx, budget: budget, directives: state}
			projection := projector{ctx: ctx, budget: budget, source: source, index: index, reporting: &reportingReserve{}}
			out := result.NewLint("cli")
			audit, err := planSuppressionAudit(ctx, r, c, &projection, result.Snapshot{}, &out, nil)
			var limit *parser.LimitExceeded
			if audit.registered || !errors.As(err, &limit) || limit.Name != tc.want || limit.Observed != tc.observed || len(out.Diagnostics) != 0 {
				t.Fatal(audit, err, out)
			}
			if budget.ChargeWork(0) == nil {
				t.Fatal("original failure was reset")
			}
		})
	}
}

func TestSuppressionAuditApplicabilityAndMissingContext(t *testing.T) {
	for _, applies := range []bool{false, true} {
		t.Run(fmt.Sprint(applies), func(t *testing.T) {
			catalog := Catalog()
			for i := range catalog {
				if catalog[i].ID == "MD001" {
					value := applies
					catalog[i].AppliesTo = func(DocumentContext) bool { return value }
				}
			}
			calls := 0
			r, err := NewRegistry(catalog, []Registration{{ID: "MD087", Check: CheckUnusedSuppressions}, {ID: "MD001", Check: func(context.Context, RuleContext) RuleOutcome {
				calls++
				return RuleOutcome{IncompleteReason: "required authorized integration unavailable"}
			}}})
			if err != nil {
				t.Fatal(err)
			}
			in, _ := NewInput("context.md", []byte("<!-- mdchk:disable MD001 -->\n"), auditTestSettings("MD001"), DocumentContext{})
			out, err := r.Lint(context.Background(), []Input{in}, RunOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if applies {
				if calls != 1 || out.Complete || len(out.Diagnostics) != 0 {
					t.Fatal(out, calls)
				}
			} else {
				if calls != 0 || !out.Complete || len(out.Diagnostics) != 1 {
					t.Fatal(out, calls)
				}
			}
		})
	}
}

func TestSuppressionAuditObservationBypassesEligibilityAndOwnsControls(t *testing.T) {
	// An observational context can return false without entering terminal syntax,
	// protection, directive permission or GFM candidate machinery. No document is
	// supplied: entering those paths would require unavailable parser authority.
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			source := parser.NewSource([]byte("text"))
			span, _ := source.Span(4, 4)
			c := RuleContext{input: Input{source: source, documentContext: DocumentContext{Flavor: flavor}}, observational: true, operation: context.Background(), budget: budget, syntaxReliable: true}
			eligible, err := c.TerminalEditEligible(context.Background(), "MD047", span)
			if err != nil || eligible {
				t.Fatal(eligible, err)
			}
			c.operation = auditStoppedContext{context.Background(), context.Canceled}
			if _, err := c.TerminalEditEligible(context.Background(), "MD047", span); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestSuppressionAuditRejectsInvalidGeometry(t *testing.T) {
	const source = "😀 <!-- mdchk:disable MD001 -->\r\n# A\r\n"
	for _, kind := range []string{"absent", "scalar", "crlf", "message"} {
		t.Run(kind, func(t *testing.T) {
			out, decisions := auditTestResult(t, context.Background(), source, auditTestSettings("MD001"), func(_ context.Context, c RuleContext) RuleOutcome {
				span, _ := c.Source().Span(35, 40)
				message := "invalid evidence"
				switch kind {
				case "absent":
					span = parser.Span{}
				case "scalar":
					span, _ = c.Source().Span(1, 2)
				case "crlf":
					span, _ = c.Source().Span(34, 34)
				case "message":
					message = " "
				}
				return RuleOutcome{Findings: []Finding{{Span: span, Message: message}}}
			}, RunOptions{})
			if out.Complete || len(out.Errors) == 0 || out.Errors[0].Code != "invalid-finding" || len(out.Diagnostics) != 0 || len(decisions) != 1 || decisions[0].Reason != "suppression-uncertain" {
				t.Fatal(out, decisions)
			}
		})
	}
}
func TestSuppressionAuditIndependentDocumentsAndRequests(t *testing.T) {
	calls := 0
	r, err := NewCatalogRegistry([]Registration{{ID: "MD087", Check: CheckUnusedSuppressions}, {ID: "MD001", Check: func(_ context.Context, c RuleContext) RuleOutcome {
		calls++
		if c.Path() == "used.md" {
			span, _ := c.Source().Span(33, 39)
			return RuleOutcome{Findings: []Finding{{Span: span, Message: "hidden evidence"}}}
		}
		return RuleOutcome{}
	}}})
	if err != nil {
		t.Fatal(err)
	}
	for request := 0; request < 2; request++ {
		clean, _ := NewInput("clean.md", []byte("<!-- mdchk:disable MD001 -->\n# A\n"), auditTestSettings("MD001"), DocumentContext{})
		used, _ := NewInput("used.md", []byte("# A\n<!-- mdchk:disable MD001 -->\n### B\n"), auditTestSettings("MD001"), DocumentContext{})
		out, err := r.Lint(context.Background(), []Input{used, clean}, RunOptions{})
		if err != nil || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Path != "clean.md" {
			t.Fatal(out, err)
		}
	}
	if calls != 4 {
		t.Fatal("cross-source observation reuse", calls)
	}
}

func TestSuppressionAuditCheckedCapacityArithmetic(t *testing.T) {
	maxHost := int(^uint(0) >> 1)
	for _, row := range []struct{ a, b int64 }{{-1, 1}, {1, -1}, {int64(^uint64(0) >> 1), 1}} {
		if _, err := auditAdd(row.a, row.b); !errors.Is(err, parser.ErrModel) {
			t.Fatal(row, err)
		}
	}
	if _, err := auditHostSum(maxHost, 1); !errors.Is(err, parser.ErrModel) {
		t.Fatal(err)
	}
	if n, err := auditHostSum(1, 2); err != nil || n != 3 {
		t.Fatal(n, err)
	}
}
func TestSuppressionDecisionAccessorsCheckOriginAndOwnership(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			origin := auditStoppedContext{context.Background(), cause}
			state := DirectiveResult{operation: budget.Token()}
			rows := []SuppressionDecision{{Rule: "MD001", Reason: "suppression-used"}}
			analysis := DirectiveAnalysis{state: state, audits: rows, auditOperation: origin}
			if out, err := analysis.SuppressionDecisions(context.Background(), budget); out != nil || !errors.Is(err, cause) {
				t.Fatal(out, err)
			}
			trace := InspectionTrace{directives: state, audits: rows, auditOperation: origin}
			if out, err := trace.SuppressionDecisions(context.Background(), budget); out != nil || !errors.Is(err, cause) {
				t.Fatal(out, err)
			}
		})
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	state := DirectiveResult{operation: budget.Token()}
	analysis := DirectiveAnalysis{state: state, audits: []SuppressionDecision{{Rule: "MD001", Reason: "suppression-used"}}, auditOperation: context.Background()}
	copy, err := analysis.SuppressionDecisions(context.Background(), budget)
	if err != nil {
		t.Fatal(err)
	}
	copy[0].Reason = "mutated"
	if analysis.audits[0].Reason != "suppression-used" {
		t.Fatal("mutable audit alias")
	}
	other, _ := parser.NewOperationBudget(parser.DefaultLimits())
	if out, err := analysis.SuppressionDecisions(context.Background(), other); out != nil || !errors.Is(err, parser.ErrModel) {
		t.Fatal(out, err)
	}
}

func TestSuppressionAuditFailureReserveChargesPriorBatchCapacity(t *testing.T) {
	for _, tc := range []struct {
		name        string
		work, nodes int64
		want        string
		observed    int64
	}{
		{"prior-errors-work", 96, 1000, "work-units", 97},
		{"prior-errors-nodes", 1000, 63, "total-nodes", 64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := parser.DefaultLimits()
			limits.WorkUnits = tc.work
			limits.TotalNodes = tc.nodes
			limits.DocumentNodes = min(limits.DocumentNodes, tc.nodes)
			budget, _ := parser.NewOperationBudget(limits)
			c := RuleContext{operation: context.Background(), budget: budget}
			out := result.NewLint("cli")
			out.Errors = make([]result.Error, 32)
			out.Errors[0].Code = "prior-file"
			err := reserveAuditFailures(context.Background(), c, &out)
			var limit *parser.LimitExceeded
			if !errors.As(err, &limit) || limit.Name != tc.want || limit.Observed != tc.observed || len(out.Errors) != 32 || cap(out.Errors) != 32 || out.Errors[0].Code != "prior-file" {
				t.Fatal(err, out)
			}
		})
	}
}
func TestSuppressionAuditInvalidRetentionFailurePreservesTypedRecords(t *testing.T) {
	ctx := context.Background()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	c := RuleContext{operation: ctx, budget: budget}
	out := result.NewLint("cli")
	out.Errors = make([]result.Error, 2)
	out.Coverage = make([]result.Coverage, 2)
	if err := reserveAuditFailures(ctx, c, &out); err != nil {
		t.Fatal(err)
	}
	errorCapacity, coverageCapacity, stopCapacity := cap(out.Errors), cap(out.Coverage), cap(out.Interruptions)
	actual := budget.ChargeWork(budget.Limits().WorkUnits)
	if actual == nil {
		t.Fatal("expected original exhaustion")
	}
	record := result.Error{Category: result.Internal, Code: "invalid-finding", Scope: "operation", Message: "already validated invalid span"}
	coverage := result.Coverage{Reason: result.ContextUnavailable, Message: "already materialized invalid finding"}
	err := publishInvalidAuditFinding(ctx, c, &out, record, coverage)
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || out.Errors[2].Code != "invalid-finding" || cap(out.Errors) != errorCapacity || cap(out.Coverage) != coverageCapacity {
		t.Fatal(out, err)
	}
	// The reserved scalar tail also retains the interruption and callback's
	// independently published noninterrupt failure/incompleteness without growth.
	recordInterruption(&out, "test.md", "analysis", actual)
	out.Coverage = append(out.Coverage, result.Coverage{Reason: result.ContextUnavailable})
	out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "rule-failure", Scope: "operation"})
	out.Coverage = append(out.Coverage, result.Coverage{Reason: result.ContextUnavailable})
	if cap(out.Errors) != errorCapacity || cap(out.Coverage) != coverageCapacity || cap(out.Interruptions) != stopCapacity {
		t.Fatal("post-stop record allocation")
	}
}

func TestSuppressionAuditNoDemandRetainsNoOperation(t *testing.T) {
	for _, registered := range []bool{false, true} {
		t.Run(fmt.Sprint(registered), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			_ = budget.ChargeWork(budget.Limits().WorkUnits)
			_ = budget.ChargeWork(1)
			regs := []Registration{}
			if registered {
				regs = append(regs, Registration{ID: "MD087", Check: CheckUnusedSuppressions})
			}
			r, err := NewCatalogRegistry(regs)
			if err != nil {
				t.Fatal(err)
			}
			c := RuleContext{operation: ctx, budget: budget, directives: DirectiveResult{rules: map[string]ResolvedRule{"MD087": {Enabled: false}}}}
			// A cancelled/exhausted caller makes even a zero-unit audit charge
			// fail. No-demand planning must therefore return before any charge.
			a, err := planSuppressionAudit(ctx, r, c, nil, result.Snapshot{}, nil, nil)
			if err != nil || a.rc.operation != nil || a.rc.budget != nil || a.decisions != nil || a.owners != nil || a.targets != nil || a.coverageIndex != -1 || a.registered != registered {
				t.Fatal(a, err)
			}
			if err := a.start(ctx); err != nil {
				t.Fatal(err)
			}
			a.finish()
		})
	}
}

func TestSuppressionDecisionEmptyViewsKeepTokenCallerAndBudget(t *testing.T) {
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	analysis := DirectiveAnalysis{state: DirectiveResult{operation: budget.Token()}}
	if rows, err := analysis.SuppressionDecisions(context.Background(), budget); err != nil || len(rows) != 0 || analysis.auditOperation != nil {
		t.Fatal(rows, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if rows, err := analysis.SuppressionDecisions(ctx, budget); rows != nil || !errors.Is(err, context.Canceled) {
		t.Fatal(rows, err)
	}
	other, _ := parser.NewOperationBudget(parser.DefaultLimits())
	if rows, err := analysis.SuppressionDecisions(context.Background(), other); rows != nil || !errors.Is(err, parser.ErrModel) {
		t.Fatal(rows, err)
	}
	_ = budget.ChargeWork(budget.Limits().WorkUnits)
	if rows, err := analysis.SuppressionDecisions(context.Background(), budget); rows != nil || err == nil {
		t.Fatal(rows, err)
	}
}

func TestSuppressionAuditReportingReserveIsSharedAndNeverProof(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	source := parser.NewSource([]byte("original\n"))
	reserve := &reportingReserve{}
	p := projector{ctx: ctx, budget: budget, source: source, index: parser.NewLineIndex(source), reporting: reserve}
	span, _ := source.Span(0, 0)
	in := Input{source: source, path: "report.md"}
	finding := Finding{Span: span, Message: "published"}
	if _, err := makeDiagnostic(in, &p, result.Snapshot{}, Metadata{ID: "MD001"}, finding); err != nil || p.stop == nil || reserve.budget == nil {
		t.Fatal(err, p.stop)
	}
	strict := p
	strict.strict = true
	if _, err := makeDiagnostic(in, &strict, result.Snapshot{}, Metadata{ID: "MD001"}, finding); !errors.Is(err, context.Canceled) {
		t.Fatal("reporting became audit authority", err)
	}
	// Exhaust the existing reserve directly rather than fitting a threshold to
	// engine output. A second projector must share the sticky finite allowance.
	originalReserve := reserve.budget
	_ = reserve.budget.ChargeWork(reserve.budget.Limits().WorkUnits)
	second := projector{ctx: ctx, budget: budget, source: source, index: parser.NewLineIndex(source), reporting: reserve}
	d, err := makeDiagnostic(in, &second, result.Snapshot{}, Metadata{ID: "MD001"}, finding)
	if !omittedProjection(err) || d.Message != "" || reserve.budget != originalReserve {
		t.Fatal("partial record or renewed reserve", d, err)
	}
}

func TestSuppressionAuditReportingRetentionRejectsWholeRecord(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	source := parser.NewSource([]byte("original\n"))
	reserve := &reportingReserve{}
	p := projector{ctx: ctx, budget: budget, source: source, index: parser.NewLineIndex(source), reporting: reserve}
	span, _ := source.Span(0, 0)
	d, err := makeDiagnostic(Input{source: source, path: "report.md"}, &p, result.Snapshot{}, Metadata{ID: "MD001"}, Finding{Span: span, Message: "published"})
	if err != nil {
		t.Fatal(err)
	}
	rows := []result.Diagnostic{d}
	_ = reserve.budget.ChargeNodes(reserve.budget.Limits().TotalNodes)
	// No append follows an unaffordable whole-record reservation. The existing
	// record is preserved, and another target cannot acquire a fresh reserve.
	retained, err := reservePresentedSlice(&p, rows)
	if !omittedProjection(err) || retained != nil || len(rows) != 1 || rows[0].Message != "published" {
		t.Fatal(retained, rows, err)
	}
	second := projector{ctx: ctx, budget: budget, source: source, index: parser.NewLineIndex(source), reporting: reserve}
	if _, err := reservePresentedSlice(&second, []result.Diagnostic(nil)); !omittedProjection(err) {
		t.Fatal("retention restarted reporting", err)
	}
}

// These bounds are handwritten from held production v9 before execution:
// growth capacities 1+2+4+8+16+32 = 63 nodes;
// (old length + new capacity + 1) at those six growths = 100 work.
// Reusing capacity, including zero requested slots, has no inventory debit.
func TestSuppressionAuditBatchGrowthSourceDerivedAccounting(t *testing.T) {
	for _, counter := range []string{"work", "nodes"} {
		t.Run(counter, func(t *testing.T) {
			limits := parser.DefaultLimits()
			limits.WorkUnits, limits.TotalNodes, limits.DocumentNodes = 100, 63, 63
			budget, err := parser.NewOperationBudget(limits)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			c := RuleContext{operation: ctx, budget: budget}
			var rows, capacities []int
			for i := range 32 {
				prior := rows
				next, err := reserveAuditSlots(ctx, c, rows, 1)
				if err != nil {
					t.Fatal(i, err)
				}
				if cap(next) != cap(prior) {
					capacities = append(capacities, cap(next))
				} else if len(prior) > 0 && &next[0] != &prior[0] {
					t.Fatal("copied despite spare capacity")
				}
				rows = append(next, i)
			}
			if !slices.Equal(capacities, []int{1, 2, 4, 8, 16, 32}) {
				t.Fatal(capacities)
			}
			for i, v := range rows {
				if v != i {
					t.Fatal("growth lost prefix", rows)
				}
			}
			for range 32 {
				next, err := reserveAuditSlots(ctx, c, rows, 0)
				if err != nil || len(next) != 32 || cap(next) != 32 || &next[0] != &rows[0] {
					t.Fatal("clean suffix copied or charged prefix", next, err)
				}
			}
			var charge error
			wantName, wantObserved, wantAllowed := "work-units", int64(101), int64(100)
			if counter == "work" {
				charge = budget.ChargeWork(1)
			} else {
				wantName, wantObserved, wantAllowed = "total-nodes", 64, 63
				charge = budget.ChargeNodes(1)
			}
			var limit *parser.LimitExceeded
			if !errors.As(charge, &limit) || limit.Name != wantName || limit.Observed != wantObserved || limit.Allowed != wantAllowed {
				t.Fatal("source-derived cumulative debit changed", charge)
			}
		})
	}
}

func TestSuppressionAuditBatchCleanPlanningDoesNotDebitPriorInventory(t *testing.T) {
	for _, counter := range []string{"work", "nodes"} {
		t.Run(counter, func(t *testing.T) {
			// The minimal trusted coordinator fixture has R=1, J(MD087)=1,
			// C=E=pending=0. Per call W=1+1+1+(3+2+1)=9, N=1+1+2=4.
			// Thirty-two calls share one meter: exactly W=288, N=128.
			// Existing output is not part of either debit and cannot be recopied.
			limits := parser.DefaultLimits()
			limits.WorkUnits, limits.TotalNodes, limits.DocumentNodes = 288, 128, 128
			budget, err := parser.NewOperationBudget(limits)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			source := parser.NewSource([]byte("# A\n"))
			r := &Registry{checks: map[string]Check{"MD087": CheckUnusedSuppressions}, ids: []string{"MD087"}}
			state := DirectiveResult{operation: budget.Token(), source: source, complete: true,
				rules:     map[string]ResolvedRule{"MD087": {Enabled: true}},
				intervals: map[string][]DirectiveInterval{"MD087": {{Start: 0, End: 4, Enabled: true, Owner: -1}}}}
			c := RuleContext{operation: ctx, budget: budget, directives: state, input: Input{path: "clean.md", source: source}}
			out := result.NewLint("project")
			out.Diagnostics = make([]result.Diagnostic, 32)
			for i := range out.Diagnostics {
				out.Diagnostics[i] = result.Diagnostic{Path: fmt.Sprintf("prior-%02d.md", i), Rule: "MD087", Message: "preserved"}
			}
			out.Coverage = []result.Coverage{{Path: "prior.md", Reason: result.ContextUnavailable, Message: "preserved"}}
			diagnostics, coverage := out.Diagnostics, out.Coverage
			for range 32 {
				a, err := planSuppressionAudit(ctx, r, c, &projector{}, result.Snapshot{}, &out, nil)
				if err != nil || a.pending != 0 || a.coverageIndex != -1 {
					t.Fatal(a, err)
				}
				if err := a.start(ctx); err != nil {
					t.Fatal(err)
				}
				a.finish()
				if len(out.Diagnostics) != 32 || cap(out.Diagnostics) != 32 || &out.Diagnostics[0] != &diagnostics[0] || len(out.Coverage) != 1 || cap(out.Coverage) != 1 || &out.Coverage[0] != &coverage[0] {
					t.Fatal("clean planning recopied operation-wide output")
				}
			}
			for i, d := range out.Diagnostics {
				if d.Path != fmt.Sprintf("prior-%02d.md", i) || d.Message != "preserved" {
					t.Fatal(d)
				}
			}
			var charge error
			wantName, wantObserved, wantAllowed := "work-units", int64(289), int64(288)
			if counter == "work" {
				charge = budget.ChargeWork(1)
			} else {
				wantName, wantObserved, wantAllowed = "total-nodes", 129, 128
				charge = budget.ChargeNodes(1)
			}
			var limit *parser.LimitExceeded
			if !errors.As(charge, &limit) || limit.Name != wantName || limit.Observed != wantObserved || limit.Allowed != wantAllowed {
				t.Fatal("clean planning debit changed", charge)
			}
		})
	}
}
