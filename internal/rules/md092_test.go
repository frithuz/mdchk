package rules

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

type md092Finding struct {
	Rule, Message, Marker string
	ByteRange             result.ByteRange `json:"byte_range"`
	Range                 result.Range
}

type md092Literal struct {
	Name, Source  string
	Bytes         int    `json:"source_bytes"`
	SHA           string `json:"source_sha256"`
	Configuration struct {
		Enabled     bool
		Omitted     bool `json:"enabled_omitted"`
		MD087       bool `json:"md087_enabled"`
		Allow       bool `json:"allow_directives"`
		Frontmatter string
	}
	Expected struct {
		Complete             bool
		Evaluation           string `json:"md092_evaluation"`
		Findings, Suppressed []md092Finding
		MD087                []md092Finding       `json:"md087_findings"`
		Decisions            []md092AuditDecision `json:"audit_decisions"`
	}
}

// md092AuditDecision is only a test expectation for detached original facts.
type md092AuditDecision struct {
	Owner, Effect int
	Rule, Reason  string
	Token         result.ByteRange
	Interval      struct {
		Start, End int
		EOF        bool `json:"includes_final_eof"`
	}
}

type md092RawPresentation struct {
	Name       string
	SHA        string               `json:"source_sha256"`
	Requested  []md092AuditDecision `json:"requested_md087_decisions"`
	Additional []md092AuditDecision `json:"additional_nonrequested_presentation"`
	Complete   []md092AuditDecision `json:"complete_raw_decisions"`
}

func md092EqualDecisions(a, b []md092AuditDecision) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// This separate source-reasoned artifact was authored AFTER failed attempt01.
// It clarifies full raw presentation; original requested oracles stay unchanged.
func md092RawPresentations(t *testing.T, cases []md092Literal) map[string]md092RawPresentation {
	t.Helper()
	b, err := os.ReadFile("testdata/md092_raw_presentation.json")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "ccb36a8cffafc426bb8dcbb6a2c8566f40a5b057f155896ab11ea8458f515292" {
		t.Fatal("raw presentation identity", err)
	}
	var fixture struct {
		Identities map[string]string `json:"fixture_identities"`
		Cases      []md092RawPresentation
	}
	if err = json.Unmarshal(b, &fixture); err != nil {
		t.Fatal(err)
	}
	identities := map[string]string{
		"internal/rules/testdata/md092.json":              "1a60725172f8b8e5038604145766f2c9cd9c9ce00b3acb16d8b3ca078477bb25",
		"internal/rules/testdata/md092_raw_lifetime.json": "1d5ab8daa2ce6d3315e7439f36311d22a24881aac59b9d3f3831f81a2131df32",
		"internal/rules/testdata/md092_unknown.json":      "486afc71751f0ea8c3ffd8a94f7bdfb09dced1731cd36a9ded5e0ce801827aed",
	}
	if !reflect.DeepEqual(fixture.Identities, identities) || len(cases) != 90 || len(fixture.Cases) != 90 {
		t.Fatal("raw presentation scope", fixture.Identities, len(cases), len(fixture.Cases))
	}
	out := map[string]md092RawPresentation{}
	for _, row := range fixture.Cases {
		if _, exists := out[row.Name]; exists {
			t.Fatal("duplicate presentation case", row.Name)
		}
		out[row.Name] = row
	}
	seen := map[string]bool{}
	additional := 0
	for _, tc := range cases {
		row, exists := out[tc.Name]
		if !exists || seen[tc.Name] || row.SHA != tc.SHA || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != row.SHA {
			t.Fatal("presentation source join", tc.Name, row)
		}
		seen[tc.Name] = true
		if !md092EqualDecisions(row.Requested, tc.Expected.Decisions) {
			t.Fatal("requested oracle changed", tc.Name, row.Requested, tc.Expected.Decisions)
		}
		combined := append(append([]md092AuditDecision{}, row.Requested...), row.Additional...)
		if !md092EqualDecisions(combined, row.Complete) {
			t.Fatal("incomplete raw expectation", tc.Name, row)
		}
		for _, d := range row.Requested {
			if d.Reason != "suppression-used" && d.Reason != "suppression-unused" && d.Reason != "suppression-uncertain" {
				t.Fatal("unexpected requested conclusion", d)
			}
		}
		for _, d := range row.Additional {
			if d.Reason != "suppression-not-requested" {
				t.Fatal("unexpected additional presentation", d)
			}
			additional++
		}
	}
	if len(seen) != len(out) || additional != 2 {
		t.Fatal("raw inventory scope", len(seen), len(out), additional)
	}
	return out
}

