package md053_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/md053"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

const md053Example = "[unused]: guide.md"

const (
	md053DefinitionsUnavailable = "Reference-definition context is unavailable; the complete MD053 inventory is withheld."
	md053UsesUnavailable        = "Reference-use context is unavailable; MD053 unused-definition findings are withheld."
)

func TestMD053RegistrationAvailabilityAndOptions(t *testing.T) {
	registered := false
	for _, registration := range rules.Registrations() {
		registered = registered || registration.ID == "MD053"
	}
	r := ruletest.Registry(t, rules.Registrations())
	if !registered || !r.Available("MD053") {
		t.Fatal("MD053 registration", registered)
	}
	s, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil || !s["MD053"].Available || !s["MD053"].Enabled || len(s["MD053"].Options) != 1 {
		t.Fatal(s["MD053"], err)
	}
	if list, ok := s["MD053"].Options["ignored_labels"].([]string); !ok || len(list) != 0 {
		t.Fatal(s["MD053"].Options)
	}
	found := false
	for _, meta := range r.Metadata() {
		if meta.ID == "MD053" {
			found = true
			if !meta.DefaultEnabled || meta.Fix != engine.PlannedFix || !meta.RequiresSyntax || len(meta.Options) != 1 {
				t.Fatal(meta)
			}
		}
	}
	if !found {
		t.Fatal("MD053 metadata missing")
	}
	for _, mapping := range []map[string]any{{"unknown": 1}, {"ignored_labels": "foo"}, {"ignored_labels": []any{1}}, {"enabled": "yes"}, {"allow_directives": "false"}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD053": setting}, engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal("invalid MD053 setting admitted", mapping)
		}
	}
}

type md053Literal struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Options      struct {
		Enabled       *bool
		IgnoredLabels []string `json:"ignored_labels"`
	}
	Findings []struct {
		Bytes, Start, End [2]int
		Expected, Message string
	}
}

func md053Literals(t *testing.T) []md053Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md053.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "fe652ecb027e06779a25cb37dcfcf65b1bb63396e7580b25d51aff406f39c58f" {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md053Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 36 {
		t.Fatal(e, len(v.Cases))
	}
	return v.Cases
}

func md053Settings() engine.Settings {
	s := ruletest.Settings("MD011")
	off, on := false, true
	s["MD011"] = engine.RuleSetting{Enabled: &off}
	s["MD053"] = engine.RuleSetting{Enabled: &on}
	return s
}

func TestMD053FrozenProductionOracle(t *testing.T) {
	for _, tc := range md053Literals(t) {
		for _, flavor := range tc.Flavors {
			for _, inspect := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/inspect-%v", tc.Name, flavor, inspect), func(t *testing.T) {
					if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
						t.Fatal("source identity")
					}
					s := md053Settings()
					setting := s["MD053"]
					if tc.Options.Enabled != nil {
						setting.Enabled = tc.Options.Enabled
					}
					if tc.Options.IgnoredLabels != nil {
						setting.Options = engine.Options{"ignored_labels": tc.Options.IgnoredLabels}
					}
					s["MD053"] = setting
					selection, e := ruletest.Registry(t, rules.Registrations()).SelectRules(s, engine.ValidationProviders{})
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
					if out.Status != status || len(out.Diagnostics) != len(tc.Findings) {
						t.Fatal(out.Status, out.Diagnostics, tc.Findings)
					}
					for i, d := range out.Diagnostics {
						w := tc.Findings[i]
						if d.Rule != "MD053" || d.Message != w.Message || d.Path != "literal.md" || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}) || d.FixAvailable || d.ByteRange.Start != w.Bytes[0] || d.ByteRange.End != w.Bytes[1] || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] {
							t.Fatal(d, w)
						}
					}
					if inspect {
						found := false
						for _, ev := range trace.Evaluations() {
							if ev.Rule == "MD053" {
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

func TestMD053LineEndingsShareRanges(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			var ranges [2][]result.Range
			for i, ending := range []string{"\n", "\r\n"} {
				source := "[b]" + ending + ending + "[a]: x" + ending + "> [b]: y" + ending + ending + "- [B]: z" + ending + "[foo" + ending + "bar]: w" + ending
				out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), source, md053Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Diagnostics) != 3 || len(out.Errors)+len(out.Coverage) != 0 {
					t.Fatal(out)
				}
				for _, d := range out.Diagnostics {
					text := source[d.ByteRange.Start:d.ByteRange.End]
					if text != "[a]:" && text != "[B]:" && text != "[foo"+ending+"bar]:" {
						t.Fatal(d)
					}
					ranges[i] = append(ranges[i], d.Range)
				}
			}
			if !reflect.DeepEqual(ranges[0], ranges[1]) {
				t.Fatal(ranges)
			}
		})
	}
}

