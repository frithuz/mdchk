package rules

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

type md051Literal struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Frontmatter  string
	Findings     []struct {
		Bytes, Start, End [2]int
		Expected, Message string
	}
}

func md051Literals(t *testing.T) []md051Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md051.json")
	if e != nil {
		t.Fatal(e)
	}
	var v struct{ Cases []md051Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) == 0 {
		t.Fatal(e)
	}
	return v.Cases
}
func md051Settings() engine.Settings {
	s := settings()
	for id := range s {
		off := false
		s[id] = engine.RuleSetting{Enabled: &off}
	}
	on := true
	s["MD051"] = engine.RuleSetting{Enabled: &on}
	return s
}
func TestMD051AnalysisFixtures(t *testing.T) {
	for _, tc := range md051Literals(t) {
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
					t.Fatal("source identity")
				}
				dc := engine.DocumentContext{Flavor: flavor}
				if tc.Frontmatter != "" {
					var e error
					dc.Frontmatter, e = parser.NewFrontmatterOptions(true, []string{tc.Frontmatter})
					if e != nil {
						t.Fatal(e)
					}
				}
				selection, e := registry(t, Registrations()).SelectRules(md051Settings(), engine.ValidationProviders{})
				if e != nil {
					t.Fatal(e)
				}
				plan, e := selection.Plan(dc, nil)
				if e != nil {
					t.Fatal(e)
				}
				original := []byte(tc.Source)
				input, e := plan.NewInput("literal.md", original)
				if e != nil {
					t.Fatal(e)
				}
				var previous []result.Diagnostic
				for pass := 0; pass < 2; pass++ {
					out, trace, e := plan.Inspect(context.Background(), input, engine.RunOptions{}, engine.InspectionOptions{})
					if e != nil || !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes)+len(out.SuppressedDiagnostics) != 0 || len(out.Diagnostics) != len(tc.Findings) || string(original) != tc.Source || string(input.Source().Bytes()) != tc.Source {
						t.Fatal(out, e, tc.Findings)
					}
					if len(out.Files) != 1 || out.Files[0].WriteStatus != "not-requested" {
						t.Fatal(out.Files)
					}
					if pass == 1 && !reflect.DeepEqual(previous, out.Diagnostics) {
						t.Fatal("non-deterministic findings")
					}
					previous = out.Diagnostics
					for i, d := range out.Diagnostics {
						w := tc.Findings[i]
						if d.Rule != "MD051" || d.Message != w.Message || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: w.Bytes[0], End: w.Bytes[1]}) || d.Range != (result.Range{Start: result.Position{Line: w.Start[0], Column: w.Start[1]}, End: result.Position{Line: w.End[0], Column: w.End[1]}}) || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}) || tc.Source[w.Bytes[0]:w.Bytes[1]] != w.Expected {
							t.Fatal(d, w)
						}
					}
					found := false
					for _, ev := range trace.Evaluations() {
						if ev.Rule == "MD051" {
							found = true
							if ev.Active != "complete" {
								t.Fatal(ev)
							}
						}
					}
					if !found {
						t.Fatal("evaluation missing")
					}
				}
			})
		}
	}
}
func TestMD051AvailabilityDefaultsAndOptions(t *testing.T) {
	r := registry(t, Registrations())
	s, e := r.ResolveSettings(nil, engine.ValidationProviders{})
	if e != nil || !s["MD051"].Available || !s["MD051"].Enabled || len(s["MD051"].Options) != 1 || s["MD051"].Options["cross_file"] != false {
		t.Fatal(s, e)
	}
	for _, meta := range r.Metadata() {
		if meta.ID == "MD051" && (meta.Fix != engine.NoFix || !meta.RequiresSyntax || len(meta.Options) != 1) {
			t.Fatal(meta)
		}
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, empty := range []bool{false, true} {
			settings := md051Settings()
			delete(settings, "MD051")
			if empty {
				settings["MD051"] = engine.RuleSetting{Options: engine.Options{}}
			}
			out := analyzeHeadings(t, r, "[x](#nope)", settings, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != 1 || len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(out)
			}
		}
	}
	for _, enabled := range []bool{false, true} {
		settings := md051Settings()
		settings["MD051"] = engine.RuleSetting{Enabled: &enabled, Options: engine.Options{"anchor_style": "github"}}
		if _, e = r.ResolveSettings(settings, engine.ValidationProviders{}); e == nil {
			t.Fatal("unknown option accepted")
		}
	}
}
func TestMD051DirectivesAndInactiveObservation(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"suppressed", "denied", "inactive", "enable", "enable-denied", "protected"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				s := md051Settings()
				enabled := mode != "inactive" && mode != "enable" && mode != "enable-denied"
				allow := mode != "denied" && mode != "enable-denied"
				s["MD051"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow}
				source := "<!-- mdchk:disable MD051 -->\n[x](#nope)"
				if mode == "inactive" {
					source = "[x](#nope)"
				}
				if mode == "enable" || mode == "enable-denied" {
					source = "<!-- mdchk:enable MD051 -->\n[x](#nope)"
				}
				if mode == "protected" {
					source = "```md\n<!-- mdchk:disable MD051 -->\n[x](#nope)\n```\n\n[x](#nope)"
				}
				selection, e := registry(t, Registrations()).SelectRules(s, engine.ValidationProviders{})
				if e != nil {
					t.Fatal(e)
				}
				plan, e := selection.Plan(engine.DocumentContext{Flavor: flavor}, nil)
				if e != nil {
					t.Fatal(e)
				}
				in, e := plan.NewInput("literal.md", []byte(source))
				if e != nil {
					t.Fatal(e)
				}
				out, trace, e := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{ObserveInactive: mode == "inactive"})
				wantVisible := 0
				if mode == "denied" || mode == "enable" || mode == "protected" {
					wantVisible = 1
				}
				if e != nil || !out.Complete || len(out.Diagnostics) != wantVisible || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents) != 0 {
					t.Fatal(out, e)
				}
				wantHidden := 0
				if mode == "suppressed" {
					wantHidden = 1
				}
				if len(out.SuppressedDiagnostics) != wantHidden {
					t.Fatal(out)
				}
				if mode == "inactive" {
					found := 0
					for _, v := range trace.Suppressed() {
						if v.Diagnostic.Rule == "MD051" {
							found++
							if v.Diagnostic.ByteRange != (result.ByteRange{Start: 4, End: 9}) {
								t.Fatal(v)
							}
						}
					}
					if found != 1 {
						t.Fatal(trace.Suppressed())
					}
				}
				if mode == "protected" && len(trace.Directives().Candidates()) != 0 {
					t.Fatal("code directive executed")
				}
			})
		}
	}
}
func TestMD051UnknownContextWithholdsAllFindings(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		source := "[x](#nope)\n\n<div>\n<x title=\"\n\n[later](#nope)"
		out := analyzeHeadings(t, registry(t, Registrations()), source, md051Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
		if out.Complete || len(out.Diagnostics)+len(out.Errors) != 0 || len(out.Coverage) != 1 || out.Coverage[0].Reason != result.ContextUnavailable {
			t.Fatal(out)
		}
	}
}

