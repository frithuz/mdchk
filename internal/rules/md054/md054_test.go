package md054_test

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
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/md054"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

const md054Example = "[Guide](guide.md)"

var md054Styles = []string{"inline", "full_reference", "collapsed_reference", "shortcut_reference", "autolink"}

func TestMD054RegistrationAvailabilityAndOptions(t *testing.T) {
	registered := false
	for _, registration := range rules.Registrations() {
		registered = registered || registration.ID == "MD054"
	}
	r := ruletest.Registry(t, rules.Registrations())
	if !registered || !r.Available("MD054") {
		t.Fatal("MD054 registration", registered)
	}
	s, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil || !s["MD054"].Available || s["MD054"].Enabled || len(s["MD054"].Options) != 1 || !reflect.DeepEqual(s["MD054"].Options["allowed_styles"], md054Styles) {
		t.Fatal(s["MD054"], err)
	}
	found := false
	for _, meta := range r.Metadata() {
		if meta.ID == "MD054" {
			found = true
			if meta.DefaultEnabled || meta.Fix != engine.PlannedFix || !meta.RequiresSyntax || len(meta.Options) != 1 {
				t.Fatal(meta)
			}
		}
	}
	if !found {
		t.Fatal("MD054 metadata missing")
	}
	for _, mapping := range []map[string]any{{"unknown": 1}, {"style": "inline"}, {"enabled": "yes"}, {"allow_directives": "false"}, {"allowed_styles": []any{}}, {"allowed_styles": []any{"reference"}}, {"allowed_styles": []any{"Inline"}}, {"allowed_styles": "inline"}, {"allowed_styles": []any{1}}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD054": setting}, engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal("invalid MD054 setting admitted", mapping)
		}
	}
}

type md054Literal struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Options      struct {
		Enabled       *bool
		AllowedStyles []string `json:"allowed_styles"`
	}
	Findings []md054LiteralFinding
}

type md054LiteralFinding struct {
	Bytes, Start, End [2]int
	Expected, Message string
}

func md054Literals(t *testing.T) []md054Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md054.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "c064141c21b18bae99ebca4a7b252f8d5d78e976873a9ba0b9b8199227e698bf" {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md054Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 60 {
		t.Fatal(e, len(v.Cases))
	}
	return v.Cases
}

func md054Settings() engine.Settings {
	s := ruletest.Settings("MD011")
	off, on := false, true
	s["MD011"] = engine.RuleSetting{Enabled: &off}
	s["MD034"] = engine.RuleSetting{Enabled: &off}
	s["MD054"] = engine.RuleSetting{Enabled: &on}
	return s
}

func md054LiteralSettings(tc md054Literal) engine.Settings {
	s := md054Settings()
	setting := s["MD054"]
	if tc.Options.Enabled != nil {
		setting.Enabled = tc.Options.Enabled
	}
	if tc.Options.AllowedStyles != nil {
		setting.Options = engine.Options{"allowed_styles": tc.Options.AllowedStyles}
	}
	s["MD054"] = setting
	return s
}

func md054AssertFindings(t *testing.T, ds []result.Diagnostic, want []md054LiteralFinding) {
	t.Helper()
	if len(ds) != len(want) {
		t.Fatal(ds, want)
	}
	for i, d := range ds {
		w := want[i]
		kind := "Link"
		if strings.HasPrefix(w.Message, "Image") {
			kind = "Image"
		}
		if w.Message != kind+" representation style \""+w.Expected+"\" is not allowed." {
			t.Fatal("fixture message names another style", w)
		}
		if d.Rule != "MD054" || d.Message != w.Message || d.FixAvailable || d.ByteRange.Start != w.Bytes[0] || d.ByteRange.End != w.Bytes[1] || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] {
			t.Fatal(d, w)
		}
	}
}

