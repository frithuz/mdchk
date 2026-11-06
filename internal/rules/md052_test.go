package rules

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
)

const md052Example = "[Guide][manual]"

func TestMD052RegistrationAvailabilityAndOptions(t *testing.T) {
	registered := false
	for _, registration := range Registrations() {
		registered = registered || registration.ID == "MD052"
	}
	r := registry(t, Registrations())
	if !registered || !r.Available("MD052") {
		t.Fatal("MD052 registration", registered)
	}
	s, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil || !s["MD052"].Available || !s["MD052"].Enabled || len(s["MD052"].Options) != 0 {
		t.Fatal(s["MD052"], err)
	}
	found := false
	for _, meta := range r.Metadata() {
		if meta.ID == "MD052" {
			found = true
			if !meta.DefaultEnabled || meta.Fix != engine.NoFix || !meta.RequiresSyntax || len(meta.Options) != 0 {
				t.Fatal(meta)
			}
		}
	}
	if !found {
		t.Fatal("MD052 metadata missing")
	}
	for _, mapping := range []map[string]any{{"unknown": 1}, {"enabled": "yes"}, {"allow_directives": "false"}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD052": setting}, engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal("invalid MD052 setting admitted", mapping)
		}
	}
}

type md052Literal struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Options      struct {
		Enabled     *bool
		Frontmatter string
	}
	Findings []struct {
		Bytes, Start, End [2]int
		Expected, Message string
	}
}

func md052Literals(t *testing.T) []md052Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md052.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "b4d56f7f2b7f4485ad3a2b1b9ec3b4834e69674a7dc6db525c01e2a804470d2e" {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md052Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 56 {
		t.Fatal(e, len(v.Cases))
	}
	return v.Cases
}

func TestMD052FrozenProductionOracle(t *testing.T) {
	for _, tc := range md052Literals(t) {
		for _, flavor := range tc.Flavors {
			for _, inspect := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/inspect-%v", tc.Name, flavor, inspect), func(t *testing.T) {
					if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
						t.Fatal("source identity")
					}
					s := onlyMD052Settings()
					setting := s["MD052"]
					if tc.Options.Enabled != nil {
						setting.Enabled = tc.Options.Enabled
					}
					s["MD052"] = setting
					dc := engine.DocumentContext{Flavor: flavor}
					if tc.Options.Frontmatter != "" {
						var e error
						dc.Frontmatter, e = parser.NewFrontmatterOptions(true, []string{tc.Options.Frontmatter})
						if e != nil {
							t.Fatal(e)
						}
					}
					selection, e := registry(t, Registrations()).SelectRules(s, engine.ValidationProviders{})
					if e != nil {
						t.Fatal(e)
					}
					plan, e := selection.Plan(dc, nil)
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
						if d.Rule != "MD052" || d.Message != w.Message || d.Path != "literal.md" || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}) || d.FixAvailable || d.ByteRange.Start != w.Bytes[0] || d.ByteRange.End != w.Bytes[1] || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] {
							t.Fatal(d, w)
						}
					}
					if inspect {
						found := false
						for _, ev := range trace.Evaluations() {
							if ev.Rule == "MD052" {
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

func TestMD052LineEndingsShareRanges(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			var ranges [2][]result.Range
			for i, ending := range []string{"\n", "\r\n"} {
				source := "intro" + ending + "[a][b] and ![c][]" + ending + ending + "> [d][e]" + ending
				out := analyzeHeadings(t, registry(t, Registrations()), source, onlyMD052Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Diagnostics) != 3 || len(out.Errors)+len(out.Coverage) != 0 {
					t.Fatal(out)
				}
				for _, d := range out.Diagnostics {
					if source[d.ByteRange.Start:d.ByteRange.End] != "[a][b]" && source[d.ByteRange.Start:d.ByteRange.End] != "![c][]" && source[d.ByteRange.Start:d.ByteRange.End] != "[d][e]" {
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

func TestMD052SuppressionInactiveAndUnused(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"used", "denied", "unused", "inactive"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				source := "<!-- mdchk:disable MD052 -->\n" + md052Example
				if mode == "unused" {
					source = "<!-- mdchk:disable MD052 -->\n[Guide][manual]\n\n[manual]: u"
				}
				if mode == "inactive" {
					source = md052Example
				}
				s := onlyMD052Settings()
				on, allow, enabled := true, mode != "denied", mode != "inactive"
				s["MD052"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow}
				s["MD087"] = engine.RuleSetting{Enabled: &on}
				sel, err := registry(t, Registrations()).SelectRules(s, engine.ValidationProviders{})
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
					case "MD052":
						visible++
						if d.Range.Start.Line != 2 || d.Range.Start.Column != 1 || d.Range.End.Column != 16 || d.Message != "Reference has no matching definition." || d.FixAvailable {
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
						if observation.Diagnostic.Rule == "MD052" {
							count++
							if observation.Diagnostic.ByteRange != (result.ByteRange{Start: 0, End: 15}) {
								t.Fatal(observation)
							}
						}
					}
					if count != 1 {
						t.Fatal("MD052 inactive observation count", count)
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
					if hidden.Diagnostic.Rule != "MD052" || hidden.Diagnostic.ByteRange != (result.ByteRange{Start: 29, End: 44}) || hidden.Diagnostic.Range.Start.Line != 2 || hidden.SuppressedBy.Kind != "directive" {
						t.Fatal(hidden)
					}
				}
			})
		}
	}
}

func TestMD052ConfiguredOffEnableAndDenial(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow-%v", allow), func(t *testing.T) {
			off := false
			s := onlyMD052Settings()
			s["MD052"] = engine.RuleSetting{Enabled: &off, AllowDirectives: &allow}
			out := analyzeHeadings(t, registry(t, Registrations()), "<!-- mdchk:enable MD052 -->\n"+md052Example, s, engine.DocumentContext{}, engine.RunOptions{})
			want := 0
			if allow {
				want = 1
			}
			if !out.Complete || len(out.Diagnostics) != want || len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(out)
			}
			if allow && (out.Diagnostics[0].Range != (result.Range{Start: result.Position{Line: 2, Column: 1}, End: result.Position{Line: 2, Column: 16}})) {
				t.Fatal(out)
			}
		})
	}
}