func TestMD053SuppressionInactiveAndUnused(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"used", "denied", "unused", "inactive"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				source := "<!-- mdchk:disable MD053 -->\n" + md053Example
				if mode == "unused" {
					source = "<!-- mdchk:disable MD053 -->\n[Guide][manual]\n\n[manual]: u"
				}
				if mode == "inactive" {
					source = md053Example
				}
				s := md053Settings()
				on, allow, enabled := true, mode != "denied", mode != "inactive"
				s["MD053"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow}
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
				out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{ObserveInactive: mode == "inactive"})
				if err != nil || !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(out, err)
				}
				visible, audit := 0, 0
				for _, d := range out.Diagnostics {
					switch d.Rule {
					case "MD053":
						visible++
						if d.Range.Start.Line != 2 || d.Range.Start.Column != 1 || d.Range.End.Column != 10 || d.Message != "Definition \"unused\" is not used by any reference." || d.FixAvailable {
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
						if observation.Diagnostic.Rule == "MD053" {
							count++
							if observation.Diagnostic.ByteRange != (result.ByteRange{Start: 0, End: 9}) {
								t.Fatal(observation)
							}
						}
					}
					if count != 1 {
						t.Fatal("MD053 inactive observation count", count)
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
					if hidden.Diagnostic.Rule != "MD053" || hidden.Diagnostic.ByteRange != (result.ByteRange{Start: 29, End: 38}) || hidden.Diagnostic.Range.Start.Line != 2 || hidden.SuppressedBy.Kind != "directive" {
						t.Fatal(hidden)
					}
				}
			})
		}
	}
}

func TestMD053ConfiguredOffEnableAndDenial(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow-%v", allow), func(t *testing.T) {
			off := false
			s := md053Settings()
			s["MD053"] = engine.RuleSetting{Enabled: &off, AllowDirectives: &allow}
			out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), "<!-- mdchk:enable MD053 -->\n"+md053Example, s, engine.DocumentContext{}, engine.RunOptions{})
			want := 0
			if allow {
				want = 1
			}
			if !out.Complete || len(out.Diagnostics) != want || len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(out)
			}
			if allow && (out.Diagnostics[0].Range != (result.Range{Start: result.Position{Line: 2, Column: 1}, End: result.Position{Line: 2, Column: 10}})) {
				t.Fatal(out)
			}
		})
	}
}

