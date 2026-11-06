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

const md060Example = "| A| B |\n|---|---|\n|1|2|\n"

const md060FixtureSHA = "4dc4a37c8333402b1a9c5fe63b32e0b8caed5217b56909aa29ff433d08bd5837"

func TestMD060RegistrationAvailabilityAndOptions(t *testing.T) {
	registered := false
	for _, registration := range Registrations() {
		registered = registered || registration.ID == "MD060"
	}
	r := registry(t, Registrations())
	if !registered || !r.Available("MD060") {
		t.Fatal("MD060 registration", registered)
	}
	s, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil || !s["MD060"].Available || s["MD060"].Enabled || len(s["MD060"].Options) != 1 || s["MD060"].Options["style"] != "aligned" {
		t.Fatal(s["MD060"], err)
	}
	found := false
	for _, meta := range r.Metadata() {
		if meta.ID == "MD060" {
			found = true
			if meta.DefaultEnabled || meta.Fix != engine.PlannedFix || !meta.RequiresSyntax || len(meta.Options) != 1 {
				t.Fatal(meta)
			}
		}
	}
	if !found {
		t.Fatal("MD060 metadata missing")
	}
	for _, mapping := range []map[string]any{{"unknown": 1}, {"styles": "tight"}, {"enabled": "yes"}, {"allow_directives": "false"}, {"style": "wide"}, {"style": 1}, {"style": nil}, {"style": []any{"tight"}}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD060": setting}, engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal("invalid MD060 setting admitted", mapping)
		}
	}
}

type md060Literal struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Options      struct {
		Enabled *bool
		Style   *string
	}
	Findings []md060LiteralFinding
}

type md060LiteralFinding struct {
	Bytes, Start, End [2]int
	Message           string
}

func md060Literals(t *testing.T) []md060Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md060.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != md060FixtureSHA {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md060Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 46 {
		t.Fatal(e, len(v.Cases))
	}
	return v.Cases
}

func md060Settings() engine.Settings {
	s := onlyMD056Settings()
	off, on := false, true
	s["MD056"] = engine.RuleSetting{Enabled: &off}
	s["MD058"] = engine.RuleSetting{Enabled: &off}
	s["MD060"] = engine.RuleSetting{Enabled: &on}
	return s
}

func md060LiteralSettings(tc md060Literal) engine.Settings {
	s := md060Settings()
	setting := s["MD060"]
	if tc.Options.Enabled != nil {
		setting.Enabled = tc.Options.Enabled
	}
	if tc.Options.Style != nil {
		setting.Options = engine.Options{"style": *tc.Options.Style}
	}
	s["MD060"] = setting
	return s
}

func md060MessageValid(message string) bool {
	if message == "Cell padding does not match the compact style." || message == "Cell padding does not match the tight style." {
		return true
	}
	width, ok := strings.CutPrefix(message, "Cell padding does not match the aligned style; column width is ")
	width, ok2 := strings.CutSuffix(width, ".")
	return ok && ok2 && width != "" && strings.Trim(width, "0123456789") == "" && (width == "0" || width[0] != '0')
}

func md060AssertFindings(t *testing.T, source string, ds []result.Diagnostic, want []md060LiteralFinding) {
	t.Helper()
	if len(ds) != len(want) {
		t.Fatal(ds, want)
	}
	for i, d := range ds {
		w := want[i]
		if !md060MessageValid(w.Message) {
			t.Fatal("fixture message", w)
		}
		// A finding covers one field on one line and never holds a line ending.
		if strings.ContainsAny(source[w.Bytes[0]:w.Bytes[1]], "\r\n") || w.Start[0] != w.End[0] {
			t.Fatal("fixture range", w)
		}
		if d.Rule != "MD060" || d.Message != w.Message || d.FixAvailable || d.ByteRange.Start != w.Bytes[0] || d.ByteRange.End != w.Bytes[1] || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] {
			t.Fatal(d, w)
		}
	}
}