func TestMD051LongComponentsWithinDefaultBudget(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, destination := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/destination-%v", flavor, destination), func(t *testing.T) {
				body := strings.Repeat("x", 200000)
				source := "[" + body + "](#nope)"
				want := 1
				if destination {
					source = "[x](#" + body + ")"
				}
				out := analyzeHeadings(t, registry(t, Registrations()), source, md051Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Diagnostics) != want || len(out.Errors)+len(out.Coverage)+len(out.Interruptions) != 0 {
					t.Fatal(out)
				}
			})
		}
	}
}

func TestMD051MissingDestinationSuppressionAudit(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, row := range []struct {
			name, source, reason   string
			visible, hidden, audit int
		}{
			{"after", "<!-- mdchk:disable MD051 -->\n[x](#nope)", "suppression-used", 0, 1, 0},
			{"before", "[x](#nope)\n<!-- mdchk:disable MD051 -->", "suppression-unused", 1, 0, 1},
			{"re-enabled", "<!-- mdchk:disable MD051 -->\n[x](#nope)\n<!-- mdchk:enable MD051 -->\n[y](#nope)", "suppression-used", 1, 1, 0},
		} {
			t.Run(flavor+"/"+row.name, func(t *testing.T) {
				r := registry(t, Registrations())
				s := md051Settings()
				on := true
				s["MD087"] = engine.RuleSetting{Enabled: &on}
				in, e := engine.NewInput("point.md", []byte(row.source), s, engine.DocumentContext{Flavor: flavor})
				if e != nil {
					t.Fatal(e)
				}
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				out, e := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Budget: budget})
				if e != nil || !out.Complete || len(out.Diagnostics) != row.visible+row.audit || len(out.SuppressedDiagnostics) != row.hidden || len(out.Errors)+len(out.Coverage)+len(out.Changes) != 0 {
					t.Fatal(out, e)
				}
				visible, audit := 0, 0
				for _, d := range out.Diagnostics {
					if d.Rule == "MD051" {
						visible++
						if d.ByteRange.End-d.ByteRange.Start != 5 {
							t.Fatal(d)
						}
					} else if d.Rule == "MD087" {
						audit++
					} else {
						t.Fatal(d)
					}
				}
				if visible != row.visible || audit != row.audit {
					t.Fatal(out.Diagnostics)
				}
				decisions := 0
				for _, record := range out.AnalysisRecords() {
					if a, ok := record.(engine.DirectiveAnalysis); ok {
						ds, e := a.SuppressionDecisions(context.Background(), budget)
						if e != nil || len(ds) != 1 || ds[0].Rule != "MD051" || ds[0].Reason != row.reason {
							t.Fatal(ds, e)
						}
						decisions++
					}
				}
				if decisions != 1 {
					t.Fatal("audit record missing", decisions)
				}
			})
		}
	}
}

