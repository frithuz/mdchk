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
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

const md058Example = "# Title\n| a |\n| - |\n# Next\n"

const md058FixtureSHA = "0329e5fe41d8e42d369e911602fe2085b3e9b002e7cd2beba0edb6ab45a4e095"

func TestMD058RegistrationAvailabilityAndOptions(t *testing.T) {
	registered := false
	for _, registration := range Registrations() {
		registered = registered || registration.ID == "MD058"
	}
	r := registry(t, Registrations())
	if !registered || !r.Available("MD058") {
		t.Fatal("MD058 registration", registered)
	}
	s, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil || !s["MD058"].Available || !s["MD058"].Enabled || len(s["MD058"].Options) != 0 {
		t.Fatal(s["MD058"], err)
	}
	found := false
	for _, meta := range r.Metadata() {
		if meta.ID == "MD058" {
			found = true
			if !meta.DefaultEnabled || meta.Fix != engine.PlannedFix || !meta.RequiresSyntax || len(meta.Options) != 0 {
				t.Fatal(meta)
			}
		}
	}
	if !found {
		t.Fatal("MD058 metadata missing")
	}
	for _, mapping := range []map[string]any{{"unknown": 1}, {"before": true}, {"enabled": "yes"}, {"allow_directives": "false"}, {"enabled": false, "unknown": true}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD058": setting}, engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal("invalid MD058 setting admitted", mapping)
		}
	}
}

type md058Literal struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Options      struct{ Enabled *bool }
	Findings     []md058LiteralFinding
}

type md058LiteralFinding struct {
	Bytes, Start, End [2]int
	Message           string
}

func md058Literals(t *testing.T) []md058Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md058.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != md058FixtureSHA {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md058Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 40 {
		t.Fatal(e, len(v.Cases))
	}
	return v.Cases
}

func md058Settings() engine.Settings {
	s := onlyMD055Settings()
	off, on := false, true
	s["MD055"] = engine.RuleSetting{Enabled: &off}
	s["MD058"] = engine.RuleSetting{Enabled: &on}
	return s
}

func md058LiteralSettings(tc md058Literal) engine.Settings {
	s := md058Settings()
	if tc.Options.Enabled != nil {
		s["MD058"] = engine.RuleSetting{Enabled: tc.Options.Enabled}
	}
	return s
}

func md058AssertFindings(t *testing.T, source string, ds []result.Diagnostic, want []md058LiteralFinding) {
	t.Helper()
	if len(ds) != len(want) {
		t.Fatal(ds, want)
	}
	for i, d := range ds {
		w := want[i]
		if w.Message != "Table requires a blank body line before it." && w.Message != "Table requires a blank body line after it." || w.Bytes[0] != w.Bytes[1] || w.Start != w.End {
			t.Fatal("fixture witness", w)
		}
		// Before points start a line; after points precede a line ending or EOF.
		if strings.HasSuffix(w.Message, "before it.") && w.Bytes[0] > 0 && w.Start[1] == 1 && source[w.Bytes[0]-1] != '\n' || strings.HasSuffix(w.Message, "after it.") && w.Bytes[0] < len(source) && source[w.Bytes[0]] != '\n' && source[w.Bytes[0]] != '\r' {
			t.Fatal("fixture point", w)
		}
		if d.Rule != "MD058" || d.Message != w.Message || d.FixAvailable || d.ByteRange.Start != w.Bytes[0] || d.ByteRange.End != w.Bytes[1] || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] {
			t.Fatal(d, w)
		}
	}
}

