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

const md059Example = "[Click here](guide.md)"

var md059Phrases = []string{"click here", "here", "more", "read more"}

func TestMD059RegistrationAvailabilityAndOptions(t *testing.T) {
	registered := false
	for _, registration := range Registrations() {
		registered = registered || registration.ID == "MD059"
	}
	r := registry(t, Registrations())
	if !registered || !r.Available("MD059") {
		t.Fatal("MD059 registration", registered)
	}
	s, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil || !s["MD059"].Available || !s["MD059"].Enabled || len(s["MD059"].Options) != 1 || !reflect.DeepEqual(s["MD059"].Options["phrases"], md059Phrases) {
		t.Fatal(s["MD059"], err)
	}
	found := false
	for _, meta := range r.Metadata() {
		if meta.ID == "MD059" {
			found = true
			if !meta.DefaultEnabled || meta.Fix != engine.NoFix || !meta.RequiresSyntax || len(meta.Options) != 1 {
				t.Fatal(meta)
			}
		}
	}
	if !found {
		t.Fatal("MD059 metadata missing")
	}
	for _, mapping := range []map[string]any{{"unknown": 1}, {"phrase": []any{"here"}}, {"enabled": "yes"}, {"allow_directives": "false"}, {"phrases": "here"}, {"phrases": []any{1}}, {"phrases": nil}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD059": setting}, engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal("invalid MD059 setting admitted", mapping)
		}
	}
}

type md059Literal struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Frontmatter  bool
	Options      struct {
		Enabled *bool
		Phrases *[]string
	}
	Findings []md059LiteralFinding
}

type md059LiteralFinding struct {
	Bytes, Start, End [2]int
	Message           string
}

func md059Literals(t *testing.T) []md059Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md059.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "3cbd8724ba09a6778ce53d5545e66d2dc391bea5389ad6baf60aad9156f411fd" {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md059Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 80 {
		t.Fatal(e, len(v.Cases))
	}
	return v.Cases
}

func md059LiteralSettings(tc md059Literal) engine.Settings {
	s := onlyMD059Settings()
	setting := s["MD059"]
	if tc.Options.Enabled != nil {
		setting.Enabled = tc.Options.Enabled
	}
	if tc.Options.Phrases != nil {
		setting.Options = engine.Options{"phrases": *tc.Options.Phrases}
	}
	s["MD059"] = setting
	return s
}

func md059LiteralContext(t *testing.T, tc md059Literal, flavor string) engine.DocumentContext {
	t.Helper()
	fm, e := parser.NewFrontmatterOptions(tc.Frontmatter, nil)
	if e != nil {
		t.Fatal(e)
	}
	return engine.DocumentContext{Flavor: flavor, Frontmatter: fm}
}

func md059AssertFindings(t *testing.T, ds []result.Diagnostic, want []md059LiteralFinding) {
	t.Helper()
	if len(ds) != len(want) {
		t.Fatal(ds, want)
	}
	for i, d := range ds {
		w := want[i]
		if w.Message != "Link label is uninformative." {
			t.Fatal("fixture message", w)
		}
		if d.Rule != "MD059" || d.Message != w.Message || d.FixAvailable || d.ByteRange.Start != w.Bytes[0] || d.ByteRange.End != w.Bytes[1] || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] {
			t.Fatal(d, w)
		}
	}
}