// Every raw accessor row is checked before any named requested projection.
func md092AssertRawDecisions(t *testing.T, source parser.Source, got []engine.SuppressionDecision, want []md092AuditDecision) []md092AuditDecision {
	t.Helper()
	if len(got) != len(want) {
		t.Fatal("complete raw decision inventory", got, want)
	}
	out := make([]md092AuditDecision, 0, len(got))
	for i, d := range got {
		w := want[i]
		if d.Owner != w.Owner || d.Effect != w.Effect || d.Rule != w.Rule || d.Reason != w.Reason || d.Token.SourceToken() != source.Token() || d.Token.Start() != w.Token.Start || d.Token.End() != w.Token.End || d.Token.Start() < 0 || d.Token.End() <= d.Token.Start() || d.Token.End() > source.Len() || d.Interval.Start != w.Interval.Start || d.Interval.End != w.Interval.End || d.Interval.Start < 0 || d.Interval.End < d.Interval.Start || d.Interval.End > source.Len() || d.Interval.Enabled || d.Interval.Owner != w.Owner || d.Interval.Token != d.Token || (d.Interval.End == source.Len()) != w.Interval.EOF {
			t.Fatal("raw original decision", d, w)
		}
		text, err := source.Text(d.Token)
		if err != nil || text != d.Rule {
			t.Fatal("original decision token spelling", text, err, d)
		}
		actual := md092AuditDecision{Owner: d.Owner, Effect: d.Effect, Rule: d.Rule, Reason: d.Reason, Token: result.ByteRange{Start: d.Token.Start(), End: d.Token.End()}}
		actual.Interval.Start, actual.Interval.End, actual.Interval.EOF = d.Interval.Start, d.Interval.End, d.Interval.End == source.Len()
		out = append(out, actual)
	}
	return out
}

func md092AssertRequestedDecisions(t *testing.T, validated []md092AuditDecision, row md092RawPresentation, original []md092AuditDecision) {
	t.Helper()
	var requested, additional []md092AuditDecision
	for _, d := range validated {
		switch d.Reason {
		case "suppression-used", "suppression-unused", "suppression-uncertain":
			requested = append(requested, d)
		case "suppression-not-requested":
			additional = append(additional, d)
		default:
			t.Fatal("unclassified already-validated raw record", d)
		}
	}
	if !md092EqualDecisions(requested, original) || !md092EqualDecisions(requested, row.Requested) || !md092EqualDecisions(additional, row.Additional) {
		t.Fatal("requested/additional inventories", requested, additional, original, row)
	}
}

func md092Literals(t *testing.T) []md092Literal {
	t.Helper()
	return md092Fixture(t, "md092.json", "1a60725172f8b8e5038604145766f2c9cd9c9ce00b3acb16d8b3ca078477bb25", 81)
}

func md092Fixture(t *testing.T, name, hash string, count int) []md092Literal {
	t.Helper()
	//nolint:gosec // Fixed hash-pinned repository fixture selected by this test.
	b, err := os.ReadFile("testdata/" + name)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != hash {
		t.Fatal("root literal oracle identity", name, err)
	}
	var oracle struct{ Cases []md092Literal }
	if err = json.Unmarshal(b, &oracle); err != nil || len(oracle.Cases) != count {
		t.Fatal(name, err, len(oracle.Cases))
	}
	return oracle.Cases
}

