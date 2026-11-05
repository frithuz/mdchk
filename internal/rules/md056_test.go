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

const md056Example = "A | B\n--- | ---\n1 | 2 | 3\n"

const md056FixtureSHA = "99f108a6a78b9078e645d3c73b2697dec3c812958b3c124680b3b8dc4ff55dae"

func TestMD056RegistrationAvailabilityAndOptions(t *testing.T) {
	registered := false
	for _, registration := range Registrations() {
		registered = registered || registration.ID == "MD056"
	}
	r := registry(t, Registrations())
	if !registered || !r.Available("MD056") {
		t.Fatal("MD056 registration", registered)
	}
	s, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil || !s["MD056"].Available || !s["MD056"].Enabled || len(s["MD056"].Options) != 0 {
		t.Fatal(s["MD056"], err)
	}
	found := false
	for _, meta := range r.Metadata() {
		if meta.ID == "MD056" {
			found = true
			if !meta.DefaultEnabled || meta.Fix != engine.NoFix || !meta.RequiresSyntax || len(meta.Options) != 0 {
				t.Fatal(meta)
			}
		}
	}
	if !found {
		t.Fatal("MD056 metadata missing")
	}
	for _, mapping := range []map[string]any{{"unknown": 1}, {"style": "x"}, {"enabled": "yes"}, {"allow_directives": "false"}, {"enabled": nil}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD056": setting}, engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal("invalid MD056 setting admitted", mapping)
		}
	}
}

type md056Literal struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Options      struct{ Enabled *bool }
	Findings     []md056LiteralFinding
}

type md056LiteralFinding struct {
	Bytes, Start, End [2]int
	Message           string
}

func md056Literals(t *testing.T) []md056Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md056.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != md056FixtureSHA {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md056Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 27 {
		t.Fatal(e, len(v.Cases))
	}
	return v.Cases
}

func md056Settings() engine.Settings {
	s := md055Settings()
	off, on := false, true
	s["MD055"] = engine.RuleSetting{Enabled: &off}
	s["MD056"] = engine.RuleSetting{Enabled: &on}
	return s
}

func md056LiteralSettings(tc md056Literal) engine.Settings {
	s := md056Settings()
	if tc.Options.Enabled != nil {
		s["MD056"] = engine.RuleSetting{Enabled: tc.Options.Enabled}
	}
	return s
}

func md056AssertFindings(t *testing.T, source string, ds []result.Diagnostic, want []md056LiteralFinding) {
	t.Helper()
	if len(ds) != len(want) {
		t.Fatal(ds, want)
	}
	for i, d := range ds {
		w := want[i]
		// A finding covers its trimmed row: never empty, never edge blanks or a line ending.
		row := source[w.Bytes[0]:w.Bytes[1]]
		if row == "" || strings.ContainsAny(row, "\r\n") || strings.TrimLeft(row, " \t") != row || strings.TrimRight(row, " \t") != row || !strings.HasPrefix(w.Message, "Row has ") {
			t.Fatal("fixture range", w)
		}
		if d.Rule != "MD056" || d.Message != w.Message || d.FixAvailable || d.ByteRange.Start != w.Bytes[0] || d.ByteRange.End != w.Bytes[1] || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] {
			t.Fatal(d, w)
		}
	}
}