func TestMD058FrozenProductionOracle(t *testing.T) {
	executions := 0
	for _, tc := range md058Literals(t) {
		for _, flavor := range tc.Flavors {
			for _, inspect := range []bool{false, true} {
				executions++
				t.Run(fmt.Sprintf("%s/%s/inspect-%v", tc.Name, flavor, inspect), func(t *testing.T) {
					if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
						t.Fatal("source identity")
					}
					selection, e := registry(t, Registrations()).SelectRules(md058LiteralSettings(tc), engine.ValidationProviders{})
					if e != nil {
						t.Fatal(e)
					}
					plan, e := selection.Plan(engine.DocumentContext{Flavor: flavor}, nil)
					if e != nil {
						t.Fatal(e)
					}
					original := []byte(tc.Source)
					in, e := plan.NewInput("literal.md", original)
					if e != nil {
						t.Fatal(e)
					}
					token := in.Source().Token()
					var out result.Result
					var trace *engine.InspectionTrace
					if inspect {
						out, trace, e = plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{})
					} else {
						out, e = plan.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
					}
					if e != nil || !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes)+len(out.Interruptions)+len(out.Omissions)+len(out.SuppressedDiagnostics) != 0 || string(original) != tc.Source || string(in.Source().Bytes()) != tc.Source || in.Source().Token() != token {
						t.Fatal(out, e)
					}
					if len(out.Files) != 1 || out.Files[0].WriteStatus != "not-requested" || len(out.Files[0].ChangeIDs) != 0 {
						t.Fatal(out.Files)
					}
					md058AssertFindings(t, tc.Source, out.Diagnostics, tc.Findings)
					for _, d := range out.Diagnostics {
						if d.Path != "literal.md" || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}) {
							t.Fatal(d)
						}
					}
					if inspect {
						found := false
						for _, ev := range trace.Evaluations() {
							if ev.Rule == "MD058" {
								found = true
								switch {
								case flavor == "commonmark":
									if ev.Active != "skipped" || !reflect.DeepEqual(ev.Reasons, []string{"inapplicable"}) {
										t.Fatal(ev)
									}
								case tc.Options.Enabled != nil && !*tc.Options.Enabled:
									if ev.Active != "skipped" {
										t.Fatal(ev)
									}
								case ev.Active != "complete":
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
	if executions != 2*42 {
		t.Fatal(executions)
	}
}

func TestMD058FixtureCoversLineEndingsAndShapes(t *testing.T) {
	names := map[string]md058Literal{}
	for _, tc := range md058Literals(t) {
		names[tc.Name] = tc
	}
	for name, tc := range names {
		if !strings.HasSuffix(name, "-crlf") {
			continue
		}
		lf, ok := names[strings.TrimSuffix(name, "-crlf")]
		if !ok || strings.ReplaceAll(lf.Source, "\n", "\r\n") != tc.Source || len(lf.Findings) != len(tc.Findings) || len(tc.Findings) == 0 {
			t.Fatal("CRLF case without its LF counterpart", name)
		}
		for i, w := range tc.Findings {
			if w.Start != lf.Findings[i].Start || w.End != lf.Findings[i].End || w.Message != lf.Findings[i].Message {
				t.Fatal("CRLF positions differ from LF", name, w, lf.Findings[i])
			}
			if strings.HasSuffix(w.Message, "after it.") && tc.Source[w.Bytes[0]] != '\r' {
				t.Fatal("after point must precede the whole CRLF", name, w)
			}
		}
	}
	for _, name := range []string{"spec-example", "spec-example-crlf", "spec-accepted", "both-sides", "both-sides-crlf", "before-heading", "before-fenced-code", "before-thematic-break", "before-definition", "after-heading", "after-fenced-code", "after-block-quote", "after-bullet-list", "after-ordered-list", "after-html-block", "after-thematic-break", "accepted-spacing", "document-edges", "block-quote-before", "block-quote-prefix-blank", "block-quote-edges", "list-item-edges", "list-item-after", "quote-in-list-item", "fenced-code-pipes", "indented-code-pipes", "html-block-pipes", "not-a-table", "raw-html-protected", "commonmark-table", "disabled"} {
		if _, ok := names[name]; !ok {
			t.Fatal("required fixture case missing", name)
		}
	}
}

func TestMD058LineEndingsShareRanges(t *testing.T) {
	for _, tc := range md058Literals(t) {
		if strings.Contains(tc.Source, "\r") {
			continue
		}
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				source := strings.ReplaceAll(tc.Source, "\n", "\r\n")
				out := analyzeHeadings(t, registry(t, Registrations()), source, md058LiteralSettings(tc), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Errors)+len(out.Coverage) != 0 {
					t.Fatal(out)
				}
				want := make([]md058LiteralFinding, len(tc.Findings))
				for i, w := range tc.Findings {
					w.Bytes[0] += strings.Count(tc.Source[:w.Bytes[0]], "\n")
					w.Bytes[1] += strings.Count(tc.Source[:w.Bytes[1]], "\n")
					want[i] = w
				}
				md058AssertFindings(t, source, out.Diagnostics, want)
				for _, d := range out.Diagnostics {
					if point := d.ByteRange.Start; point > 0 && point < len(source) && source[point-1] == '\r' && source[point] == '\n' {
						t.Fatal("range splits CRLF", d)
					}
				}
			})
		}
	}
}

func TestMD058SuppressionInactiveAndUnused(t *testing.T) {
	for _, mode := range []string{"used", "denied", "unused", "inactive"} {
		t.Run(mode, func(t *testing.T) {
			source := "<!-- mdchk:disable MD058 -->\n\n" + md058Example
			if mode == "unused" {
				source = "<!-- mdchk:disable MD058 -->\n\n# Title\n\n| a |\n| - |\n\n# Next\n"
			}
			if mode == "inactive" {
				source = md058Example
			}
			s := md058Settings()
			on, allow, enabled := true, mode != "denied", mode != "inactive"
			s["MD058"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow}
			s["MD087"] = engine.RuleSetting{Enabled: &on}
			sel, err := registry(t, Registrations()).SelectRules(s, engine.ValidationProviders{})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := sel.Plan(engine.DocumentContext{Flavor: "gfm"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			in, err := plan.NewInput("literal.md", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{ObserveInactive: mode == "inactive"})
			if err != nil || !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
				t.Fatal(out, err)
			}
			visible, audit := 0, 0
			for _, d := range out.Diagnostics {
				switch d.Rule {
				case "MD058":
					visible++
					if d.FixAvailable {
						t.Fatal(d)
					}
				case "MD087":
					audit++
				default:
					t.Fatal(d)
				}
			}
			wantVisible, wantHidden, wantAudit := 0, 2, 0
			if mode == "denied" {
				wantVisible, wantHidden = 2, 0
			}
			if mode == "unused" {
				wantHidden, wantAudit = 0, 1
			}
			if mode == "inactive" {
				var ranges []result.ByteRange
				for _, observation := range trace.Suppressed() {
					if observation.Diagnostic.Rule == "MD058" {
						ranges = append(ranges, observation.Diagnostic.ByteRange)
					}
				}
				if !reflect.DeepEqual(ranges, []result.ByteRange{{Start: 8, End: 8}, {Start: 19, End: 19}}) {
					t.Fatal("MD058 inactive observations", ranges)
				}
				wantHidden = 0
			}
			if visible != wantVisible || audit != wantAudit || len(out.SuppressedDiagnostics) != wantHidden {
				t.Fatal(out)
			}
			for i, wire := range out.SuppressedDiagnostics {
				var hidden result.SuppressedDiagnostic
				if err := json.Unmarshal(wire, &hidden); err != nil {
					t.Fatal(err)
				}
				want := []result.ByteRange{{Start: 38, End: 38}, {Start: 49, End: 49}}[i]
				if hidden.Diagnostic.Rule != "MD058" || hidden.Diagnostic.ByteRange != want || hidden.SuppressedBy.Kind != "directive" {
					t.Fatal(hidden)
				}
			}
		})
	}
}

func TestMD058CommonMarkIsInapplicable(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			out := analyzeHeadings(t, registry(t, Registrations()), md058Example, md058Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			want := 2
			if flavor == "commonmark" {
				want = 0
			}
			if !out.Complete || len(out.Diagnostics) != want || len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(out)
			}
		})
	}
}

