package rules

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

type md014Literal struct {
	Name, Source  string
	Bytes         int    `json:"source_bytes"`
	SHA           string `json:"source_sha256"`
	Configuration struct {
		Enabled          bool
		Omitted          bool `json:"enabled_omitted"`
		Empty            bool `json:"empty_options"`
		MD087            bool `json:"md087_enabled"`
		Directives       bool `json:"allow_directives"`
		Frontmatter      string
		Languages        []string
		LanguagesOmitted bool `json:"languages_omitted"`
	}
	Expected struct {
		Complete             bool
		Evaluation           string `json:"md014_evaluation"`
		Findings, Suppressed []md092Finding
		MD087                []md092Finding       `json:"md087_findings"`
		Raw                  []md092AuditDecision `json:"raw_decisions"`
		Requested            []md092AuditDecision `json:"requested_audit_decisions"`
		Coverage             []result.Coverage
		LintExit             int  `json:"isolated_lint_exit"`
		InspectExit          int  `json:"completed_inspect_exit"`
		Unchanged            bool `json:"source_unchanged"`
		NoCorrections        bool `json:"no_md014_corrections"`
	}
}

func md014Literals(t *testing.T) []md014Literal {
	t.Helper()
	b, err := os.ReadFile("testdata/md014-v2.json")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "b06f76546386295e38b4dc71b871d21398fc25b5e647ae1093ccdb1e5e787431" {
		t.Fatal("immutable root oracle v2", err)
	}
	var oracle struct{ Cases []md014Literal }
	if err = json.Unmarshal(b, &oracle); err != nil || len(oracle.Cases) != 134 {
		t.Fatal(err, len(oracle.Cases))
	}
	return oracle.Cases
}

func md014Settings() engine.Settings {
	s := md092Settings()
	off, on := false, true
	s["MD092"] = engine.RuleSetting{Enabled: &off}
	s["MD014"] = engine.RuleSetting{Enabled: &on}
	return s
}

func md014Plan(t *testing.T, r *engine.Registry, tc md014Literal, flavor string) *engine.ExecutionPlan {
	t.Helper()
	s := md014Settings()
	setting := engine.RuleSetting{Enabled: &tc.Configuration.Enabled, AllowDirectives: &tc.Configuration.Directives}
	if tc.Configuration.Omitted {
		setting.Enabled = nil
	}
	if tc.Configuration.Empty {
		setting.Options = engine.Options{}
	}
	if !tc.Configuration.LanguagesOmitted {
		setting.Options = engine.Options{"languages": tc.Configuration.Languages}
	}
	s["MD014"] = setting
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

func TestMD014FrozenProductionOracle(t *testing.T) {
	for _, tc := range md014Literals(t) {
		for _, flavor := range []string{"commonmark", "gfm"} {
			for _, inspect := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/inspect-%v", tc.Name, flavor, inspect), func(t *testing.T) {
					if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA || !tc.Expected.Unchanged || !tc.Expected.NoCorrections || tc.Expected.InspectExit != 0 {
						t.Fatal("oracle identity/scope", tc)
					}
					p := md014Plan(t, registry(t, Registrations()), tc, flavor)
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
					if err != nil || out.Complete != tc.Expected.Complete || len(out.Errors)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes)+len(out.Interruptions)+len(out.Omissions) != 0 || string(original) != tc.Source || string(in.Source().Bytes()) != tc.Source || in.Source().Token() != token {
						t.Fatal(out, err)
					}
					if len(out.Files) != 1 || out.Files[0].WriteStatus != "not-requested" || len(out.Files[0].ChangeIDs) != 0 || !reflect.DeepEqual(out.CheckedFiles, []string{"literal.md"}) {
						t.Fatal(out.Files)
					}
					status := "clean"
					if tc.Expected.LintExit == 10 {
						status = "violations"
					}
					if tc.Expected.LintExit == 15 {
						status = "incomplete"
					}
					if out.Status != status {
						t.Fatal("engine outcome", out.Status, status)
					}
					want := append(append([]md092Finding{}, tc.Expected.Findings...), tc.Expected.MD087...)
					sort.Slice(want, func(i, j int) bool {
						if want[i].ByteRange.Start != want[j].ByteRange.Start {
							return want[i].ByteRange.Start < want[j].ByteRange.Start
						}
						return want[i].Rule < want[j].Rule
					})
					if len(out.Diagnostics) != len(want) {
						t.Fatal(out.Diagnostics, want)
					}
					for i, d := range out.Diagnostics {
						if d.Path != "literal.md" {
							t.Fatal(d)
						}
						md092AssertDiagnostic(t, tc.Source, d, want[i])
					}
					if len(out.SuppressedDiagnostics) != len(tc.Expected.Suppressed) {
						t.Fatal(out.SuppressedDiagnostics, tc.Expected.Suppressed)
					}
					for i, raw := range out.SuppressedDiagnostics {
						var h result.SuppressedDiagnostic
						if e := json.Unmarshal(raw, &h); e != nil {
							t.Fatal(e)
						}
						if h.Diagnostic.Path != "literal.md" {
							t.Fatal(h)
						}
						md092AssertDiagnostic(t, tc.Source, h.Diagnostic, tc.Expected.Suppressed[i])
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
						t.Fatal(hidden, tc.Expected.Suppressed)
					}
					for i, h := range hidden {
						md092AssertDiagnostic(t, tc.Source, h.Diagnostic, tc.Expected.Suppressed[i])
						joined := false
						for _, d := range decisions {
							if d.Owner == h.Owner && d.Rule == h.Diagnostic.Rule && d.Token == h.Token && h.Diagnostic.ByteRange.Start >= d.Interval.Start && h.Diagnostic.ByteRange.Start < d.Interval.End {
								joined = true
							}
						}
						if !joined {
							t.Fatal("hidden original owner join", h, decisions)
						}
					}
					validated := md092AssertRawDecisions(t, in.Source(), decisions, tc.Expected.Raw)
					var requested []md092AuditDecision
					for _, d := range validated {
						if d.Reason != "suppression-not-requested" {
							requested = append(requested, d)
						}
					}
					if !md092EqualDecisions(requested, tc.Expected.Requested) {
						t.Fatal("requested audit inventory", requested, tc.Expected.Requested)
					}
					coverage := append([]result.Coverage{}, out.Coverage...)
					for i := range coverage {
						coverage[i].ID = ""
					}
					if len(coverage) != len(tc.Expected.Coverage) {
						t.Fatal(coverage, tc.Expected.Coverage)
					}
					for i := range coverage {
						if !reflect.DeepEqual(coverage[i], tc.Expected.Coverage[i]) {
							t.Fatal(coverage, tc.Expected.Coverage)
						}
					}
					if inspect {
						rows, e := trace.SuppressionDecisions(context.Background(), budget)
						if e != nil {
							t.Fatal(e)
						}
						md092AssertRawDecisions(t, in.Source(), rows, tc.Expected.Raw)
						found := false
						for _, ev := range trace.Evaluations() {
							if ev.Rule == "MD014" {
								found = true
								switch tc.Expected.Evaluation {
								case "not-demanded":
									if ev.Active != "skipped" || ev.Observational != "not-requested" || len(ev.ActiveCoverage)+len(ev.ObservationalCoverage) != 0 {
										t.Fatal(ev)
									}
								case "complete":
									if ev.Active != "complete" && ev.Observational != "complete" {
										t.Fatal(ev)
									}
									spans := append(append([]parser.Span{}, ev.ActiveCoverage...), ev.ObservationalCoverage...)
									if len(spans) != 1 || spans[0].SourceToken() != token || spans[0].End() != len(tc.Source) {
										t.Fatal(ev)
									}
								case "unavailable":
									if ev.Active != "partial" && ev.Observational != "partial" || len(ev.ActiveCoverage)+len(ev.ObservationalCoverage) != 0 {
										t.Fatal(ev)
									}
								default:
									t.Fatal("unknown expected evaluation", tc.Expected.Evaluation)
								}
							}
						}
						if !found {
							t.Fatal("MD014 evaluation absent")
						}
					}
				})
			}
		}
	}
}