func TestMD056FrozenProductionOracle(t *testing.T) {
	for _, tc := range md056Literals(t) {
		for _, flavor := range tc.Flavors {
			for _, inspect := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/inspect-%v", tc.Name, flavor, inspect), func(t *testing.T) {
					if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
						t.Fatal("source identity")
					}
					selection, e := registry(t, Registrations()).SelectRules(md056LiteralSettings(tc), engine.ValidationProviders{})
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
					md056AssertFindings(t, tc.Source, out.Diagnostics, tc.Findings)
					for _, d := range out.Diagnostics {
						if d.Path != "literal.md" || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}) {
							t.Fatal(d)
						}
					}
					if inspect {
						found := false
						for _, ev := range trace.Evaluations() {
							if ev.Rule == "MD056" {
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

func TestMD056FixtureCoversLineEndingsAndCases(t *testing.T) {
	names := map[string]md056Literal{}
	for _, tc := range md056Literals(t) {
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
			if tc.Source[w.Bytes[1]-1] == '\r' || tc.Source[w.Bytes[1]] == '\n' {
				t.Fatal("finding includes or splits the CRLF", name, w)
			}
		}
	}
	for _, name := range []string{"spec-example", "spec-accepted", "code-span-pipe", "escaped-pipe", "spec-example-crlf", "short-row", "long-row", "header-delimiter-mismatch", "block-quote", "list-item", "fenced-code", "indented-trailing-spaces", "non-ascii", "multiple-tables", "commonmark-table", "disabled"} {
		if _, ok := names[name]; !ok {
			t.Fatal("required fixture case missing", name)
		}
	}
}

func TestMD056LineEndingsShareRanges(t *testing.T) {
	for _, tc := range md056Literals(t) {
		if strings.Contains(tc.Source, "\r") {
			continue
		}
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				source := strings.ReplaceAll(tc.Source, "\n", "\r\n")
				out := analyzeHeadings(t, registry(t, Registrations()), source, md056LiteralSettings(tc), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Errors)+len(out.Coverage) != 0 {
					t.Fatal(out)
				}
				want := make([]md056LiteralFinding, len(tc.Findings))
				for i, w := range tc.Findings {
					w.Bytes[0] += strings.Count(tc.Source[:w.Bytes[0]], "\n")
					w.Bytes[1] += strings.Count(tc.Source[:w.Bytes[1]], "\n")
					want[i] = w
				}
				md056AssertFindings(t, source, out.Diagnostics, want)
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

func TestMD056SuppressionInactiveAndUnused(t *testing.T) {
	for _, mode := range []string{"used", "denied", "unused", "inactive"} {
		t.Run(mode, func(t *testing.T) {
			source := "<!-- mdchk:disable MD056 -->\n" + md056Example
			if mode == "unused" {
				source = "<!-- mdchk:disable MD056 -->\nA | B\n--- | ---\n1 | 2\n"
			}
			if mode == "inactive" {
				source = md056Example
			}
			s := md056Settings()
			on, allow, enabled := true, mode != "denied", mode != "inactive"
			s["MD056"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow}
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
				case "MD056":
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
			wantVisible, wantHidden, wantAudit := 0, 1, 0
			if mode == "denied" {
				wantVisible, wantHidden = 1, 0
			}
			if mode == "unused" {
				wantHidden, wantAudit = 0, 1
			}
			if mode == "inactive" {
				var ranges []result.ByteRange
				for _, observation := range trace.Suppressed() {
					if observation.Diagnostic.Rule == "MD056" {
						ranges = append(ranges, observation.Diagnostic.ByteRange)
					}
				}
				if !reflect.DeepEqual(ranges, []result.ByteRange{{Start: 16, End: 25}}) {
					t.Fatal("MD056 inactive observations", ranges)
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
				if hidden.Diagnostic.Rule != "MD056" || hidden.Diagnostic.ByteRange != (result.ByteRange{Start: 45, End: 54}) || hidden.Diagnostic.Range.Start.Line != 4 || hidden.SuppressedBy.Kind != "directive" {
					t.Fatal(hidden)
				}
			}
		})
	}
}

func TestMD056CommonMarkIsInapplicable(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			out := analyzeHeadings(t, registry(t, Registrations()), md056Example, md056Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			want := 1
			if flavor == "commonmark" {
				want = 0
			}
			if !out.Complete || len(out.Diagnostics) != want || len(out.Errors)+len(out.Coverage) != 0 {
				t.Fatal(out)
			}
		})
	}
}

func TestMD056UnavailableInventoryOutcome(t *testing.T) {
	called := false
	r := registry(t, []engine.Registration{{ID: "MD056", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
		called = true
		rows, err := c.TableCellCounts(ctx)
		if err != nil || rows.Availability != parser.Available || len(rows.Rows) != 1 {
			t.Fatal("actual positive prerequisite", rows, err)
		}
		out := tableCellCountOutcome(ctx, c, parser.TableCellCountsInventory{Availability: parser.Unavailable, Rows: rows.Rows})
		if out.Err != nil || out.Findings != nil || out.IncompleteReason != "Table row context is unavailable; the complete MD056 inventory is withheld." {
			t.Fatal(out)
		}
		for _, availability := range []parser.Availability{"", "partial"} {
			if bad := tableCellCountOutcome(ctx, c, parser.TableCellCountsInventory{Availability: availability, Rows: rows.Rows}); !errors.Is(bad.Err, parser.ErrModel) || bad.Findings != nil {
				t.Fatal(bad)
			}
		}
		return out
	}}})
	out := analyzeHeadings(t, r, md056Example, md056Settings(), engine.DocumentContext{Flavor: "gfm"}, engine.RunOptions{})
	if !called || out.Complete || len(out.Diagnostics)+len(out.Errors) != 0 || len(out.Coverage) != 1 || out.Coverage[0].Rule != "MD056" || out.Coverage[0].Reason != result.ContextUnavailable || out.Coverage[0].Message != "Table row context is unavailable; the complete MD056 inventory is withheld." {
		t.Fatal(out)
	}
}

func TestMD056ReportOnlyLeavesSourceAndPlansNothing(t *testing.T) {
	for _, mode := range []string{"", "project"} {
		t.Run(mode, func(t *testing.T) {
			source := md056Example + "\n| C | D |\n| --- | --- |\n| 3 |\n"
			caller := []byte(source)
			s := md056Settings()
			outcomes := 0
			r := registry(t, []engine.Registration{{ID: "MD056", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				out := mismatchedTableCells(ctx, c)
				if out.Err != nil || len(out.Findings) != 2 || out.IncompleteReason != "" {
					t.Fatal(out)
				}
				for _, f := range out.Findings {
					if f.FixEligible {
						t.Fatal("MD056 finding is fix-eligible", f)
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
				if err != nil || !out.Complete || len(out.Diagnostics) != 2 || string(caller) != source || string(in.Source().Bytes()) != source {
					t.Fatal(out, err)
				}
				if len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 || len(out.Files) != 1 || len(out.Files[0].ChangeIDs) != 0 || out.Files[0].WriteStatus != "not-requested" {
					t.Fatal(out)
				}
				for _, d := range out.Diagnostics {
					if d.Rule != "MD056" || d.FixAvailable {
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

func TestMD056DoesNotExecuteProtectedDirectives(t *testing.T) {
	s := md056Settings()
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
	source := "```md\n<!-- mdchk:disable MD056 -->\n```\n\n" + md056Example
	in, err := plan.NewInput("literal.md", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{})
	if err != nil || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD056" || out.Diagnostics[0].Range.Start.Line != 7 || len(trace.Directives().Candidates()) != 0 || len(out.Errors)+len(out.Coverage)+len(out.SuppressedDiagnostics) != 0 {
		t.Fatal(out, err)
	}
}