func TestMD054FrozenProductionOracle(t *testing.T) {
	for _, tc := range md054Literals(t) {
		for _, flavor := range tc.Flavors {
			for _, inspect := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/inspect-%v", tc.Name, flavor, inspect), func(t *testing.T) {
					if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
						t.Fatal("source identity")
					}
					for _, w := range tc.Findings {
						if strings.HasPrefix(w.Message, "Image") && tc.Source[w.Bytes[0]] != '!' {
							t.Fatal("image range does not start at !", w)
						}
					}
					selection, e := ruletest.Registry(t, rules.Registrations()).SelectRules(md054LiteralSettings(tc), engine.ValidationProviders{})
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
					if len(out.Files) != 1 || out.Files[0].WriteStatus != "not-requested" || len(out.Files[0].ChangeIDs) != 0 || !reflect.DeepEqual(out.CheckedFiles, []string{"literal.md"}) {
						t.Fatal(out.Files)
					}
					status := "clean"
					if len(tc.Findings) > 0 {
						status = "violations"
					}
					if out.Status != status {
						t.Fatal(out.Status, out.Diagnostics, tc.Findings)
					}
					md054AssertFindings(t, out.Diagnostics, tc.Findings)
					for _, d := range out.Diagnostics {
						if d.Path != "literal.md" || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}) {
							t.Fatal(d)
						}
					}
					if inspect {
						found := false
						for _, ev := range trace.Evaluations() {
							if ev.Rule == "MD054" {
								found = true
								if tc.Options.Enabled != nil && !*tc.Options.Enabled {
									if ev.Active != "skipped" {
										t.Fatal(ev)
									}
								} else if ev.Active != "complete" {
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
}

func TestMD054FixtureCoversEveryStyle(t *testing.T) {
	seen := map[string]bool{}
	for _, tc := range md054Literals(t) {
		for _, w := range tc.Findings {
			seen[strings.SplitN(w.Message, " ", 2)[0]+"/"+w.Expected] = true
		}
	}
	for _, style := range md054Styles {
		if !seen["Link/"+style] || style != "autolink" && !seen["Image/"+style] {
			t.Fatal("style without reported fixture", style, seen)
		}
	}
}

func TestMD054LineEndingsShareRanges(t *testing.T) {
	for _, tc := range md054Literals(t) {
		if strings.Contains(tc.Source, "\r") {
			continue
		}
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				source := strings.ReplaceAll(tc.Source, "\n", "\r\n")
				out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), source, md054LiteralSettings(tc), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Errors)+len(out.Coverage) != 0 {
					t.Fatal(out)
				}
				want := make([]md054LiteralFinding, len(tc.Findings))
				for i, w := range tc.Findings {
					w.Bytes[0] += strings.Count(tc.Source[:w.Bytes[0]], "\n")
					w.Bytes[1] += strings.Count(tc.Source[:w.Bytes[1]], "\n")
					want[i] = w
				}
				md054AssertFindings(t, out.Diagnostics, want)
				for _, d := range out.Diagnostics {
					for _, point := range []int{d.ByteRange.Start, d.ByteRange.End} {
						if point > 0 && point < len(source) && source[point-1] == '\r' && source[point] == '\n' {
							t.Fatal("range splits CRLF", d)
						}
					}
				}
			})
		}
	}
}