func md092AllLiterals(t *testing.T) []md092Literal {
	t.Helper()
	out := md092Literals(t)
	out = append(out, md092Fixture(t, "md092_raw_lifetime.json", "1d5ab8daa2ce6d3315e7439f36311d22a24881aac59b9d3f3831f81a2131df32", 6)...)
	return append(out, md092Fixture(t, "md092_unknown.json", "486afc71751f0ea8c3ffd8a94f7bdfb09dced1731cd36a9ded5e0ce801827aed", 3)...)
}

func md092Settings() engine.Settings {
	s := engine.Settings{}
	off, on := false, true
	for _, m := range engine.Catalog() {
		s[m.ID] = engine.RuleSetting{Enabled: &off}
	}
	s["MD092"] = engine.RuleSetting{Enabled: &on}
	return s
}

func md092Plan(t *testing.T, r *engine.Registry, tc md092Literal, flavor string) *engine.ExecutionPlan {
	t.Helper()
	s := md092Settings()
	s["MD092"] = engine.RuleSetting{Enabled: &tc.Configuration.Enabled, AllowDirectives: &tc.Configuration.Allow}
	if tc.Configuration.Omitted {
		s["MD092"] = engine.RuleSetting{AllowDirectives: &tc.Configuration.Allow}
	}
	s["MD087"] = engine.RuleSetting{Enabled: &tc.Configuration.MD087}
	dc := engine.DocumentContext{Flavor: flavor}
	if tc.Configuration.Frontmatter != "none" {
		var err error
		dc.Frontmatter, err = parser.NewFrontmatterOptions(true, []string{tc.Configuration.Frontmatter})
		if err != nil {
			t.Fatal(err)
		}
	}
	sel, err := r.SelectRules(s, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := sel.Plan(dc, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func md092AssertDiagnostic(t *testing.T, source string, got result.Diagnostic, want md092Finding) {
	t.Helper()
	if got.Rule != want.Rule || got.Severity != "error" || got.Message != want.Message || got.ByteRange != want.ByteRange || got.Range != want.Range || got.FixAvailable || got.Snapshot != (result.Snapshot{View: "original", SizeBytes: len(source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}) {
		t.Fatal(got, want)
	}
	if want.ByteRange.Start < 0 || want.ByteRange.End > len(source) || want.ByteRange.End <= want.ByteRange.Start || source[want.ByteRange.Start:want.ByteRange.End] != want.Marker {
		t.Fatal("invalid oracle range", want)
	}
}

func TestMD092FrozenProductionOracle(t *testing.T) {
	for _, inspect := range []bool{false, true} {
		md092CheckProductionLiterals(t, md092AllLiterals(t), inspect)
	}
}

func md092CheckProductionLiterals(t *testing.T, cases []md092Literal, inspect bool) {
	t.Helper()
	presentations := md092RawPresentations(t, cases)
	r := registry(t, Registrations())
	for _, tc := range cases {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(tc.Name+"/"+flavor+fmt.Sprintf("/inspect-%v", inspect), func(t *testing.T) {
				if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
					t.Fatal("source identity")
				}
				p := md092Plan(t, r, tc, flavor)
				original := []byte(tc.Source)
				in, err := p.NewInput("literal.md", original)
				if err != nil {
					t.Fatal(err)
				}
				token := in.Source().Token()
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				var out result.Result
				var trace *engine.InspectionTrace
				if inspect {
					out, trace, err = p.Inspect(context.Background(), in, engine.RunOptions{Budget: budget}, engine.InspectionOptions{})
				} else {
					out, err = p.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Budget: budget})
				}
				if err != nil || out.Complete != tc.Expected.Complete || len(out.Errors) != 0 || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 || string(original) != tc.Source || string(in.Source().Bytes()) != tc.Source || token != in.Source().Token() {
					t.Fatal(out, err)
				}
				if len(out.Files) != 1 || out.Files[0].WriteStatus != "not-requested" || len(out.Files[0].ChangeIDs) != 0 {
					t.Fatal(out.Files)
				}
				if inspect {
					found := false
					for _, evaluation := range trace.Evaluations() {
						if evaluation.Rule == "MD092" {
							found = true
							switch tc.Expected.Evaluation {
							case "not-demanded":
								if evaluation.Active != "skipped" || evaluation.Observational != "not-requested" || len(evaluation.ActiveCoverage)+len(evaluation.ObservationalCoverage) != 0 {
									t.Fatal(evaluation)
								}
							case "complete":
								if evaluation.Active != "complete" && evaluation.Observational != "complete" {
									t.Fatal(evaluation)
								}
								spans := append(append([]parser.Span{}, evaluation.ActiveCoverage...), evaluation.ObservationalCoverage...)
								if len(spans) != 1 || spans[0].SourceToken() != token || spans[0].End() != len(tc.Source) {
									t.Fatal(evaluation)
								}
							case "unavailable":
								if evaluation.Active != "partial" && evaluation.Observational != "partial" || len(evaluation.ActiveCoverage)+len(evaluation.ObservationalCoverage) != 0 {
									t.Fatal(evaluation)
								}
							default:
								t.Fatal("unknown literal evaluation", tc.Expected.Evaluation)
							}
						}
					}
					if !found {
						t.Fatal("MD092 evaluation absent")
					}
				}
				want := append(append([]md092Finding{}, tc.Expected.Findings...), tc.Expected.MD087...)
				sort.Slice(want, func(i, j int) bool { return want[i].ByteRange.Start < want[j].ByteRange.Start })
				if len(out.Diagnostics) != len(want) {
					t.Fatal(out.Diagnostics, want)
				}
				for i, d := range out.Diagnostics {
					md092AssertDiagnostic(t, tc.Source, d, want[i])
				}
				var hidden []engine.DirectiveSuppression
				var decisions []engine.SuppressionDecision
				for _, record := range out.AnalysisRecords() {
					if a, ok := record.(engine.DirectiveAnalysis); ok {
						hidden = append(hidden, a.Suppressed()...)
						rows, e := a.SuppressionDecisions(context.Background(), budget)
						if e != nil {
							t.Fatal(e)
						}
						decisions = append(decisions, rows...)
					}
				}
				if len(hidden) != len(tc.Expected.Suppressed) {
					t.Fatal(hidden, decisions, tc.Expected)
				}
				for i, h := range hidden {
					md092AssertDiagnostic(t, tc.Source, h.Diagnostic, tc.Expected.Suppressed[i])
					if h.Owner != 0 || h.Token.SourceToken() != token {
						t.Fatal(h)
					}
				}
				presentation := presentations[tc.Name]
				validated := md092AssertRawDecisions(t, in.Source(), decisions, presentation.Complete)
				md092AssertRequestedDecisions(t, validated, presentation, tc.Expected.Decisions)
				if inspect {
					rows, e := trace.SuppressionDecisions(context.Background(), budget)
					if e != nil {
						t.Fatal(e)
					}
					observed := md092AssertRawDecisions(t, in.Source(), rows, presentation.Complete)
					md092AssertRequestedDecisions(t, observed, presentation, tc.Expected.Decisions)
				}
				var wantCoverage []result.Coverage
				if !tc.Expected.Complete {
					wantCoverage = append(wantCoverage, result.Coverage{Path: "literal.md", Rule: "MD092", Reason: result.ContextUnavailable, Message: "Conflict-marker protection context is unavailable; the complete MD092 inventory is withheld."})
					if tc.Configuration.MD087 {
						wantCoverage = append(wantCoverage, result.Coverage{Path: "literal.md", Rule: "MD087", Reason: result.ContextUnavailable, Message: "Suppression usefulness is uncertain; required target observations are incomplete."})
					}
				}
				gotCoverage := append([]result.Coverage{}, out.Coverage...)
				sort.Slice(gotCoverage, func(i, j int) bool { return gotCoverage[i].Rule < gotCoverage[j].Rule })
				sort.Slice(wantCoverage, func(i, j int) bool { return wantCoverage[i].Rule < wantCoverage[j].Rule })
				if len(gotCoverage) != len(wantCoverage) {
					t.Fatal(gotCoverage, wantCoverage)
				}
				for i := range wantCoverage {
					wantCoverage[i].ID = fmt.Sprintf("coverage-%d", i+1)
					if !reflect.DeepEqual(gotCoverage[i], wantCoverage[i]) {
						t.Fatal(gotCoverage, wantCoverage)
					}
				}

			})
		}
	}
}