func TestMD051CrossFileOptionAndAnchorStyle(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, row := range []struct {
			name, source, style string
			crossFile           bool
			findings, coverage  int
		}{
			{"off-skips-other-documents", "[x](other.md#a)\n", "github", false, 0, 0},
			{"on-without-context-is-incomplete", "[x](other.md#a)\n", "github", true, 0, 1},
			{"on-same-document-complete", "# A\n\n[x](#a) [y](#b)\n", "github", true, 1, 0},
			{"on-remote-complete", "[x](https://example.test/#a)\n", "github", true, 0, 0},
			{"on-without-fragment-complete", "[x](other.md)\n", "github", true, 0, 0},
			{"on-empty-fragment-complete", "[x](other.md#) [y](other.md?q=1#)\n", "github", true, 0, 0},
			{"on-single-letter-prefix", "[x](C:#x)\n", "github", true, 0, 1},
			{"on-duplicate-id-resolves-once", "<a id=\"d\"></a><a id=\"d\"></a>\n\n[x](#d)\n", "github", true, 0, 0},
			{"on-both", "[x](other.md#a) [y](#b)\n", "github", true, 1, 1},
			{"explicit-ignores-generated", "# A\n\n[x](#a)\n", "explicit", false, 1, 0},
			{"explicit-accepts-html-id", "<a id=\"a\"></a>\n\n[x](#a)\n", "explicit", false, 0, 0},
		} {
			t.Run(flavor+"/"+row.name, func(t *testing.T) {
				s := md051Settings()
				on := true
				s["MD051"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"cross_file": row.crossFile}}
				out := analyzeHeadings(t, registry(t, Registrations()), row.source, s, engine.DocumentContext{Flavor: flavor, AnchorStyle: row.style}, engine.RunOptions{})
				if len(out.Diagnostics) != row.findings || len(out.Coverage) != row.coverage || len(out.Errors) != 0 || out.Complete != (row.coverage == 0) {
					t.Fatal(out)
				}
				for _, c := range out.Coverage {
					if c.Rule != "MD051" || c.Reason != result.ContextUnavailable {
						t.Fatal(c)
					}
				}
			})
		}
	}
}

func TestMD051RuleControls(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, source := range []string{"", "# A\n\n[x](#a)", "[x](#nope)"} {
			for _, originating := range []bool{false, true} {
				for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
					t.Run(fmt.Sprintf("%s/%q/origin-%v/%v", flavor, source, originating, reason), func(t *testing.T) {
						origin := &md014PublicationStop{Context: context.Background(), reason: reason}
						called := false
						r := registry(t, []engine.Registration{{ID: "MD051", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
							gate := ".unresolvedFragmentTargets.func1"
							caller := &md014PublicationStop{Context: context.Background(), gate: gate, reason: reason}
							var target context.Context = caller
							if originating {
								origin.gate = gate
								target = context.Background()
							}
							out := unresolvedFragmentTargets(target, c)
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
						in, e := engine.NewInput("literal.md", []byte(source), md051Settings(), engine.DocumentContext{Flavor: flavor})
						if e != nil {
							t.Fatal(e)
						}
						if _, e = r.Lint(origin, []engine.Input{in}, engine.RunOptions{}); e != nil || !called {
							t.Fatal(e, called)
						}
					})
				}
			}
		}
	}
}