func TestMD014AvailabilityAndOptionOwnership(t *testing.T) {
	r := registry(t, Registrations())
	s, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil || !s["MD014"].Enabled || !s["MD014"].Available || !reflect.DeepEqual(s["MD014"].Options["languages"], []string{"sh", "shell", "bash", "zsh", "console"}) {
		t.Fatal(s["MD014"], err)
	}
	s["MD014"].Options["languages"].([]string)[0] = "changed"
	before := engine.Catalog()
	for _, m := range before {
		if m.ID == "MD014" {
			if !m.DefaultEnabled || !m.RequiresSyntax || m.Fix != engine.PlannedFix || m.UsesFrontmatter {
				t.Fatal(m)
			}
			m.Options["languages"].Default.([]string)[0] = "changed"
		}
	}
	after, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil || after["MD014"].Options["languages"].([]string)[0] != "sh" {
		t.Fatal(after, err)
	}
}

func TestMD014SharedObservationAndInactiveDemand(t *testing.T) {
	for _, name := range []string{"md087-used-hidden-prompt", "md087-unused-mixed-output", "md087-unknown-withheld-prefix-uncertain", "configured-off-not-demanded"} {
		for _, inactive := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/inactive-%v", name, inactive), func(t *testing.T) {
				var tc md014Literal
				for _, x := range md014Literals(t) {
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
					if regs[i].ID == "MD014" {
						actual := regs[i].Check
						regs[i].Check = func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome { calls++; return actual(ctx, c) }
					}
				}
				p := md014Plan(t, registry(t, regs), tc, "gfm")
				in, err := p.NewInput("literal.md", []byte(tc.Source))
				if err != nil {
					t.Fatal(err)
				}
				out, trace, err := p.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{ObserveInactive: inactive})
				want := 1
				if name == "configured-off-not-demanded" && !inactive {
					want = 0
				}
				if err != nil || calls != want || out.Complete != tc.Expected.Complete {
					t.Fatal(out, err, calls)
				}
				if name == "configured-off-not-demanded" && inactive {
					count := 0
					for _, h := range trace.Suppressed() {
						if h.Diagnostic.Rule == "MD014" {
							count++
							if h.Diagnostic.ByteRange != (result.ByteRange{Start: 6, End: 7}) || h.Diagnostic.FixAvailable {
								t.Fatal(h)
							}
						}
					}
					if count != 1 {
						t.Fatal(trace.Suppressed())
					}
				}
			})
		}
	}
}