func TestMD092OriginalStickyCallbackControls(t *testing.T) {
	for _, source := range []string{"", "clean", "`code`", "```\n<<<<<<<\n```", "<<<<<<<", "<<<<<<< before\n\n<div>\n<x a=\""} {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			t.Run(source+"/"+kind, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					b, _ := parser.NewOperationBudget(parser.DefaultLimits())
					called := false
					r := registry(t, []engine.Registration{{ID: "MD092", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						switch kind {
						case "work":
							_ = b.ChargeWork(b.Limits().WorkUnits)
						case "nodes":
							_ = b.ChargeNodes(b.Limits().TotalNodes)
						case "elapsed":
							time.Sleep(61 * time.Second)
						}
						var out engine.RuleOutcome
						for range 2 {
							out = conflictMarkers(ctx, c)
							var limit *parser.LimitExceeded
							if !errors.As(out.Err, &limit) || out.Findings != nil || out.IncompleteReason != "" {
								t.Fatal(out)
							}
						}
						called = true
						return out
					}}})
					analyzeHeadings(t, r, source, md092Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: b})
					if !called {
						t.Fatal("callback absent")
					}
				})
			})
		}
	}
}

// Function identities come from held source, not sampled checkpoint counters.
// The context interrupts only a named deferred publication gate; it also proves
// that gate was reached after the actual positive comparison precondition.
type md092PublicationStop struct {
	context.Context
	gate    string
	reason  error
	reached bool
}