func TestMD060FrozenProductionOracle(t *testing.T) {
	for _, tc := range md060Literals(t) {
		for _, flavor := range tc.Flavors {
			for _, inspect := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/inspect-%v", tc.Name, flavor, inspect), func(t *testing.T) {
					if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
						t.Fatal("source identity")
					}
					selection, e := registry(t, Registrations()).SelectRules(md060LiteralSettings(tc), engine.ValidationProviders{})
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
					md060AssertFindings(t, tc.Source, out.Diagnostics, tc.Findings)
					for _, d := range out.Diagnostics {
						if d.Path != "literal.md" || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}) {
							t.Fatal(d)
						}
					}
					if inspect {
						found := false
						for _, ev := range trace.Evaluations() {
							if ev.Rule == "MD060" {
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
}

func TestMD060FixtureCoversLineEndingsAndCases(t *testing.T) {
	names := map[string]md060Literal{}
	for _, tc := range md060Literals(t) {
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
			if strings.Contains(tc.Source[w.Bytes[0]:w.Bytes[1]], "\r") || w.Bytes[1] > 0 && tc.Source[w.Bytes[1]-1] == '\r' {
				t.Fatal("finding includes the CRLF", name, w)
			}
		}
	}
	for _, name := range []string{"spec-example", "spec-example-aligned", "spec-example-compact", "spec-example-tight", "spec-accepted", "spec-accepted-compact", "spec-accepted-tight", "spec-example-crlf", "empty-cells-aligned", "empty-cells-compact", "empty-cells-tight", "no-outer-pipes", "no-outer-pipes-misaligned", "leading-only", "trailing-only", "escaped-pipe", "escaped-pipe-width", "code-span-pipe", "multibyte", "multibyte-accepted", "multibyte-crlf", "unequal-body-widths", "excess-and-missing-cells", "short-row-accepted", "tab-padding", "tab-padding-compact", "right-and-center", "block-quote", "list-item", "fenced-code", "indented-code", "html-block", "no-delimiter-row", "multiple-tables", "commonmark-table", "disabled"} {
		if _, ok := names[name]; !ok {
			t.Fatal("required fixture case missing", name)
		}
	}
}

func TestMD060LineEndingsShareRanges(t *testing.T) {
	for _, tc := range md060Literals(t) {
		if strings.Contains(tc.Source, "\r") {
			continue
		}
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				source := strings.ReplaceAll(tc.Source, "\n", "\r\n")
				out := analyzeHeadings(t, registry(t, Registrations()), source, md060LiteralSettings(tc), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Errors)+len(out.Coverage) != 0 {
					t.Fatal(out)
				}
				want := make([]md060LiteralFinding, len(tc.Findings))
				for i, w := range tc.Findings {
					w.Bytes[0] += strings.Count(tc.Source[:w.Bytes[0]], "\n")
					w.Bytes[1] += strings.Count(tc.Source[:w.Bytes[1]], "\n")
					want[i] = w
				}
				md060AssertFindings(t, source, out.Diagnostics, want)
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

func TestMD060SuppressionInactiveAndUnused(t *testing.T) {
	for _, mode := range []string{"used", "denied", "unused", "inactive"} {
		t.Run(mode, func(t *testing.T) {
			source := "<!-- mdchk:disable MD060 -->\n" + md060Example
			if mode == "unused" {
				source = "<!-- mdchk:disable MD060 -->\n| A   | B   |\n| --- | --- |\n| 1   | 2   |\n"
			}
			if mode == "inactive" {
				source = md060Example
			}
			s := md060Settings()
			on, allow, enabled := true, mode != "denied", mode != "inactive"
			s["MD060"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow}
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
				case "MD060":
					visible++
					if d.Range.Start.Line < 2 || d.FixAvailable {
						t.Fatal(d)
					}
				case "MD087":
					audit++
				default:
					t.Fatal(d)
				}
			}
			wantVisible, wantHidden, wantAudit := 0, 6, 0
			if mode == "denied" {
				wantVisible, wantHidden = 6, 0
			}
			if mode == "unused" {
				wantHidden, wantAudit = 0, 1
			}
			if mode == "inactive" {
				var ranges []result.ByteRange
				for _, observation := range trace.Suppressed() {
					if observation.Diagnostic.Rule == "MD060" {
						ranges = append(ranges, observation.Diagnostic.ByteRange)
					}
				}
				if !reflect.DeepEqual(ranges, []result.ByteRange{{Start: 1, End: 3}, {Start: 4, End: 7}, {Start: 10, End: 13}, {Start: 14, End: 17}, {Start: 20, End: 21}, {Start: 22, End: 23}}) {
					t.Fatal("MD060 inactive observations", ranges)
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
				want := []result.ByteRange{{Start: 30, End: 32}, {Start: 33, End: 36}, {Start: 39, End: 42}, {Start: 43, End: 46}, {Start: 49, End: 50}, {Start: 51, End: 52}}[i]
				if hidden.Diagnostic.Rule != "MD060" || hidden.Diagnostic.ByteRange != want || hidden.Diagnostic.Range.Start.Line != 2+i/2 || hidden.SuppressedBy.Kind != "directive" {
					t.Fatal(hidden)
				}
			}
		})
	}
}

func TestMD060CommonMarkIsInapplicable(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			out := analyzeHeadings(t, registry(t, Registrations()), md060Example, md060Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			want := 6
			if flavor == "commonmark" {
				want = 0
			}
			if !out.Complete || len(out.Diagnostics) != want || len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(out)
			}
		})
	}
}