func TestMD054SuppressionInactiveAndUnused(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"used", "denied", "unused", "inactive"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				source := "<!-- mdchk:disable MD054 -->\n" + md054Example
				if mode == "unused" {
					source = "<!-- mdchk:disable MD054 -->\n[Guide][manual]\n\n[manual]: u"
				}
				if mode == "inactive" {
					source = md054Example
				}
				s := md054Settings()
				on, allow, enabled := true, mode != "denied", mode != "inactive"
				s["MD054"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow, Options: engine.Options{"allowed_styles": []string{"full_reference"}}}
				s["MD087"] = engine.RuleSetting{Enabled: &on}
				sel, err := ruletest.Registry(t, rules.Registrations()).SelectRules(s, engine.ValidationProviders{})
				if err != nil {
					t.Fatal(err)
				}
				plan, err := sel.Plan(engine.DocumentContext{Flavor: flavor}, nil)
				if err != nil {
					t.Fatal(err)
				}
				in, err := plan.NewInput("literal.md", []byte(source))
				if err != nil {
					t.Fatal(err)
				}
				// Observing the inactive MD057 needs the link targets to exist.
				resources := ruletest.NewNativeFragments(t, map[string]string{"docs/guide.md": "", "docs/u": ""}, flavor, "literal.md", in.Source())
				out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{Fragments: resources}, engine.InspectionOptions{ObserveInactive: mode == "inactive"})
				if err != nil || !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(out, err)
				}
				visible, audit := 0, 0
				for _, d := range out.Diagnostics {
					switch d.Rule {
					case "MD054":
						visible++
						if d.Range.Start.Line != 2 || d.Range.Start.Column != 1 || d.Range.End.Column != 18 || d.Message != `Link representation style "inline" is not allowed.` || d.FixAvailable {
							t.Fatal(d)
						}
					case "MD087":
						audit++
					default:
						t.Fatal(d)
					}
				}
				wantVisible, wantHidden, wantAudit := 0, 1, 0
				if mode == "denied" {
					wantVisible, wantHidden = 1, 0
				}
				if mode == "unused" {
					wantHidden, wantAudit = 0, 1
				}
				if mode == "inactive" {
					count := 0
					for _, observation := range trace.Suppressed() {
						if observation.Diagnostic.Rule == "MD054" {
							count++
							if observation.Diagnostic.ByteRange != (result.ByteRange{Start: 0, End: 17}) {
								t.Fatal(observation)
							}
						}
					}
					if count != 1 {
						t.Fatal("MD054 inactive observation count", count)
					}
					wantHidden = 0
				}
				if visible != wantVisible || audit != wantAudit || len(out.SuppressedDiagnostics) != wantHidden {
					t.Fatal(out)
				}
				for _, wire := range out.SuppressedDiagnostics {
					var hidden result.SuppressedDiagnostic
					if err := json.Unmarshal(wire, &hidden); err != nil {
						t.Fatal(err)
					}
					if hidden.Diagnostic.Rule != "MD054" || hidden.Diagnostic.ByteRange != (result.ByteRange{Start: 29, End: 46}) || hidden.Diagnostic.Range.Start.Line != 2 || hidden.SuppressedBy.Kind != "directive" {
						t.Fatal(hidden)
					}
				}
			})
		}
	}
}

func TestMD054ConfiguredOffEnableAndDenial(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow-%v", allow), func(t *testing.T) {
			off := false
			s := md054Settings()
			s["MD054"] = engine.RuleSetting{Enabled: &off, AllowDirectives: &allow, Options: engine.Options{"allowed_styles": []string{"full_reference"}}}
			out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), "<!-- mdchk:enable MD054 -->\n"+md054Example, s, engine.DocumentContext{}, engine.RunOptions{})
			want := 0
			if allow {
				want = 1
			}
			if !out.Complete || len(out.Diagnostics) != want || len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(out)
			}
			if allow && (out.Diagnostics[0].Range != (result.Range{Start: result.Position{Line: 2, Column: 1}, End: result.Position{Line: 2, Column: 18}})) {
				t.Fatal(out)
			}
		})
	}
}

func TestMD054UnknownContextWithholdsObservation(t *testing.T) {
	for _, audit := range []bool{false, true} {
		t.Run(fmt.Sprintf("audit-%v", audit), func(t *testing.T) {
			s := md054Settings()
			s["MD054"] = engine.RuleSetting{Enabled: s["MD054"].Enabled, Options: engine.Options{"allowed_styles": []string{"full_reference"}}}
			source := md054Example + "\n\n<div>\n<x title=\"\n\n" + md054Example
			if audit {
				on := true
				s["MD087"] = engine.RuleSetting{Enabled: &on}
				source = "<!-- mdchk:disable MD054 -->\n" + source
			}
			out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), source, s, engine.DocumentContext{}, engine.RunOptions{})
			want := 1
			if audit {
				want = 2
			}
			if out.Complete || len(out.Diagnostics)+len(out.Errors)+len(out.SuppressedDiagnostics) != 0 || len(out.Coverage) != want {
				t.Fatal(out)
			}
			for _, c := range out.Coverage {
				if c.Reason != result.ContextUnavailable {
					t.Fatal(c)
				}
			}
		})
	}
}