func (c *md092PublicationStop) Err() error {
	if c.gate == "" {
		return c.Context.Err()
	}
	var pcs [32]uintptr
	n := runtime.Callers(1, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if strings.HasSuffix(frame.Function, c.gate) {
			c.reached = true
			return c.reason
		}
		if !more {
			break
		}
	}
	return c.Context.Err()
}
func TestMD092ComparisonAndDistinctOuterFinalControls(t *testing.T) {
	for _, source := range []string{"", "clean", "`code`", "```\n<<<<<<<\n```", "<<<<<<<", "<<<<<<< before\n\n<div>\n<x a=\""} {
		for _, whole := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%q/whole-%v/origin-%v/%v", source, whole, originating, reason), func(t *testing.T) {
						origin := &md092PublicationStop{Context: context.Background(), reason: reason}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD092", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							items, e := c.ConflictMarkers(ctx)
							if e != nil {
								t.Fatal(e)
							}
							if source == "<<<<<<<" {
								pre := compareConflictMarkers(ctx, c, items.Markers)
								if pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 0 || pre.Findings[0].Span.End() != 7 {
									t.Fatal("actual positive precondition", pre)
								}
							}
							gate := ".compareConflictMarkers.func1"
							if whole {
								gate = ".conflictMarkers.func1"
							}
							caller := &md092PublicationStop{Context: context.Background(), gate: gate, reason: reason}
							var target context.Context = caller
							if originating {
								origin.gate = gate
								target = context.Background()
							}
							var out engine.RuleOutcome
							if whole {
								out = conflictMarkers(target, c)
							} else {
								out = compareConflictMarkers(target, c, items.Markers)
							}
							reached := caller.reached
							if originating {
								reached = origin.reached
							}
							if !reached || !errors.Is(out.Err, reason) || out.Findings != nil || out.IncompleteReason != "" {
								t.Fatal(out, reached)
							}
							called = true
							return out
						}}})
						in, e := engine.NewInput("literal.md", []byte(source), md092Settings(), engine.DocumentContext{})
						if e != nil {
							t.Fatal(e)
						}
						_, e = r.Lint(origin, []engine.Input{in}, engine.RunOptions{})
						if e != nil || !called {
							t.Fatal(e, called)
						}
					})
				}
			}
		}
	}
}