func TestMD059FrozenProductionOracle(t *testing.T) {
	for _, tc := range md059Literals(t) {
		for _, flavor := range tc.Flavors {
			for _, inspect := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/inspect-%v", tc.Name, flavor, inspect), func(t *testing.T) {
					if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
						t.Fatal("source identity")
					}
					for _, w := range tc.Findings {
						if w.Bytes[0] == 0 || tc.Source[w.Bytes[0]-1] != '[' || tc.Source[w.Bytes[1]] != ']' {
							t.Fatal("range is not a label interior", w)
						}
					}
					selection, e := registry(t, Registrations()).SelectRules(md059LiteralSettings(tc), engine.ValidationProviders{})
					if e != nil {
						t.Fatal(e)
					}
					plan, e := selection.Plan(md059LiteralContext(t, tc, flavor), nil)
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
					md059AssertFindings(t, out.Diagnostics, tc.Findings)
					for _, d := range out.Diagnostics {
						if d.Path != "literal.md" || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}) {
							t.Fatal(d)
						}
					}
					if inspect {
						found := false
						for _, ev := range trace.Evaluations() {
							if ev.Rule == "MD059" {
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

func TestMD059FixtureCoversLineEndingsAndOptions(t *testing.T) {
	names := map[string]md059Literal{}
	for _, tc := range md059Literals(t) {
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
	}
	for _, name := range []string{"default-here-crlf", "default-read-more-soft-break-crlf", "block-quote-crlf", "multiline-quoted-crlf", "table-cell-crlf", "custom-replaces-default", "custom-empty-list", "custom-normalized-phrase", "custom-empty-phrase", "disabled", "frontmatter"} {
		if _, ok := names[name]; !ok {
			t.Fatal("required fixture case missing", name)
		}
	}
}

func TestMD059LineEndingsShareRanges(t *testing.T) {
	for _, tc := range md059Literals(t) {
		if strings.Contains(tc.Source, "\r") {
			continue
		}
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				source := strings.ReplaceAll(tc.Source, "\n", "\r\n")
				out := analyzeHeadings(t, registry(t, Registrations()), source, md059LiteralSettings(tc), md059LiteralContext(t, tc, flavor), engine.RunOptions{})
				if !out.Complete || len(out.Errors)+len(out.Coverage) != 0 {
					t.Fatal(out)
				}
				want := make([]md059LiteralFinding, len(tc.Findings))
				for i, w := range tc.Findings {
					w.Bytes[0] += strings.Count(tc.Source[:w.Bytes[0]], "\n")
					w.Bytes[1] += strings.Count(tc.Source[:w.Bytes[1]], "\n")
					want[i] = w
				}
				md059AssertFindings(t, out.Diagnostics, want)
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

func TestMD059SuppressionInactiveAndUnused(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"used", "denied", "unused", "inactive"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				source := "<!-- mdchk:disable MD059 -->\n" + md059Example
				if mode == "unused" {
					source = "<!-- mdchk:disable MD059 -->\n[Installation guide](guide.md)"
				}
				if mode == "inactive" {
					source = md059Example
				}
				s := onlyMD059Settings()
				on, allow, enabled := true, mode != "denied", mode != "inactive"
				s["MD059"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow}
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
				// Observing the inactive MD057 needs the link target to exist.
				resources := newNativeFragments(t, map[string]string{"docs/guide.md": ""}, flavor, "literal.md", in.Source())
				out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{Fragments: resources}, engine.InspectionOptions{ObserveInactive: mode == "inactive"})
				if err != nil || !out.Complete || len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(out, err)
				}
				visible, audit := 0, 0
				for _, d := range out.Diagnostics {
					switch d.Rule {
					case "MD059":
						visible++
						if d.Range.Start.Line != 2 || d.Range.Start.Column != 2 || d.Range.End.Column != 12 || d.Message != "Link label is uninformative." || d.FixAvailable {
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
						if observation.Diagnostic.Rule == "MD059" {
							count++
							if observation.Diagnostic.ByteRange != (result.ByteRange{Start: 1, End: 11}) {
								t.Fatal(observation)
							}
						}
					}
					if count != 1 {
						t.Fatal("MD059 inactive observation count", count)
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
					if hidden.Diagnostic.Rule != "MD059" || hidden.Diagnostic.ByteRange != (result.ByteRange{Start: 30, End: 40}) || hidden.Diagnostic.Range.Start.Line != 2 || hidden.SuppressedBy.Kind != "directive" {
						t.Fatal(hidden)
					}
				}
			})
		}
	}
}

func TestMD059ConfiguredOffEnableAndDenial(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow-%v", allow), func(t *testing.T) {
			off := false
			s := onlyMD059Settings()
			s["MD059"] = engine.RuleSetting{Enabled: &off, AllowDirectives: &allow}
			out := analyzeHeadings(t, registry(t, Registrations()), "<!-- mdchk:enable MD059 -->\n"+md059Example, s, engine.DocumentContext{}, engine.RunOptions{})
			want := 0
			if allow {
				want = 1
			}
			if !out.Complete || len(out.Diagnostics) != want || len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(out)
			}
			if allow && (out.Diagnostics[0].Range != (result.Range{Start: result.Position{Line: 2, Column: 2}, End: result.Position{Line: 2, Column: 12}})) {
				t.Fatal(out)
			}
		})
	}
}

