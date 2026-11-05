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

const md055Example = "A | B\n--- | ---\n| 1 | 2 |\n"

const md055FixtureSHA = "5439144ca04395f31749913a620651b8c6c82ca67d33b4f729533df89b1dbefa"

func TestMD055RegistrationAvailabilityAndOptions(t *testing.T) {
	registered := false
	for _, registration := range Registrations() {
		registered = registered || registration.ID == "MD055"
	}
	r := registry(t, Registrations())
	if !registered || !r.Available("MD055") {
		t.Fatal("MD055 registration", registered)
	}
	s, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil || !s["MD055"].Available || !s["MD055"].Enabled || len(s["MD055"].Options) != 1 || s["MD055"].Options["style"] != "consistent" {
		t.Fatal(s["MD055"], err)
	}
	found := false
	for _, meta := range r.Metadata() {
		if meta.ID == "MD055" {
			found = true
			if !meta.DefaultEnabled || meta.Fix != engine.PlannedFix || !meta.RequiresSyntax || len(meta.Options) != 1 {
				t.Fatal(meta)
			}
		}
	}
	if !found {
		t.Fatal("MD055 metadata missing")
	}
	for _, mapping := range []map[string]any{{"unknown": 1}, {"styles": "none"}, {"enabled": "yes"}, {"allow_directives": "false"}, {"style": "both"}, {"style": 1}, {"style": nil}, {"style": []any{"none"}}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD055": setting}, engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal("invalid MD055 setting admitted", mapping)
		}
	}
}

type md055Literal struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Options      struct {
		Enabled *bool
		Style   *string
	}
	Findings []md055LiteralFinding
}

type md055LiteralFinding struct {
	Bytes, Start, End [2]int
	Message           string
}

func md055Literals(t *testing.T) []md055Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md055.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != md055FixtureSHA {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md055Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 38 {
		t.Fatal(e, len(v.Cases))
	}
	return v.Cases
}

func md055Settings() engine.Settings {
	s := md059Settings()
	off, on := false, true
	s["MD059"] = engine.RuleSetting{Enabled: &off}
	s["MD055"] = engine.RuleSetting{Enabled: &on}
	return s
}

func md055LiteralSettings(tc md055Literal) engine.Settings {
	s := md055Settings()
	setting := s["MD055"]
	if tc.Options.Enabled != nil {
		setting.Enabled = tc.Options.Enabled
	}
	if tc.Options.Style != nil {
		setting.Options = engine.Options{"style": *tc.Options.Style}
	}
	s["MD055"] = setting
	return s
}

func md055MessageValid(message string) bool {
	for _, kind := range []string{"Missing", "Unexpected"} {
		for _, edge := range []string{"leading", "trailing"} {
			for _, convention := range []string{"leading and trailing pipes", "a leading pipe only", "a trailing pipe only", "no outer pipes"} {
				if message == kind+" "+edge+" pipe; expected "+convention+"." {
					return true
				}
			}
		}
	}
	return false
}

func md055AssertFindings(t *testing.T, source string, ds []result.Diagnostic, want []md055LiteralFinding) {
	t.Helper()
	if len(ds) != len(want) {
		t.Fatal(ds, want)
	}
	for i, d := range ds {
		w := want[i]
		if !md055MessageValid(w.Message) {
			t.Fatal("fixture message", w)
		}
		// Unexpected pipes cover the pipe byte; missing pipes are insertion points.
		if strings.HasPrefix(w.Message, "Unexpected") && (w.Bytes[1] != w.Bytes[0]+1 || source[w.Bytes[0]] != '|') || strings.HasPrefix(w.Message, "Missing") && w.Bytes[1] != w.Bytes[0] {
			t.Fatal("fixture range", w)
		}
		if d.Rule != "MD055" || d.Message != w.Message || d.FixAvailable || d.ByteRange.Start != w.Bytes[0] || d.ByteRange.End != w.Bytes[1] || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] {
			t.Fatal(d, w)
		}
	}
}