func TestMD092InvalidUTF8Admission(t *testing.T) {
	s := md092Settings()
	off := false
	s["MD094"] = engine.RuleSetting{Enabled: &off}
	calls := 0
	regs := Registrations()
	for i := range regs {
		if regs[i].ID == "MD092" {
			regs[i].Check = func(context.Context, engine.RuleContext) engine.RuleOutcome {
				calls++
				t.Fatal("UTF8 gate bypassed")
				return engine.RuleOutcome{}
			}
		}
	}
	r := registry(t, regs)
	source := []byte{'<', 'd', 'i', 'v', '>', '\n', 0xff}
	before := append([]byte(nil), source...)
	in, err := engine.NewInput("invalid.md", source, s, engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil || len(out.Errors) != 1 || out.Errors[0].Category != result.IO || out.Errors[0].Code != "invalid-utf8" || out.Errors[0].Rule != "" || out.Errors[0].ByteOffset == nil || *out.Errors[0].ByteOffset != 6 || len(out.Diagnostics) != 0 || calls != 0 || !reflect.DeepEqual(source, before) {
		t.Fatal(out, err)
	}
}

func TestMD092AvailabilityAndStrictOptions(t *testing.T) {
	r := registry(t, Registrations())
	resolved, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil || !resolved["MD092"].Enabled || !resolved["MD092"].Available || len(resolved["MD092"].Options) != 0 {
		t.Fatal(resolved["MD092"], err)
	}
	for _, m := range engine.Catalog() {
		if m.ID == "MD092" && (!m.DefaultEnabled || m.Fix != engine.NoFix || !m.RequiresSyntax || m.UsesFrontmatter) {
			t.Fatal(m)
		}
	}
	for _, key := range []string{"unknown", "allowed_tags", "options", "style"} {
		s := md092Settings()
		off := false
		s["MD092"] = engine.RuleSetting{Enabled: &off, Options: engine.Options{key: nil}}
		if _, err := r.ResolveSettings(s, engine.ValidationProviders{}); err == nil {
			t.Fatal(key)
		}
	}
}

func TestMD092ComparisonRejectsLateFactsAtomically(t *testing.T) {
	for _, kind := range []string{"absent", "foreign", "spelling", "short", "order", "nonmaximal", "physical-prefix"} {
		t.Run(kind, func(t *testing.T) {
			called := false
			r := registry(t, []engine.Registration{{ID: "MD092", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				items, err := c.ConflictMarkers(ctx)
				if err != nil || len(items.Markers) != 2 {
					t.Fatal(items, err)
				}
				prefix := compareConflictMarkers(ctx, c, items.Markers[:1])
				if prefix.Err != nil || len(prefix.Findings) != 1 || prefix.Findings[0].Span.Start() != 0 || prefix.Findings[0].Span.End() != 7 {
					t.Fatal(prefix)
				}
				bad := items.Markers[1]
				switch kind {
				case "absent":
					bad = parser.Span{}
				case "foreign":
					bad, _ = parser.NewSource([]byte("<<<<<<<\n<<<<<<<< suffix")).Span(8, 16)
				case "spelling":
					bad, _ = c.Source().Span(8, 17)
				case "short":
					bad, _ = c.Source().Span(8, 14)
				case "order":
					bad = items.Markers[0]
				case "nonmaximal":
					bad, _ = c.Source().Span(8, 15)
				case "physical-prefix":
					bad, _ = c.Source().Span(9, 16)
				}
				items.Markers[1] = bad
				out := compareConflictMarkers(ctx, c, items.Markers)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				called = true
				return out
			}}})
			analyzeHeadings(t, r, "<<<<<<<\n<<<<<<<< suffix", md092Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if !called {
				t.Fatal("callback absent")
			}
		})
	}
}

func TestMD092SingleSharedCallbackAndFreshDocumentState(t *testing.T) {
	for _, name := range []string{"md087-used-hidden-open", "md087-unused-no-open", "md087-used-hidden-standalone-close", "md087-unknown-withheld-prefix-uncertain", "explicit-disabled"} {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(name+"/"+flavor, func(t *testing.T) {
				var tc md092Literal
				for _, x := range md092AllLiterals(t) {
					if x.Name == name {
						tc = x
					}
				}
				if tc.Name == "" {
					t.Fatal(name)
				}
				calls := 0
				regs := Registrations()
				for i := range regs {
					if regs[i].ID == "MD092" {
						actual := regs[i].Check
						regs[i].Check = func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome { calls++; return actual(ctx, c) }
					}
				}
				p := md092Plan(t, registry(t, regs), tc, flavor)
				in, err := p.NewInput("literal.md", []byte(tc.Source))
				if err != nil {
					t.Fatal(err)
				}
				out, err := p.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
				want := 1
				if tc.Expected.Evaluation == "not-demanded" {
					want = 0
				}
				if err != nil || calls != want || out.Complete != tc.Expected.Complete {
					t.Fatal(out, err, calls)
				}
			})
		}
	}
	r := registry(t, Registrations())
	s := md092Settings()
	a, _ := engine.NewInput("a.md", []byte("<<<<<<<"), s, engine.DocumentContext{})
	b, _ := engine.NewInput("b.md", []byte("======="), s, engine.DocumentContext{})
	out, err := r.Lint(context.Background(), []engine.Input{a, b}, engine.RunOptions{})
	if err != nil || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Path != "a.md" {
		t.Fatal(out, err)
	}
}