func TestMD060UnavailableInventoryOutcome(t *testing.T) {
	called := false
	r := registry(t, []engine.Registration{{ID: "MD060", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
		called = true
		tables, err := c.TableLayout(ctx)
		if err != nil || tables.Availability != parser.Available || len(tables.Tables) != 1 {
			t.Fatal("actual positive prerequisite", tables, err)
		}
		out := tableLayoutOutcome(ctx, c, parser.TableLayoutInventory{Availability: parser.Unavailable, Tables: tables.Tables})
		if out.Err != nil || out.Findings != nil || out.IncompleteReason != "Table row context is unavailable; the complete MD060 inventory is withheld." {
			t.Fatal(out)
		}
		for _, availability := range []parser.Availability{"", "partial"} {
			if bad := tableLayoutOutcome(ctx, c, parser.TableLayoutInventory{Availability: availability, Tables: tables.Tables}); !errors.Is(bad.Err, parser.ErrModel) || bad.Findings != nil {
				t.Fatal(bad)
			}
		}
		return out
	}}})
	out := analyzeHeadings(t, r, md060Example, md060Settings(), engine.DocumentContext{Flavor: "gfm"}, engine.RunOptions{})
	if !called || out.Complete || len(out.Diagnostics)+len(out.Errors) != 0 || len(out.Coverage) != 1 || out.Coverage[0].Rule != "MD060" || out.Coverage[0].Reason != result.ContextUnavailable || out.Coverage[0].Message != "Table row context is unavailable; the complete MD060 inventory is withheld." {
		t.Fatal(out)
	}
}

func TestMD060ReportOnlyLeavesSourceAndPlansNothing(t *testing.T) {
	for _, mode := range []string{"", "project"} {
		t.Run(mode, func(t *testing.T) {
			source := md060Example + "\n| C | D |\n| --- | --- |\n|3 | 4 |\n"
			caller := []byte(source)
			s := md060Settings()
			outcomes := 0
			r := registry(t, []engine.Registration{{ID: "MD060", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				out := tableColumnLayout(ctx, c)
				if out.Err != nil || len(out.Findings) != 10 || out.IncompleteReason != "" {
					t.Fatal(out)
				}
				for _, f := range out.Findings {
					if f.FixEligible {
						t.Fatal("MD060 finding is fix-eligible", f)
					}
				}
				outcomes++
				return out
			}}})
			for _, reg := range []*engine.Registry{r, registry(t, Registrations())} {
				in, err := engine.NewInput("literal.md", caller, s, engine.DocumentContext{Flavor: "gfm"})
				if err != nil {
					t.Fatal(err)
				}
				out, err := reg.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Mode: mode})
				if err != nil || !out.Complete || len(out.Diagnostics) != 10 || string(caller) != source || string(in.Source().Bytes()) != source {
					t.Fatal(out, err)
				}
				if len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 || len(out.Files) != 1 || len(out.Files[0].ChangeIDs) != 0 || out.Files[0].WriteStatus != "not-requested" {
					t.Fatal(out)
				}
				for _, d := range out.Diagnostics {
					if d.Rule != "MD060" || d.FixAvailable {
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

func TestMD060DoesNotExecuteProtectedDirectives(t *testing.T) {
	s := md060Settings()
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
	source := "```md\n<!-- mdchk:disable MD060 -->\n```\n\n" + md060Example
	in, err := plan.NewInput("literal.md", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{})
	if err != nil || !out.Complete || len(out.Diagnostics) != 6 || out.Diagnostics[0].Rule != "MD060" || out.Diagnostics[0].Range.Start.Line != 5 || len(trace.Directives().Candidates()) != 0 || len(out.Errors)+len(out.Coverage)+len(out.SuppressedDiagnostics) != 0 {
		t.Fatal(out, err)
	}
}