func TestMD058ReportOnlyLeavesSourceAndPlansNothing(t *testing.T) {
	for _, mode := range []string{"", "project"} {
		t.Run(mode, func(t *testing.T) {
			caller := []byte(md058Example)
			outcomes := 0
			r := registry(t, []engine.Registration{{ID: "MD058", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				out := tableBlankLines(ctx, c)
				if out.Err != nil || len(out.Findings) != 2 || out.IncompleteReason != "" {
					t.Fatal(out)
				}
				for _, f := range out.Findings {
					if f.FixEligible {
						t.Fatal("MD058 finding is fix-eligible", f)
					}
				}
				outcomes++
				return out
			}}})
			for _, reg := range []*engine.Registry{r, registry(t, Registrations())} {
				in, err := engine.NewInput("literal.md", caller, md058Settings(), engine.DocumentContext{Flavor: "gfm"})
				if err != nil {
					t.Fatal(err)
				}
				out, err := reg.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Mode: mode})
				if err != nil || !out.Complete || len(out.Diagnostics) != 2 || string(caller) != md058Example || string(in.Source().Bytes()) != md058Example {
					t.Fatal(out, err)
				}
				if len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 || len(out.Files) != 1 || len(out.Files[0].ChangeIDs) != 0 || out.Files[0].WriteStatus != "not-requested" {
					t.Fatal(out)
				}
				for _, d := range out.Diagnostics {
					if d.Rule != "MD058" || d.FixAvailable {
						t.Fatal(d)
					}
				}
			}
			if outcomes != 1 {
				t.Fatal("rule outcome not observed", outcomes)
			}
		})
	}
}

