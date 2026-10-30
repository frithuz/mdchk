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

type md091Finding struct {
	Rule, Message, Marker string
	ByteRange             result.ByteRange `json:"byte_range"`
	Range                 result.Range
}

type md091Literal struct {
	Name, Source  string
	Bytes         int    `json:"source_bytes"`
	SHA           string `json:"source_sha256"`
	Configuration struct {
		Enabled     bool
		MD087       bool `json:"md087_enabled"`
		Allow       bool `json:"allow_directives"`
		Frontmatter string
	}
	Expected struct {
		Complete             bool
		Evaluation           string `json:"md091_evaluation"`
		Findings, Suppressed []md091Finding
		MD087                []md091Finding `json:"md087_findings"`
		Decisions            []struct {
			Owner, Effect int
			Rule, Reason  string
			Token         result.ByteRange
			Interval      struct {
				Start, End int
				EOF        bool `json:"includes_final_eof"`
			}
		} `json:"audit_decisions"`
	}
}

func md091Literals(t *testing.T) []md091Literal {
	t.Helper()
	b, err := os.ReadFile("testdata/md091.json")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "2110047ce76969edbe4970529358d4caef98a4d0021bcff824db3426f013f05d" {
		t.Fatal("root literal oracle identity", err)
	}
	var oracle struct{ Cases []md091Literal }
	if err = json.Unmarshal(b, &oracle); err != nil || len(oracle.Cases) != 111 {
		t.Fatal(err)
	}
	return oracle.Cases
}

func md091Settings() engine.Settings {
	s := engine.Settings{}
	off, on := false, true
	for _, m := range engine.Catalog() {
		s[m.ID] = engine.RuleSetting{Enabled: &off}
	}
	s["MD091"] = engine.RuleSetting{Enabled: &on}
	return s
}

func md091Plan(t *testing.T, r *engine.Registry, tc md091Literal, flavor string) *engine.ExecutionPlan {
	t.Helper()
	s := md091Settings()
	s["MD091"] = engine.RuleSetting{Enabled: &tc.Configuration.Enabled, AllowDirectives: &tc.Configuration.Allow}
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

func md091AssertDiagnostic(t *testing.T, source string, got result.Diagnostic, want md091Finding) {
	t.Helper()
	if got.Rule != want.Rule || got.Message != want.Message || got.ByteRange != want.ByteRange || got.Range != want.Range || got.FixAvailable || got.Snapshot != (result.Snapshot{View: "original", SizeBytes: len(source), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}) {
		t.Fatal(got, want)
	}
	if want.ByteRange.Start < 0 || want.ByteRange.End > len(source) || want.ByteRange.End <= want.ByteRange.Start || source[want.ByteRange.Start:want.ByteRange.End] != want.Marker {
		t.Fatal("invalid oracle range", want)
	}
}

func TestMD091FrozenProductionOracle(t *testing.T) {
	md091CheckProductionLiterals(t, md091Literals(t))
}

func md091CheckProductionLiterals(t *testing.T, cases []md091Literal) {
	t.Helper()
	r := registry(t, Registrations())
	for _, tc := range cases {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
					t.Fatal("source identity")
				}
				p := md091Plan(t, r, tc, flavor)
				original := []byte(tc.Source)
				in, err := p.NewInput("literal.md", original)
				if err != nil {
					t.Fatal(err)
				}
				token := in.Source().Token()
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				out, err := p.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Budget: budget})
				if err != nil || out.Complete != tc.Expected.Complete || len(out.Errors) != 0 || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 || string(original) != tc.Source || string(in.Source().Bytes()) != tc.Source || token != in.Source().Token() {
					t.Fatal(out, err)
				}
				want := append(append([]md091Finding{}, tc.Expected.Findings...), tc.Expected.MD087...)
				sort.Slice(want, func(i, j int) bool { return want[i].ByteRange.Start < want[j].ByteRange.Start })
				if len(out.Diagnostics) != len(want) {
					t.Fatal(out.Diagnostics, want)
				}
				for i, d := range out.Diagnostics {
					md091AssertDiagnostic(t, tc.Source, d, want[i])
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
				if len(hidden) != len(tc.Expected.Suppressed) || len(decisions) != len(tc.Expected.Decisions) {
					t.Fatal(hidden, decisions, tc.Expected)
				}
				for i, h := range hidden {
					md091AssertDiagnostic(t, tc.Source, h.Diagnostic, tc.Expected.Suppressed[i])
					if h.Owner != 0 || h.Token.SourceToken() != token {
						t.Fatal(h)
					}
				}
				for i, d := range decisions {
					w := tc.Expected.Decisions[i]
					if d.Owner != w.Owner || d.Effect != w.Effect || d.Rule != w.Rule || d.Reason != w.Reason || d.Token.SourceToken() != token || d.Token.Start() != w.Token.Start || d.Token.End() != w.Token.End || d.Interval.Start != w.Interval.Start || d.Interval.End != w.Interval.End || d.Interval.Enabled || d.Interval.Owner != w.Owner || d.Interval.Token != d.Token || w.Interval.EOF && d.Interval.End != len(tc.Source) {
						t.Fatal(d, w)
					}
				}
				if !tc.Expected.Complete {
					found := false
					for _, c := range out.Coverage {
						if c.Rule == "MD091" && c.Reason == result.ContextUnavailable && c.Message == "HTML Markdown candidate context is unavailable; the complete MD091 inventory is withheld." {
							found = true
						}
					}
					if !found {
						t.Fatal(out.Coverage)
					}
				}
			})
		}
	}
}