func TestMD055FrozenProductionOracle(t *testing.T) {
	for _, tc := range md055Literals(t) {
		for _, flavor := range tc.Flavors {
			for _, inspect := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/inspect-%v", tc.Name, flavor, inspect), func(t *testing.T) {
					if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
						t.Fatal("source identity")
					}
					selection, e := registry(t, Registrations()).SelectRules(md055LiteralSettings(tc), engine.ValidationProviders{})
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
					md055AssertFindings(t, tc.Source, out.Diagnostics, tc.Findings)
					for _, d := range out.Diagnostics {
						if d.Path != "literal.md" || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}) {
							t.Fatal(d)
						}
					}
					if inspect {
						found := false
						for _, ev := range trace.Evaluations() {
							if ev.Rule == "MD055" {
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

func TestMD055FixtureCoversLineEndingsAndOptions(t *testing.T) {
	names := map[string]md055Literal{}
	for _, tc := range md055Literals(t) {
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
			if strings.HasPrefix(w.Message, "Missing trailing") && tc.Source[w.Bytes[1]:w.Bytes[1]+1] == "\n" {
				t.Fatal("missing trailing pipe after CR", name, w)
			}
		}
	}
	for _, name := range []string{"spec-example", "spec-accepted", "spec-example-crlf", "consistent-explicit", "leading-and-trailing-missing", "none-fully-piped", "consistent-both", "consistent-leading-only", "consistent-trailing-only", "consistent-neither", "escaped-pipe-edge", "code-span-pipe", "empty-edge-cells", "body-cell-counts", "block-quote", "list-item", "fenced-code", "html-block", "no-delimiter-row", "trailing-spaces", "non-ascii", "multiple-tables", "commonmark-table", "disabled"} {
		if _, ok := names[name]; !ok {
			t.Fatal("required fixture case missing", name)
		}
	}
}

func TestMD055LineEndingsShareRanges(t *testing.T) {
	for _, tc := range md055Literals(t) {
		if strings.Contains(tc.Source, "\r") {
			continue
		}
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				source := strings.ReplaceAll(tc.Source, "\n", "\r\n")
				out := analyzeHeadings(t, registry(t, Registrations()), source, md055LiteralSettings(tc), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Errors)+len(out.Coverage) != 0 {
					t.Fatal(out)
				}
				want := make([]md055LiteralFinding, len(tc.Findings))
				for i, w := range tc.Findings {
					w.Bytes[0] += strings.Count(tc.Source[:w.Bytes[0]], "\n")
					w.Bytes[1] += strings.Count(tc.Source[:w.Bytes[1]], "\n")
					want[i] = w
				}
				md055AssertFindings(t, source, out.Diagnostics, want)
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

func TestMD055SuppressionInactiveAndUnused(t *testing.T) {
	for _, mode := range []string{"used", "denied", "unused", "inactive"} {
		t.Run(mode, func(t *testing.T) {
			source := "<!-- mdchk:disable MD055 -->\n" + md055Example
			if mode == "unused" {
				source = "<!-- mdchk:disable MD055 -->\nA | B\n--- | ---\n1 | 2\n"
			}
			if mode == "inactive" {
				source = md055Example
			}
			s := md055Settings()
			on, allow, enabled := true, mode != "denied", mode != "inactive"
			s["MD055"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow}
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
				case "MD055":
					visible++
					if d.Range.Start.Line != 4 || d.FixAvailable {
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
					if observation.Diagnostic.Rule == "MD055" {
						ranges = append(ranges, observation.Diagnostic.ByteRange)
					}
				}
				if !reflect.DeepEqual(ranges, []result.ByteRange{{Start: 16, End: 17}, {Start: 24, End: 25}}) {
					t.Fatal("MD055 inactive observations", ranges)
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
				want := []result.ByteRange{{Start: 45, End: 46}, {Start: 53, End: 54}}[i]
				if hidden.Diagnostic.Rule != "MD055" || hidden.Diagnostic.ByteRange != want || hidden.Diagnostic.Range.Start.Line != 4 || hidden.SuppressedBy.Kind != "directive" {
					t.Fatal(hidden)
				}
			}
		})
	}
}

func TestMD055CommonMarkIsInapplicable(t *testing.T) {
	source := "| A | B |\n| --- | --- |\n1 | 2\n"
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			out := analyzeHeadings(t, registry(t, Registrations()), source, md055Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
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

func TestMD055UnavailableInventoryOutcome(t *testing.T) {
	called := false
	r := registry(t, []engine.Registration{{ID: "MD055", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
		called = true
		rows, err := c.TableOuterPipes(ctx)
		if err != nil || rows.Availability != parser.Available || len(rows.Rows) != 3 {
			t.Fatal("actual positive prerequisite", rows, err)
		}
		out := tableOuterPipeOutcome(ctx, c, parser.TableOuterPipesInventory{Availability: parser.Unavailable, Rows: rows.Rows})
		if out.Err != nil || out.Findings != nil || out.IncompleteReason != "Table row context is unavailable; the complete MD055 inventory is withheld." {
			t.Fatal(out)
		}
		for _, availability := range []parser.Availability{"", "partial"} {
			if bad := tableOuterPipeOutcome(ctx, c, parser.TableOuterPipesInventory{Availability: availability, Rows: rows.Rows}); !errors.Is(bad.Err, parser.ErrModel) || bad.Findings != nil {
				t.Fatal(bad)
			}
		}
		return out
	}}})
	out := analyzeHeadings(t, r, md055Example, md055Settings(), engine.DocumentContext{Flavor: "gfm"}, engine.RunOptions{})
	if !called || out.Complete || len(out.Diagnostics)+len(out.Errors) != 0 || len(out.Coverage) != 1 || out.Coverage[0].Rule != "MD055" || out.Coverage[0].Reason != result.ContextUnavailable || out.Coverage[0].Message != "Table row context is unavailable; the complete MD055 inventory is withheld." {
		t.Fatal(out)
	}
}

func TestMD055ReportOnlyLeavesSourceAndPlansNothing(t *testing.T) {
	for _, mode := range []string{"", "project"} {
		t.Run(mode, func(t *testing.T) {
			source := md055Example + "\n| C | D |\n| --- | --- |\n3 | 4\n"
			caller := []byte(source)
			s := md055Settings()
			outcomes := 0
			r := registry(t, []engine.Registration{{ID: "MD055", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				out := outerTablePipes(ctx, c)
				if out.Err != nil || len(out.Findings) != 4 || out.IncompleteReason != "" {
					t.Fatal(out)
				}
				for _, f := range out.Findings {
					if f.FixEligible {
						t.Fatal("MD055 finding is fix-eligible", f)
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
				if err != nil || !out.Complete || len(out.Diagnostics) != 4 || string(caller) != source || string(in.Source().Bytes()) != source {
					t.Fatal(out, err)
				}
				if len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 || len(out.Files) != 1 || len(out.Files[0].ChangeIDs) != 0 || out.Files[0].WriteStatus != "not-requested" {
					t.Fatal(out)
				}
				for _, d := range out.Diagnostics {
					if d.Rule != "MD055" || d.FixAvailable {
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

func TestMD055DoesNotExecuteProtectedDirectives(t *testing.T) {
	s := md055Settings()
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
	source := "```md\n<!-- mdchk:disable MD055 -->\n```\n\n" + md055Example
	in, err := plan.NewInput("literal.md", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{})
	if err != nil || !out.Complete || len(out.Diagnostics) != 2 || out.Diagnostics[0].Rule != "MD055" || out.Diagnostics[0].Range.Start.Line != 7 || len(trace.Directives().Candidates()) != 0 || len(out.Errors)+len(out.Coverage)+len(out.SuppressedDiagnostics) != 0 {
		t.Fatal(out, err)
	}
}
