package md075_test

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
)

const md075Example = "A | B\n1 | 2\n"

const md075FixtureSHA = "ab1f0cdd6c5e51fea5e3d7a9ea02bc4417c9d1ca264b2a5e26cbed3afa12b497"

func TestMD075RegistrationAvailabilityAndOptions(t *testing.T) {
	registered := false
	for _, registration := range rules.Registrations() {
		registered = registered || registration.ID == "MD075"
	}
	r := registry(t, rules.Registrations())
	if !registered || !r.Available("MD075") {
		t.Fatal("MD075 registration", registered)
	}
	s, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil || !s["MD075"].Available || !s["MD075"].Enabled || len(s["MD075"].Options) != 0 {
		t.Fatal(s["MD075"], err)
	}
	found := false
	for _, meta := range r.Metadata() {
		if meta.ID == "MD075" {
			found = true
			if !meta.DefaultEnabled || meta.Fix != engine.NoFix || !meta.RequiresSyntax || len(meta.Options) != 0 {
				t.Fatal(meta)
			}
		}
	}
	if !found {
		t.Fatal("MD075 metadata missing")
	}
	for _, mapping := range []map[string]any{{"unknown": 1}, {"style": "x"}, {"enabled": "yes"}, {"allow_directives": "false"}, {"enabled": nil}} {
		setting, err := engine.SettingFromMapping(mapping)
		if err == nil {
			_, err = r.ResolveSettings(engine.Settings{"MD075": setting}, engine.ValidationProviders{})
		}
		if err == nil {
			t.Fatal("invalid MD075 setting admitted", mapping)
		}
	}
}

type md075Literal struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Options      struct {
		Enabled     *bool
		Frontmatter string
	}
	Findings []md075LiteralFinding
}

type md075LiteralFinding struct {
	Bytes, Start, End [2]int
	Message           string
}

func md075Literals(t *testing.T) []md075Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md075.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != md075FixtureSHA {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md075Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 46 {
		t.Fatal(e, len(v.Cases))
	}
	return v.Cases
}

func md075Settings() engine.Settings {
	s := onlyMD056Settings()
	off, on := false, true
	s["MD056"] = engine.RuleSetting{Enabled: &off}
	s["MD075"] = engine.RuleSetting{Enabled: &on}
	return s
}

func md075LiteralSettings(tc md075Literal) engine.Settings {
	s := md075Settings()
	if tc.Options.Enabled != nil {
		s["MD075"] = engine.RuleSetting{Enabled: tc.Options.Enabled}
	}
	return s
}

func md075AssertFindings(t *testing.T, source string, ds []result.Diagnostic, want []md075LiteralFinding) {
	t.Helper()
	if len(ds) != len(want) {
		t.Fatal(ds, want)
	}
	for i, d := range ds {
		w := want[i]
		// A finding covers its trimmed run: never empty, never edge blanks or a line ending.
		run := source[w.Bytes[0]:w.Bytes[1]]
		if run == "" || strings.Trim(run, " \t\r\n") != run || w.Message != "Pipe rows are not part of a table." {
			t.Fatal("fixture range", w)
		}
		if d.Rule != "MD075" || d.Message != w.Message || d.FixAvailable || d.ByteRange.Start != w.Bytes[0] || d.ByteRange.End != w.Bytes[1] || d.Range.Start.Line != w.Start[0] || d.Range.Start.Column != w.Start[1] || d.Range.End.Line != w.End[0] || d.Range.End.Column != w.End[1] {
			t.Fatal(d, w)
		}
	}
}