func TestMD052UnknownContextWithholdsObservation(t *testing.T) {
	for _, audit := range []bool{false, true} {
		t.Run(fmt.Sprintf("audit-%v", audit), func(t *testing.T) {
			s := onlyMD052Settings()
			source := md052Example + "\n\n<div>\n<x title=\"\n\n" + md052Example
			if audit {
				on := true
				s["MD087"] = engine.RuleSetting{Enabled: &on}
				source = "<!-- mdchk:disable MD052 -->\n" + source
			}
			out := analyzeHeadings(t, registry(t, Registrations()), source, s, engine.DocumentContext{}, engine.RunOptions{})
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

func TestMD052UnavailableInventoryOutcome(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			called := false
			r := registry(t, []engine.Registration{{ID: "MD052", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				called = true
				items, err := c.UndefinedReferences(ctx)
				if err != nil || items.Availability != parser.Available || len(items.Spans) != 1 {
					t.Fatal("actual positive prerequisite", items, err)
				}
				out := undefinedReferenceOutcome(ctx, c, parser.UndefinedReferenceInventory{Availability: parser.Unavailable, Spans: items.Spans})
				if out.Err != nil || out.Findings != nil || out.IncompleteReason != "Reference-definition context is unavailable; the complete MD052 inventory is withheld." {
					t.Fatal(out)
				}
				if bad := undefinedReferenceOutcome(ctx, c, parser.UndefinedReferenceInventory{Spans: items.Spans}); !errors.Is(bad.Err, parser.ErrModel) || bad.Findings != nil {
					t.Fatal(bad)
				}
				return out
			}}})
			out := analyzeHeadings(t, r, md052Example, onlyMD052Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			if !called || out.Complete || len(out.Diagnostics)+len(out.Errors) != 0 || len(out.Coverage) != 1 || out.Coverage[0].Rule != "MD052" || out.Coverage[0].Reason != result.ContextUnavailable || out.Coverage[0].Message != "Reference-definition context is unavailable; the complete MD052 inventory is withheld." {
				t.Fatal(out)
			}
		})
	}
}

func TestMD052ReportOnlyLeavesSourceAndPlansNothing(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"", "project"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				source := md052Example + "\n\n![Logo][]\n"
				caller := []byte(source)
				in, err := engine.NewInput("literal.md", caller, onlyMD052Settings(), engine.DocumentContext{Flavor: flavor})
				if err != nil {
					t.Fatal(err)
				}
				out, err := registry(t, Registrations()).Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Mode: mode})
				if err != nil || !out.Complete || len(out.Diagnostics) != 2 || string(caller) != source || string(in.Source().Bytes()) != source {
					t.Fatal(out, err)
				}
				if len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 || len(out.Files) != 1 || len(out.Files[0].ChangeIDs) != 0 || out.Files[0].WriteStatus != "not-requested" {
					t.Fatal(out)
				}
				for _, d := range out.Diagnostics {
					if d.Rule != "MD052" || d.FixAvailable {
						t.Fatal(d)
					}
				}
			})
		}
	}
}

func TestMD052DoesNotExecuteProtectedDirectives(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			s := onlyMD052Settings()
			on := true
			s["MD087"] = engine.RuleSetting{Enabled: &on}
			selected, err := registry(t, Registrations()).SelectRules(s, engine.ValidationProviders{})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := selected.Plan(engine.DocumentContext{Flavor: flavor}, nil)
			if err != nil {
				t.Fatal(err)
			}
			source := "```md\n<!-- mdchk:disable MD052 -->\n" + md052Example + "\n```\n\n" + md052Example
			in, err := plan.NewInput("literal.md", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{})
			if err != nil || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD052" || out.Diagnostics[0].Range.Start.Line != 6 || len(trace.Directives().Candidates()) != 0 || len(out.Errors)+len(out.Coverage)+len(out.SuppressedDiagnostics) != 0 {
				t.Fatal(out, err)
			}
		})
	}
}

func TestMD052DefaultAndEmptyOptionsEnabled(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, empty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/empty-%v", flavor, empty), func(t *testing.T) {
				s := onlyMD052Settings()
				delete(s, "MD052")
				if empty {
					s["MD052"] = engine.RuleSetting{Options: engine.Options{}}
				}
				out := analyzeHeadings(t, registry(t, Registrations()), md052Example, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD052" || out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 0, End: 15}) || out.Diagnostics[0].FixAvailable || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(out)
				}
			})
		}
	}
}