func TestMD091RealSharedDemandAndInactiveInspection(t *testing.T) {
	for _, name := range []string{"md087-real-hidden-finding-used", "md087-real-complete-absence-unused", "md087-atomic-withheld-prefix-uncertain", "md087-configured-off-incapable-no-callback", "default-disabled"} {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(name+"/"+flavor, func(t *testing.T) {
				var tc md091Literal
				for _, x := range md091Literals(t) {
					if x.Name == name {
						tc = x
					}
				}
				for _, inactive := range []bool{false, true} {
					calls := 0
					registrations := Registrations()
					for i := range registrations {
						if registrations[i].ID == "MD091" {
							actual := registrations[i].Check
							registrations[i].Check = func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome { calls++; return actual(ctx, c) }
						}
					}
					p := md091Plan(t, registry(t, registrations), tc, flavor)
					in, err := p.NewInput("literal.md", []byte(tc.Source))
					if err != nil {
						t.Fatal(err)
					}
					out, trace, err := p.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{ObserveInactive: inactive})
					want := 1
					if !inactive && tc.Expected.Evaluation == "not-requested" {
						want = 0
					}
					if err != nil || calls != want || out.Complete != tc.Expected.Complete {
						t.Fatal(out, err, calls, want)
					}
					// Independent observation may retain other inactive production
					// rules. Ordinary visible and hidden output must still match the
					// complete oracle inventory, rather than filtering unexpected rules.
					visible := append(append([]md091Finding{}, tc.Expected.Findings...), tc.Expected.MD087...)
					sort.Slice(visible, func(i, j int) bool { return visible[i].ByteRange.Start < visible[j].ByteRange.Start })
					if len(out.Errors) != 0 || len(out.Diagnostics) != len(visible) || len(out.SuppressedDiagnostics) != len(tc.Expected.Suppressed) {
						t.Fatal(out)
					}
					for i, d := range out.Diagnostics {
						md091AssertDiagnostic(t, tc.Source, d, visible[i])
					}
					for i, raw := range out.SuppressedDiagnostics {
						var hidden result.SuppressedDiagnostic
						if e := json.Unmarshal(raw, &hidden); e != nil {
							t.Fatal(e)
						}
						md091AssertDiagnostic(t, tc.Source, hidden.Diagnostic, tc.Expected.Suppressed[i])
					}
					// trace.Suppressed contains independently observed inactive
					// callbacks. Active target findings hidden by directives belong
					// to the ordinary suppressed result/analysis records above.
					var target []engine.DirectiveSuppression
					for _, hidden := range trace.Suppressed() {
						if hidden.Diagnostic.Rule == "MD091" {
							target = append(target, hidden)
						}
					}
					wantObserved := 0
					if inactive && tc.Expected.Evaluation == "not-requested" {
						wantObserved = 1
					}
					if len(target) != wantObserved {
						t.Fatal(target, wantObserved, trace.Suppressed())
					}
					if wantObserved == 1 {
						// Forced observation does not change configured capability.
						// Marker expectations come from an identical literal in the
						// frozen root oracle, not an observed inactive inventory.
						var expected md091Finding
						for _, literal := range md091Literals(t) {
							if name == "default-disabled" && literal.Name == "basic-strong" {
								if literal.Source != tc.Source {
									t.Fatal("literal association")
								}
								expected = literal.Expected.Findings[0]
							}
							if name == "md087-configured-off-incapable-no-callback" && literal.Name == "md087-real-hidden-finding-used" {
								if literal.Source != tc.Source {
									t.Fatal("literal association")
								}
								expected = literal.Expected.Suppressed[0]
							}
						}
						md091AssertDiagnostic(t, tc.Source, target[0].Diagnostic, expected)
					}
				}
			})
		}
	}
}