func TestMD075FrozenProductionOracle(t *testing.T) {
	for _, tc := range md075Literals(t) {
		for _, flavor := range tc.Flavors {
			for _, inspect := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/inspect-%v", tc.Name, flavor, inspect), func(t *testing.T) {
					if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
						t.Fatal("source identity")
					}
					selection, e := registry(t, rules.Registrations()).SelectRules(md075LiteralSettings(tc), engine.ValidationProviders{})
					if e != nil {
						t.Fatal(e)
					}
					dc := engine.DocumentContext{Flavor: flavor}
					if tc.Options.Frontmatter != "" {
						dc.Frontmatter, e = parser.NewFrontmatterOptions(true, []string{tc.Options.Frontmatter})
						if e != nil {
							t.Fatal(e)
						}
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
					if out.Status != status {
						t.Fatal(out.Status, out.Diagnostics, tc.Findings)
					}
					md075AssertFindings(t, tc.Source, out.Diagnostics, tc.Findings)
					for _, d := range out.Diagnostics {
						if d.Path != "literal.md" || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}) {
							t.Fatal(d)
						}
					}
					if inspect {
						found := false
						for _, ev := range trace.Evaluations() {
							if ev.Rule == "MD075" {
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

func TestMD075FixtureCoversLineEndingsAndCases(t *testing.T) {
	names := map[string]md075Literal{}
	for _, tc := range md075Literals(t) {
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
			if tc.Source[w.Bytes[1]-1] == '\r' || tc.Source[w.Bytes[1]] == '\n' || strings.Count(tc.Source[w.Bytes[0]:w.Bytes[1]], "\r\n") != strings.Count(lf.Source[lf.Findings[i].Bytes[0]:lf.Findings[i].Bytes[1]], "\n") {
				t.Fatal("finding includes or splits the CRLF", name, w)
			}
		}
	}
	for _, name := range []string{"spec-example", "spec-mismatch", "spec-accepted", "same-cell-counts", "outer-pipes", "delimiter-row", "no-structure", "prose", "one-structural-line", "escaped-pipe", "backslash-parity", "code-span-pipe", "fenced-code", "indented-code", "html-block", "front-matter", "setext-heading", "header-delimiter-mismatch", "valid-table-short-long", "paragraph-before-table", "block-quote", "list-item", "quote-in-list", "separate-paragraphs", "spec-example-crlf", "indented-trailing-spaces", "non-ascii", "commonmark", "disabled"} {
		if _, ok := names[name]; !ok {
			t.Fatal("required fixture case missing", name)
		}
	}
}

func TestMD075LineEndingsShareRanges(t *testing.T) {
	for _, tc := range md075Literals(t) {
		if strings.Contains(tc.Source, "\r") {
			continue
		}
		for _, flavor := range tc.Flavors {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				source := strings.ReplaceAll(tc.Source, "\n", "\r\n")
				dc := engine.DocumentContext{Flavor: flavor}
				if tc.Options.Frontmatter != "" {
					var e error
					if dc.Frontmatter, e = parser.NewFrontmatterOptions(true, []string{tc.Options.Frontmatter}); e != nil {
						t.Fatal(e)
					}
				}
				out := analyzeHeadings(t, registry(t, rules.Registrations()), source, md075LiteralSettings(tc), dc, engine.RunOptions{})
				if !out.Complete || len(out.Errors)+len(out.Coverage) != 0 {
					t.Fatal(out)
				}
				want := make([]md075LiteralFinding, len(tc.Findings))
				for i, w := range tc.Findings {
					w.Bytes[0] += strings.Count(tc.Source[:w.Bytes[0]], "\n")
					w.Bytes[1] += strings.Count(tc.Source[:w.Bytes[1]], "\n")
					want[i] = w
				}
				md075AssertFindings(t, source, out.Diagnostics, want)
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

func TestMD075SuppressionInactiveAndUnused(t *testing.T) {
	for _, mode := range []string{"used", "denied", "unused", "inactive"} {
		t.Run(mode, func(t *testing.T) {
			source := "<!-- mdchk:disable MD075 -->\n" + md075Example
			if mode == "unused" {
				source = "<!-- mdchk:disable MD075 -->\nA | B\n--- | ---\n1 | 2\n"
			}
			if mode == "inactive" {
				source = md075Example
			}
			s := md075Settings()
			on, allow, enabled := true, mode != "denied", mode != "inactive"
			s["MD075"] = engine.RuleSetting{Enabled: &enabled, AllowDirectives: &allow}
			s["MD087"] = engine.RuleSetting{Enabled: &on}
			sel, err := registry(t, rules.Registrations()).SelectRules(s, engine.ValidationProviders{})
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
				case "MD075":
					visible++
					if d.Range.Start.Line != 2 || d.FixAvailable {
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
					if observation.Diagnostic.Rule == "MD075" {
						ranges = append(ranges, observation.Diagnostic.ByteRange)
					}
				}
				if !reflect.DeepEqual(ranges, []result.ByteRange{{Start: 0, End: 11}}) {
					t.Fatal("MD075 inactive observations", ranges)
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
				if hidden.Diagnostic.Rule != "MD075" || hidden.Diagnostic.ByteRange != (result.ByteRange{Start: 29, End: 40}) || hidden.Diagnostic.Range.Start.Line != 2 || hidden.SuppressedBy.Kind != "directive" {
					t.Fatal(hidden)
				}
			}
		})
	}
}

func TestMD075CommonMarkIsInapplicable(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			out := analyzeHeadings(t, registry(t, rules.Registrations()), md075Example, md075Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
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

func TestMD075UnavailableInventoryOutcome(t *testing.T) {
	called := false
	r := registry(t, []engine.Registration{{ID: "MD075", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
		called = true
		rows, err := c.PipeRowCandidates(ctx)
		if err != nil || rows.Availability != parser.Available || len(rows.Rows) != 2 {
			t.Fatal("actual positive prerequisite", rows, err)
		}
		out := pipeRowCandidateOutcome(ctx, c, parser.PipeRowCandidatesInventory{Availability: parser.Unavailable, Rows: rows.Rows})
		if out.Err != nil || out.Findings != nil || out.IncompleteReason != "Paragraph pipe-row context is unavailable; the complete MD075 inventory is withheld." {
			t.Fatal(out)
		}
		for _, availability := range []parser.Availability{"", "partial"} {
			if bad := pipeRowCandidateOutcome(ctx, c, parser.PipeRowCandidatesInventory{Availability: availability, Rows: rows.Rows}); !errors.Is(bad.Err, parser.ErrModel) || bad.Findings != nil {
				t.Fatal(bad)
			}
		}
		return out
	}}})
	out := analyzeHeadings(t, r, md075Example, md075Settings(), engine.DocumentContext{Flavor: "gfm"}, engine.RunOptions{})
	if !called || out.Complete || len(out.Diagnostics)+len(out.Errors) != 0 || len(out.Coverage) != 1 || out.Coverage[0].Rule != "MD075" || out.Coverage[0].Reason != result.ContextUnavailable || out.Coverage[0].Message != "Paragraph pipe-row context is unavailable; the complete MD075 inventory is withheld." {
		t.Fatal(out)
	}
}

func TestMD075ReportOnlyLeavesSourceAndPlansNothing(t *testing.T) {
	for _, mode := range []string{"", "project"} {
		t.Run(mode, func(t *testing.T) {
			source := md075Example + "\nprose\n\n| C |\n| D |\n"
			caller := []byte(source)
			s := md075Settings()
			outcomes := 0
			r := registry(t, []engine.Registration{{ID: "MD075", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				out := headerlessPipeRows(ctx, c)
				if out.Err != nil || len(out.Findings) != 2 || out.IncompleteReason != "" {
					t.Fatal(out)
				}
				for _, f := range out.Findings {
					if f.FixEligible {
						t.Fatal("MD075 finding is fix-eligible", f)
					}
				}
				outcomes++
				return out
			}}})
			for _, reg := range []*engine.Registry{r, registry(t, rules.Registrations())} {
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
					if d.Rule != "MD075" || d.FixAvailable {
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

func TestMD075DoesNotExecuteProtectedDirectives(t *testing.T) {
	s := md075Settings()
	on := true
	s["MD087"] = engine.RuleSetting{Enabled: &on}
	selected, err := registry(t, rules.Registrations()).SelectRules(s, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selected.Plan(engine.DocumentContext{Flavor: "gfm"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := "```md\n<!-- mdchk:disable MD075 -->\n```\n\n" + md075Example
	in, err := plan.NewInput("literal.md", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{})
	if err != nil || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD075" || out.Diagnostics[0].Range.Start.Line != 5 || len(trace.Directives().Candidates()) != 0 || len(out.Errors)+len(out.Coverage)+len(out.SuppressedDiagnostics) != 0 {
		t.Fatal(out, err)
	}
}

func TestMD075AndMD056NeverShareRows(t *testing.T) {
	s := md075Settings()
	on := true
	s["MD056"] = engine.RuleSetting{Enabled: &on}
	for _, tc := range []struct {
		source string
		rules  []string
	}{
		{"| a | b |\n| - | - |\n| 1 |\n| 1 | 2 | 3 |\n", []string{"MD056", "MD056"}},
		{"| A | B |\n| --- |\n| 1 |\n", []string{"MD075"}},
		{"x | y\n| h | i |\n| - | - |\n| 1 |\n", []string{"MD075"}},
		{"A | B\n--- | ---\n1 | 2\n", nil},
	} {
		out := analyzeHeadings(t, registry(t, rules.Registrations()), tc.source, s, engine.DocumentContext{Flavor: "gfm"}, engine.RunOptions{})
		var rules []string
		for _, d := range out.Diagnostics {
			rules = append(rules, d.Rule)
		}
		if !out.Complete || len(out.Errors)+len(out.Coverage) != 0 || !reflect.DeepEqual(rules, tc.rules) {
			t.Fatal(tc.source, out.Diagnostics)
		}
	}
}