func TestMD092InspectFullSourceAndInactive(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, name := range []string{"published-cycle", "hidden-open-keeps-context", "explicit-disabled"} {
			for _, inactive := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/inactive-%v", flavor, name, inactive), func(t *testing.T) {
					var tc md092Literal
					for _, x := range md092Literals(t) {
						if x.Name == name {
							tc = x
						}
					}
					p := md092Plan(t, registry(t, Registrations()), tc, flavor)
					in, err := p.NewInput("literal.md", []byte(tc.Source))
					if err != nil {
						t.Fatal(err)
					}
					line := 3
					if name == "hidden-open-keeps-context" {
						line = 4
					}
					out, trace, err := p.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{ObserveInactive: inactive})
					if err != nil || !out.Complete || len(out.Errors) != 0 {
						t.Fatal(out, err)
					}
					want := len(tc.Expected.Findings)
					if len(out.Diagnostics) != want {
						t.Fatal(out.Diagnostics)
					}
					if want == 1 {
						d := out.Diagnostics[0]
						if d.Rule != "MD092" || d.ByteRange.End-d.ByteRange.Start != 7 || d.Range.Start.Line != line {
							t.Fatal(d)
						}
					}
					if name == "explicit-disabled" && inactive {
						found := false
						for _, h := range trace.Suppressed() {
							if h.Diagnostic.Rule == "MD092" {
								found = true
							}
						}
						if !found {
							t.Fatal(trace.Suppressed())
						}
					}
				})
			}
		}
	}
}

// Exhaustion is injected at the actual deferred outer gate after production
// evaluation. It uses the original allowance and no measured setup subtraction.
type md092FinalQuota struct {
	context.Context
	budget         *parser.OperationBudget
	kind           string
	armed, reached bool
}