func TestMD091AvailabilityAndStrictOptions(t *testing.T) {
	r := registry(t, Registrations())
	resolved, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved["MD091"].Enabled || !resolved["MD091"].Available || len(resolved["MD091"].Options) != 0 {
		t.Fatal(resolved["MD091"])
	}
	for _, m := range engine.Catalog() {
		if m.ID == "MD091" && (m.DefaultEnabled || m.Fix != engine.NoFix || !m.RequiresSyntax || m.UsesFrontmatter) {
			t.Fatal(m)
		}
	}
	for _, key := range []string{"unknown", "allowed_tags", "options", "style"} {
		s := md091Settings()
		off := false
		s["MD091"] = engine.RuleSetting{Enabled: &off, Options: engine.Options{key: nil}}
		if _, err := r.ResolveSettings(s, engine.ValidationProviders{}); err == nil {
			t.Fatal(key)
		}
	}
}

func TestMD091ComparisonRejectsLateFactsAtomically(t *testing.T) {
	for _, kind := range []string{"absent", "foreign", "spelling", "extent", "kind", "order"} {
		t.Run(kind, func(t *testing.T) {
			called := false
			r := registry(t, []engine.Registration{{ID: "MD091", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				items, err := c.HTMLMarkdownCandidates(ctx)
				if err != nil || len(items.Candidates) != 2 {
					t.Fatal(items, err)
				}
				prefix := compareHTMLMarkdown(ctx, c, items.Candidates[:1])
				if prefix.Err != nil || len(prefix.Findings) != 1 || prefix.Findings[0].Span.Start() != 6 || prefix.Findings[0].Span.End() != 8 {
					t.Fatal("positive prefix precondition", prefix)
				}
				bad := items.Candidates[1]
				switch kind {
				case "absent":
					bad.Marker = parser.Span{}
				case "foreign":
					bad.Marker, _ = parser.NewSource([]byte("<div>\n**a** **b**")).Span(12, 14)
				case "spelling":
					bad.Marker, _ = c.Source().Span(13, 15)
				case "extent":
					bad.Extent = items.Candidates[0].Extent
				case "kind":
					bad.Kind = "unknown"
				case "order":
					bad = items.Candidates[0]
				}
				items.Candidates[1] = bad
				out := compareHTMLMarkdown(ctx, c, items.Candidates)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				called = true
				return out
			}}})
			analyzeHeadings(t, r, "<div>\n**a** **b**", md091Settings(), engine.DocumentContext{}, engine.RunOptions{})
			if !called {
				t.Fatal("callback absent")
			}
		})
	}
}

func TestMD091OriginalStickyCallbackControls(t *testing.T) {
	for _, source := range []string{"", "clean", "`code`", "<pre>**x**", "<div>**x**", "<div>**x** <x a=\""} {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			t.Run(source+"/"+kind, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					b, _ := parser.NewOperationBudget(parser.DefaultLimits())
					called := false
					r := registry(t, []engine.Registration{{ID: "MD091", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
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
							out = markdownInsideHTML(ctx, c)
							var limit *parser.LimitExceeded
							if !errors.As(out.Err, &limit) || out.Findings != nil || out.IncompleteReason != "" {
								t.Fatal(out)
							}
						}
						called = true
						return out
					}}})
					analyzeHeadings(t, r, source, md091Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: b})
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
type md091PublicationStop struct {
	context.Context
	gate    string
	reason  error
	reached bool
}

func (c *md091PublicationStop) Err() error {
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
func TestMD091ComparisonAndDistinctOuterFinalControls(t *testing.T) {
	for _, source := range []string{"", "clean", "`code`", "<pre>**x**", "<div>**x**", "<div>**x** <x a=\""} {
		for _, whole := range []bool{false, true} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%q/whole-%v/origin-%v/%v", source, whole, originating, reason), func(t *testing.T) {
						origin := &md091PublicationStop{Context: context.Background(), reason: reason}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD091", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							items, e := c.HTMLMarkdownCandidates(ctx)
							if e != nil {
								t.Fatal(e)
							}
							if source == "<div>**x**" {
								pre := compareHTMLMarkdown(ctx, c, items.Candidates)
								if pre.Err != nil || len(pre.Findings) != 1 || pre.Findings[0].Span.Start() != 5 || pre.Findings[0].Span.End() != 7 {
									t.Fatal("actual positive precondition", pre)
								}
							}
							gate := ".compareHTMLMarkdown.func1"
							if whole {
								gate = ".markdownInsideHTML.func1"
							}
							caller := &md091PublicationStop{Context: context.Background(), gate: gate, reason: reason}
							var target context.Context = caller
							if originating {
								origin.gate = gate
								target = context.Background()
							}
							var out engine.RuleOutcome
							if whole {
								out = markdownInsideHTML(target, c)
							} else {
								out = compareHTMLMarkdown(target, c, items.Candidates)
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
						in, e := engine.NewInput("literal.md", []byte(source), md091Settings(), engine.DocumentContext{})
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

func TestMD091InvalidUTF8Admission(t *testing.T) {
	s := md091Settings()
	off := false
	s["MD094"] = engine.RuleSetting{Enabled: &off}
	calls := 0
	regs := Registrations()
	for i := range regs {
		if regs[i].ID == "MD091" {
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