func TestMD058DoesNotExecuteProtectedDirectives(t *testing.T) {
	s := md058Settings()
	on := true
	s["MD087"] = engine.RuleSetting{Enabled: &on}
	selected, err := registry(t, Registrations()).SelectRules(s, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selected.Plan(engine.DocumentContext{Flavor: "gfm"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := "```md\n<!-- mdchk:disable MD058 -->\n```\n\n" + md058Example
	in, err := plan.NewInput("literal.md", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{})
	if err != nil || !out.Complete || len(out.Diagnostics) != 2 || out.Diagnostics[0].Rule != "MD058" || out.Diagnostics[0].Range.Start.Line != 6 || len(trace.Directives().Candidates()) != 0 || len(out.Errors)+len(out.Coverage)+len(out.SuppressedDiagnostics) != 0 {
		t.Fatal(out, err)
	}
}

func TestMD058ComparisonRejectsMalformedPointsAtomically(t *testing.T) {
	for _, kind := range []string{"absent", "foreign", "nonzero", "utf8-interior", "crlf-interior"} {
		t.Run(kind, func(t *testing.T) {
			source := "# 雪\r\n| a |\r\n| - |\r\n# N"
			r := registry(t, []engine.Registration{{ID: "MD058", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				facts, e := c.TableSpacing(ctx)
				if e != nil || len(facts) != 2 {
					t.Fatal(facts, e)
				}
				switch kind {
				case "absent":
					facts[1].Point = parser.Span{}
				case "foreign":
					facts[1].Point, _ = parser.NewSource([]byte(source)).Span(8, 8)
				case "nonzero":
					facts[1].Point, _ = c.Source().Span(7, 9)
				case "utf8-interior":
					facts[1].Point, _ = c.Source().Span(3, 3)
				case "crlf-interior":
					facts[1].Point, _ = c.Source().Span(6, 6)
				}
				out := compareTableBlankLines(ctx, c, facts)
				if !errors.Is(out.Err, parser.ErrModel) || out.Findings != nil {
					t.Fatal(out)
				}
				return out
			}}})
			out := analyzeHeadings(t, r, source, md058Settings(), engine.DocumentContext{Flavor: "gfm"}, engine.RunOptions{})
			if out.Complete || len(out.Diagnostics) != 0 {
				t.Fatal(out)
			}
		})
	}
}

// Every interruption before the inventory completes withholds all findings.
func TestMD058WithholdsTheWholeInventory(t *testing.T) {
	for _, kind := range []string{"work", "nodes"} {
		t.Run(kind, func(t *testing.T) {
			var used int64
			for extra := int64(-1); ; extra++ {
				if extra > 4096 {
					t.Fatal("no sufficient allowance found")
				}
				var out engine.RuleOutcome
				b, _ := parser.NewOperationBudget(parser.DefaultLimits())
				r := registry(t, []engine.Registration{{ID: "MD058", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					charge, limit := b.ChargeWork, b.Limits().WorkUnits
					if kind == "nodes" {
						charge, limit = b.ChargeNodes, b.Limits().TotalNodes
					}
					// The first run measures the allowance already used before the rule.
					if extra < 0 {
						var e *parser.LimitExceeded
						if !errors.As(charge(limit), &e) {
							t.Fatal("prefix must exhaust")
						}
						used = e.Observed - limit
						return engine.RuleOutcome{Err: e}
					}
					if e := charge(limit - used - extra); e != nil {
						t.Fatal(e)
					}
					out = tableBlankLines(ctx, c)
					return out
				}}})
				analyzeHeadings(t, r, md058Example, md058Settings(), engine.DocumentContext{Flavor: "gfm"}, engine.RunOptions{Budget: b})
				if extra < 0 {
					continue
				}
				if out.Err == nil {
					if len(out.Findings) != 2 {
						t.Fatal(out)
					}
					return
				}
				var limit *parser.LimitExceeded
				if !errors.As(out.Err, &limit) || out.Findings != nil {
					t.Fatal(extra, out)
				}
			}
		})
	}
	for _, originating := range []bool{false, true} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(fmt.Sprintf("origin-%t/%s", originating, reason), func(t *testing.T) {
				for stop := 1; ; stop++ {
					if stop > 4096 {
						t.Fatal("no sufficient stop found")
					}
					origin := &lateStopContext{Context: context.Background(), stop: 1 << 30, reason: reason}
					var out engine.RuleOutcome
					r := registry(t, []engine.Registration{{ID: "MD058", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						var caller context.Context = &lateStopContext{Context: context.Background(), stop: stop, reason: reason}
						if originating {
							origin.stop = origin.calls + stop
							caller = context.Background()
						}
						out = tableBlankLines(caller, c)
						return out
					}}})
					in, e := engine.NewInput("spacing.md", []byte(md058Example), md058Settings(), engine.DocumentContext{Flavor: "gfm"})
					if e != nil {
						t.Fatal(e)
					}
					if _, e := r.Lint(origin, []engine.Input{in}, engine.RunOptions{}); e != nil {
						t.Fatal(e)
					}
					if out.Err == nil {
						if len(out.Findings) != 2 || stop < 8 {
							t.Fatal(stop, out)
						}
						return
					}
					if !errors.Is(out.Err, reason) || out.Findings != nil {
						t.Fatal(stop, out)
					}
				}
			})
		}
	}
	for _, kind := range []string{"work", "nodes", "elapsed"} {
		t.Run("sticky/"+kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				limits := parser.DefaultLimits()
				limits.OperationTime = 20 * time.Millisecond
				b, _ := parser.NewOperationBudget(limits)
				r := registry(t, []engine.Registration{{ID: "MD058", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					switch kind {
					case "work":
						_ = b.ChargeWork(limits.WorkUnits)
					case "nodes":
						_ = b.ChargeNodes(limits.TotalNodes)
					case "elapsed":
						time.Sleep(25 * time.Millisecond)
					}
					var out engine.RuleOutcome
					for range 2 {
						out = tableBlankLines(ctx, c)
						var e *parser.LimitExceeded
						if !errors.As(out.Err, &e) || out.Findings != nil {
							t.Fatal(out)
						}
					}
					return out
				}}})
				out := analyzeHeadings(t, r, md058Example, md058Settings(), engine.DocumentContext{Flavor: "gfm"}, engine.RunOptions{Budget: b})
				if out.Complete || len(out.Diagnostics) != 0 {
					t.Fatal(out)
				}
			})
		})
	}
}