// The parser's own unavailable paths are covered with altered parsed documents
// in internal/parser; here the rule maps both reduced inventories to coverage.
func TestMD053UnavailableInventoryOutcomes(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"definitions", "uses"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				called := false
				r := ruletest.Registry(t, []engine.Registration{{ID: "MD053", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					called = true
					items, err := c.UnneededDefinitions(ctx)
					if err != nil || items.Shadowed != parser.Available || items.Unused != parser.Available || len(items.Definitions) != 2 || items.Definitions[0].Shadowed || !items.Definitions[1].Shadowed {
						t.Fatal("actual positive prerequisite", items, err)
					}
					reduced := parser.UnneededDefinitionInventory{Shadowed: parser.Unavailable, Unused: parser.Unavailable}
					if mode == "uses" {
						reduced = parser.UnneededDefinitionInventory{Definitions: items.Definitions[1:], Shadowed: parser.Available, Unused: parser.Unavailable}
					}
					out := md053.UnneededDefinitionOutcome(ctx, c, reduced)
					want := md053DefinitionsUnavailable
					if mode == "uses" {
						want = md053UsesUnavailable
					}
					if out.Err != nil || out.IncompleteReason != want || (mode == "uses") != (len(out.Findings) == 1) {
						t.Fatal(out)
					}
					for _, bad := range []parser.UnneededDefinitionInventory{
						{Definitions: items.Definitions},
						{Definitions: items.Definitions, Shadowed: parser.Available},
						{Definitions: items.Definitions[:1], Shadowed: parser.Available, Unused: parser.Unavailable},
						{Definitions: []parser.UnneededDefinition{items.Definitions[1], items.Definitions[0]}, Shadowed: parser.Available, Unused: parser.Available},
					} {
						if got := md053.UnneededDefinitionOutcome(ctx, c, bad); !errors.Is(got.Err, parser.ErrModel) || got.Findings != nil {
							t.Fatal(bad, got)
						}
					}
					return out
				}}})
				out := ruletest.AnalyzeDocument(t, r, "[a]: x\n[A]: y\n", md053Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !called || out.Complete || len(out.Errors) != 0 || len(out.Coverage) != 1 || out.Coverage[0].Rule != "MD053" || out.Coverage[0].Reason != result.ContextUnavailable {
					t.Fatal(out)
				}
				if mode == "definitions" && (len(out.Diagnostics) != 0 || out.Coverage[0].Message != md053DefinitionsUnavailable) {
					t.Fatal(out)
				}
				if mode == "uses" && (len(out.Diagnostics) != 1 || out.Coverage[0].Message != md053UsesUnavailable || out.Diagnostics[0].Message != "Definition \"a\" is shadowed by an earlier definition of the same label." || out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 7, End: 11})) {
					t.Fatal(out)
				}
			})
		}
	}
}

func TestMD053ReportOnlyLeavesSourceAndPlansNothing(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"", "project"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				source := md053Example + "\n[Unused]: other.md\n"
				caller := []byte(source)
				in, err := engine.NewInput("literal.md", caller, md053Settings(), engine.DocumentContext{Flavor: flavor})
				if err != nil {
					t.Fatal(err)
				}
				out, err := ruletest.Registry(t, rules.Registrations()).Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Mode: mode})
				if err != nil || !out.Complete || len(out.Diagnostics) != 2 || string(caller) != source || string(in.Source().Bytes()) != source {
					t.Fatal(out, err)
				}
				if len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 || len(out.Files) != 1 || len(out.Files[0].ChangeIDs) != 0 || out.Files[0].WriteStatus != "not-requested" {
					t.Fatal(out)
				}
				for _, d := range out.Diagnostics {
					if d.Rule != "MD053" || d.FixAvailable {
						t.Fatal(d)
					}
				}
			})
		}
	}
}

func TestMD053DoesNotExecuteProtectedDirectives(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			s := md053Settings()
			on := true
			s["MD087"] = engine.RuleSetting{Enabled: &on}
			selected, err := ruletest.Registry(t, rules.Registrations()).SelectRules(s, engine.ValidationProviders{})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := selected.Plan(engine.DocumentContext{Flavor: flavor}, nil)
			if err != nil {
				t.Fatal(err)
			}
			source := "```md\n<!-- mdchk:disable MD053 -->\n" + md053Example + "\n```\n\n" + md053Example
			in, err := plan.NewInput("literal.md", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{})
			if err != nil || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD053" || out.Diagnostics[0].Range.Start.Line != 6 || len(trace.Directives().Candidates()) != 0 || len(out.Errors)+len(out.Coverage)+len(out.SuppressedDiagnostics) != 0 {
				t.Fatal(out, err)
			}
		})
	}
}

func TestMD053DefaultAndEmptyOptionsEnabled(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, empty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/empty-%v", flavor, empty), func(t *testing.T) {
				s := md053Settings()
				delete(s, "MD053")
				if empty {
					s["MD053"] = engine.RuleSetting{Options: engine.Options{}}
				}
				out := ruletest.AnalyzeDocument(t, ruletest.Registry(t, rules.Registrations()), md053Example, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD053" || out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 0, End: 9}) || out.Diagnostics[0].FixAvailable || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(out)
				}
			})
		}
	}
}