func TestMD054UnavailableInventoryOutcome(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			called := false
			r := ruletest.Registry(t, []engine.Registration{{ID: "MD054", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				called = true
				items, err := c.LinkRepresentations(ctx)
				if err != nil || items.Availability != parser.Available || len(items.Items) != 1 {
					t.Fatal("actual positive prerequisite", items, err)
				}
				out := md054.LinkRepresentationOutcome(ctx, c, parser.LinkRepresentationsInventory{Availability: parser.Unavailable, Items: items.Items})
				if out.Err != nil || out.Findings != nil || out.IncompleteReason != "Link representation context is unavailable; the complete MD054 inventory is withheld." {
					t.Fatal(out)
				}
				if bad := md054.LinkRepresentationOutcome(ctx, c, parser.LinkRepresentationsInventory{Items: items.Items}); !errors.Is(bad.Err, parser.ErrModel) || bad.Findings != nil {
					t.Fatal(bad)
				}
				return out
			}}})
			s := md054Settings()
			s["MD054"] = engine.RuleSetting{Enabled: s["MD054"].Enabled, Options: engine.Options{"allowed_styles": []string{"full_reference"}}}
			out := ruletest.AnalyzeDocument(t, r, md054Example, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			if !called || out.Complete || len(out.Diagnostics)+len(out.Errors) != 0 || len(out.Coverage) != 1 || out.Coverage[0].Rule != "MD054" || out.Coverage[0].Reason != result.ContextUnavailable || out.Coverage[0].Message != "Link representation context is unavailable; the complete MD054 inventory is withheld." {
				t.Fatal(out)
			}
		})
	}
}

func TestMD054ReportOnlyLeavesSourceAndPlansNothing(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"", "project"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				source := md054Example + "\n\n![Logo][]\n\n[logo]: logo.png\n"
				caller := []byte(source)
				s := md054Settings()
				s["MD054"] = engine.RuleSetting{Enabled: s["MD054"].Enabled, Options: engine.Options{"allowed_styles": []string{"full_reference"}}}
				outcomes := 0
				r := ruletest.Registry(t, []engine.Registration{{ID: "MD054", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					out := md054.LinkRepresentationStyle(ctx, c)
					if out.Err != nil || len(out.Findings) != 2 || out.IncompleteReason != "" {
						t.Fatal(out)
					}
					for _, f := range out.Findings {
						if f.FixEligible {
							t.Fatal("MD054 finding is fix-eligible", f)
						}
					}
					outcomes++
					return out
				}}})
				for _, reg := range []*engine.Registry{r, ruletest.Registry(t, rules.Registrations())} {
					in, err := engine.NewInput("literal.md", caller, s, engine.DocumentContext{Flavor: flavor})
					if err != nil {
						t.Fatal(err)
					}
					out, err := reg.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Mode: mode})
					if err != nil || !out.Complete || len(out.Diagnostics) != 2 || string(caller) != source || string(in.Source().Bytes()) != source {
						t.Fatal(out, err)
					}
					if len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 || len(out.Files) != 1 || len(out.Files[0].ChangeIDs) != 0 || out.Files[0].WriteStatus != "not-requested" {
						t.Fatal(out)
					}
					for _, d := range out.Diagnostics {
						if d.Rule != "MD054" || d.FixAvailable {
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
}

func TestMD054DoesNotExecuteProtectedDirectives(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			s := md054Settings()
			on := true
			s["MD054"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"allowed_styles": []string{"full_reference"}}}
			s["MD087"] = engine.RuleSetting{Enabled: &on}
			selected, err := ruletest.Registry(t, rules.Registrations()).SelectRules(s, engine.ValidationProviders{})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := selected.Plan(engine.DocumentContext{Flavor: flavor}, nil)
			if err != nil {
				t.Fatal(err)
			}
			source := "```md\n<!-- mdchk:disable MD054 -->\n" + md054Example + "\n```\n\n" + md054Example
			in, err := plan.NewInput("literal.md", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{})
			if err != nil || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD054" || out.Diagnostics[0].Range.Start.Line != 6 || len(trace.Directives().Candidates()) != 0 || len(out.Errors)+len(out.Coverage)+len(out.SuppressedDiagnostics) != 0 {
				t.Fatal(out, err)
			}
		})
	}
}

func TestMD054DefaultStylesReportNothing(t *testing.T) {
	source := "[a](u) [b][c] [c][] [c] <https://example.com> https://example.org\n\n![a](u) ![b][c] ![c][] ![c]\n\n[c]: v\n"
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, empty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/empty-%v", flavor, empty), func(t *testing.T) {
				s := md054Settings()
				on := true
				s["MD054"] = engine.RuleSetting{Enabled: &on}
				if empty {
					s["MD054"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{}}
				}
				out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), source, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Diagnostics)+len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(out)
				}
			})
		}
	}
}