func (c *md092FinalQuota) Err() error {
	if c.armed {
		var pcs [32]uintptr
		n := runtime.Callers(1, pcs[:])
		frames := runtime.CallersFrames(pcs[:n])
		for {
			f, more := frames.Next()
			if strings.HasSuffix(f.Function, ".conflictMarkers.func1") {
				c.reached = true
				if c.kind == "nodes" {
					_ = c.budget.ChargeNodes(c.budget.Limits().TotalNodes)
				} else {
					_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
				}
				break
			}
			if !more {
				break
			}
		}
	}
	return c.Context.Err()
}

func TestMD092DistinctOuterFinalStickyQuotaDiscard(t *testing.T) {
	for _, source := range []string{"", "clean", "```\n<<<<<<<\n```", "<<<<<<<", "<<<<<<< before\n\n<div>\n<x title=\""} {
		for _, kind := range []string{"work", "nodes"} {
			for _, originating := range []bool{false, true} {
				t.Run(fmt.Sprintf("%q/%s/origin-%v", source, kind, originating), func(t *testing.T) {
					b, _ := parser.NewOperationBudget(parser.DefaultLimits())
					origin := &md092FinalQuota{Context: context.Background(), budget: b, kind: kind}
					called := false
					r := registry(t, []engine.Registration{{ID: "MD092", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						pre := conflictMarkers(ctx, c)
						if pre.Err != nil {
							t.Fatal(pre)
						}
						if source == "<<<<<<<" && (len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 0 || pre.Findings[0].Span.End() != 7) {
							t.Fatal("positive precondition", pre)
						}
						caller := &md092FinalQuota{Context: context.Background(), budget: b, kind: kind, armed: true}
						var target context.Context = caller
						if originating {
							origin.armed = true
							target = context.Background()
						}
						out := conflictMarkers(target, c)
						var stop *parser.LimitExceeded
						reached := caller.reached
						if originating {
							reached = origin.reached
						}
						if !reached || !errors.As(out.Err, &stop) || out.Findings != nil || out.IncompleteReason != "" {
							t.Fatal(out, reached)
						}
						called = true
						return out
					}}})
					in, err := engine.NewInput("literal.md", []byte(source), md092Settings(), engine.DocumentContext{})
					if err != nil {
						t.Fatal(err)
					}
					out, err := r.Lint(origin, []engine.Input{in}, engine.RunOptions{Budget: b})
					if err != nil || !called || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) == 0 {
						t.Fatal(out, err, called)
					}
				})
			}
		}
	}
}

func TestMD092MD087InterruptedSingleObservationRemainsUncertain(t *testing.T) {
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	calls := 0
	regs := Registrations()
	for i := range regs {
		if regs[i].ID == "MD092" {
			actual := regs[i].Check
			regs[i].Check = func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				calls++
				child, cancel := context.WithCancel(ctx)
				cancel()
				return actual(child, c)
			}
		}
	}
	s := md092Settings()
	on := true
	s["MD087"] = engine.RuleSetting{Enabled: &on}
	in, err := engine.NewInput("literal.md", []byte("<!-- mdchk:disable MD092 -->\n<<<<<<<\n"), s, engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := registry(t, regs).Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Budget: b})
	if err != nil || calls != 1 || out.Complete || len(out.Diagnostics)+len(out.SuppressedDiagnostics) != 0 || len(out.Interruptions) == 0 {
		t.Fatal(out, err, calls)
	}
	var decisions []engine.SuppressionDecision
	for _, record := range out.AnalysisRecords() {
		if a, ok := record.(engine.DirectiveAnalysis); ok {
			rows, e := a.SuppressionDecisions(context.Background(), b)
			if e != nil {
				t.Fatal(e)
			}
			decisions = append(decisions, rows...)
		}
	}
	if len(decisions) != 1 || decisions[0].Rule != "MD092" || decisions[0].Reason != "suppression-uncertain" {
		t.Fatal(decisions)
	}
}