func TestMD059UnknownContextWithholdsObservation(t *testing.T) {
	for _, audit := range []bool{false, true} {
		t.Run(fmt.Sprintf("audit-%v", audit), func(t *testing.T) {
			s := onlyMD059Settings()
			source := md059Example + "\n\n<div>\n<x title=\"\n\n" + md059Example
			if audit {
				on := true
				s["MD087"] = engine.RuleSetting{Enabled: &on}
				source = "<!-- mdchk:disable MD059 -->\n" + source
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
				if c.Rule == "MD059" && c.Message != "Link label context is unavailable; the complete MD059 inventory is withheld." {
					t.Fatal(c)
				}
			}
		})
	}
}

func TestMD059UnavailableInventoryOutcome(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			called := false
			r := registry(t, []engine.Registration{{ID: "MD059", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				called = true
				items, err := c.LinkLabels(ctx)
				if err != nil || items.Availability != parser.Available || len(items.Items) != 1 || items.Items[0].Text != "Click here" {
					t.Fatal("actual positive prerequisite", items, err)
				}
				out := linkLabelOutcome(ctx, c, parser.LinkLabelsInventory{Availability: parser.Unavailable, Items: items.Items})
				if out.Err != nil || out.Findings != nil || out.IncompleteReason != "Link label context is unavailable; the complete MD059 inventory is withheld." {
					t.Fatal(out)
				}
				for _, availability := range []parser.Availability{"", "partial"} {
					if bad := linkLabelOutcome(ctx, c, parser.LinkLabelsInventory{Availability: availability, Items: items.Items}); !errors.Is(bad.Err, parser.ErrModel) || bad.Findings != nil {
						t.Fatal(bad)
					}
				}
				return out
			}}})
			out := analyzeHeadings(t, r, md059Example, onlyMD059Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			if !called || out.Complete || len(out.Diagnostics)+len(out.Errors) != 0 || len(out.Coverage) != 1 || out.Coverage[0].Rule != "MD059" || out.Coverage[0].Reason != result.ContextUnavailable || out.Coverage[0].Message != "Link label context is unavailable; the complete MD059 inventory is withheld." {
				t.Fatal(out)
			}
		})
	}
}

func TestMD059ReportOnlyLeavesSourceAndPlansNothing(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"", "project"} {
			t.Run(flavor+"/"+mode, func(t *testing.T) {
				source := md059Example + "\n\n[more][]\n\n[more]: more.md\n"
				caller := []byte(source)
				s := onlyMD059Settings()
				outcomes := 0
				r := registry(t, []engine.Registration{{ID: "MD059", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					out := uninformativeLinkLabels(ctx, c)
					if out.Err != nil || len(out.Findings) != 2 || out.IncompleteReason != "" {
						t.Fatal(out)
					}
					for _, f := range out.Findings {
						if f.FixEligible {
							t.Fatal("MD059 finding is fix-eligible", f)
						}
					}
					outcomes++
					return out
				}}})
				for _, reg := range []*engine.Registry{r, registry(t, Registrations())} {
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
						if d.Rule != "MD059" || d.FixAvailable {
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

func TestMD059DoesNotExecuteProtectedDirectives(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			s := onlyMD059Settings()
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
			source := "```md\n<!-- mdchk:disable MD059 -->\n" + md059Example + "\n```\n\n" + md059Example
			in, err := plan.NewInput("literal.md", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{})
			if err != nil || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD059" || out.Diagnostics[0].Range.Start.Line != 6 || len(trace.Directives().Candidates()) != 0 || len(out.Errors)+len(out.Coverage)+len(out.SuppressedDiagnostics) != 0 {
				t.Fatal(out, err)
			}
		})
	}
}

func TestMD059EmptyPhrasesReportNothingWithoutDeficit(t *testing.T) {
	source := "[here](u) [more][c] [c][] [c] [click here](u) [Read more](u)\n\n[c]: v\n"
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			s := onlyMD059Settings()
			on := true
			s["MD059"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"phrases": []string{}}}
			out := analyzeHeadings(t, registry(t, Registrations()), source, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics)+len(out.Errors)+len(out.Coverage)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
				t.Fatal(out)
			}
			s["MD059"] = engine.RuleSetting{Enabled: &on}
			if out = analyzeHeadings(t, registry(t, Registrations()), source, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{}); !out.Complete || len(out.Diagnostics) != 4 {
				t.Fatal("default phrases precondition", out)
			}
		})
	}
}
