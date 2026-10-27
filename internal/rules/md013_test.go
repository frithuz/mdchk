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

func md013Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD013"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"length": 1}}
	return s
}
func TestMD013AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md013.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor         string
		Recognize, Incomplete        bool
		Length                       int
		CodeBlocks, Tables, Headings bool
		SHA256                       string
		SizeBytes                    int

		Expected []struct {
			Range   []int
			Source  string
			Message string
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	r := registry(t, Registrations())
	for _, f := range fixtures {
		for _, flavor := range []string{"commonmark", "gfm"} {
			if f.Flavor != "" && f.Flavor != flavor {
				continue
			}
			t.Run(f.Name+"/"+flavor, func(t *testing.T) {
				fm, _ := parser.NewFrontmatterOptions(f.Recognize, nil)
				dc := engine.DocumentContext{Flavor: flavor, Frontmatter: fm}
				setting := md013Settings()
				on := true
				setting["MD013"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"length": f.Length, "code_blocks": f.CodeBlocks, "tables": f.Tables, "headings": f.Headings}}
				out := analyzeHeadings(t, r, f.Source, setting, dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, setting, dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) || out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(f.Expected) {
					t.Fatal("outcome", out.Complete, out.Diagnostics, out.Errors, out.Interruptions, "expected", f.Expected)
				}
				snapshot := out.Files[0].InputSnapshot
				if snapshot == nil || snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source))) || snapshot.SizeBytes != f.SizeBytes || snapshot.SHA256 != f.SHA256 {
					t.Fatal(out.Files)
				}
				uncertain := false
				for _, coverage := range out.Coverage {
					uncertain = uncertain || coverage.Reason == result.ParserUncertain
				}
				if uncertain != f.Incomplete || (!f.Incomplete && len(out.Coverage) != 0) {
					t.Fatal(out.Coverage)
				}
				for i, w := range f.Expected {
					d := out.Diagnostics[i]
					got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if !reflect.DeepEqual(got, w.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source || d.Rule != "MD013" || d.FixAvailable || d.Message != w.Message {
						t.Fatal(got, d, w)
					}
				}
			})
		}
	}
}
func TestMD013CatalogAndStrictOptions(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD013") || r.Available("MD011") || len(r.Metadata()) != 87 {
		t.Fatal("catalog")
	}
	on := 0
	for _, m := range r.Metadata() {
		if m.DefaultEnabled {
			on++
		}
		if m.ID == "MD013" && (m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.NoFix || len(m.Options) != 4) {
			t.Fatal(m)
		}
	}
	if on != 59 {
		t.Fatal(on)
	}
	for _, n := range []int{1, int(^uint(0) >> 1)} {
		if _, e := r.ResolveSettings(engine.Settings{"MD013": {Options: engine.Options{"length": n}}}, engine.ValidationProviders{}); e != nil {
			t.Fatal(e)
		}
	}
	for _, opts := range []engine.Options{{"length": 0}, {"length": -1}, {"length": nil}, {"length": "1"}, {"length": 1.0}, {"length": true}, {"unknown": true}, {"code_blocks": 1}, {"tables": "false"}, {"headings": nil}} {
		off := false
		if _, e := r.ResolveSettings(engine.Settings{"MD013": {Enabled: &off, Options: opts}}, engine.ValidationProviders{}); e == nil {
			t.Fatal(opts)
		}
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		s := md013Settings()
		delete(s, "MD013")
		out := analyzeHeadings(t, r, "long line\n", s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
		if !out.Complete || len(out.Diagnostics) != 0 {
			t.Fatal(out)
		}
	}
}
func TestMD013FinalPublicationWithholdsAllFindings(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 1}, {"clean", "a\nb", 1}, {"violation", "aa\naa\naa\naa\naa\n", 15}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+reason.Error(), func(t *testing.T) {
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD013", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, err := c.MaximumLineWidth(ctx)
					if err != nil {
						t.Fatal(err)
					}
					// Fixed independent counts: one check per item, optional
					// finding growth/emission, then the final publication check.
					late := &md018LateContext{Context: ctx, reason: reason, stop: tc.final}
					observed = compareMaximumLineWidth(late, c, items)
					return observed
				}}})
				out := analyzeHeadings(t, r, tc.source, md013Settings(), engine.DocumentContext{}, engine.RunOptions{})
				if !errors.Is(observed.Err, reason) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
					t.Fatal(observed, out)
				}
			})
		}
	}
}

func TestMD013CallbackQuotaAndCopyBoundaries(t *testing.T) {
	for _, kind := range []string{"work", "nodes"} {
		for _, remaining := range []int64{0, 20, 21} {
			if kind == "nodes" && remaining == 20 {
				remaining = 16
			}
			if kind == "nodes" && remaining == 21 {
				remaining = 17
			}
			t.Run(fmt.Sprintf("%s/%d", kind, remaining), func(t *testing.T) {
				var used int64
				run := func(probe bool) (result.Result, engine.RuleOutcome) {
					budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
					var observed engine.RuleOutcome
					r := registry(t, []engine.Registration{{ID: "MD013", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						items, e := c.MaximumLineWidth(ctx)
						if e != nil || len(items) != 5 {
							t.Fatal(items, e)
						}
						charge, limit := budget.ChargeWork, budget.Limits().WorkUnits
						if kind == "nodes" {
							charge, limit = budget.ChargeNodes, budget.Limits().TotalNodes
						}
						if probe {
							var stopped *parser.LimitExceeded
							if !errors.As(charge(limit), &stopped) {
								t.Fatal("probe must exhaust")
							}
							used = stopped.Observed - limit
							return engine.RuleOutcome{Err: stopped}
						}
						if e := charge(limit - used - remaining); e != nil {
							t.Fatal(e)
						}
						observed = compareMaximumLineWidth(ctx, c, items)
						return observed
					}}})
					return analyzeHeadings(t, r, "aa\naa\naa\naa\naa\n", md013Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget}), observed
				}
				run(true)
				out, observed := run(false)
				success := remaining == 21 || remaining == 17
				if success {
					if observed.Err != nil || len(observed.Findings) != 5 {
						t.Fatal(out, observed)
					}
				} else {
					var limit *parser.LimitExceeded
					if !errors.As(observed.Err, &limit) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 {
						t.Fatal("direct callback quota omitted", out, observed)
					}
				}
			})
		}
	}
}

func TestMD013DirectiveStartAndRetainedOptions(t *testing.T) {
	r := registry(t, Registrations())
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source   string
			enabled, allow bool
			active, hidden int
			start          int
		}{
			{"before", "a <!-- mdchk:disable MD013 --> ABCDEFGHIJ\n", true, true, 0, 1, 30},
			{"late", "ABCDEFGHIJ <!-- mdchk:disable MD013 -->\n", true, true, 1, 0, 8},
			{"configured-off", "a <!-- mdchk:enable MD013 --> ABCDEFGHIJ\n", false, true, 1, 0, 29},
			{"denied-disable", "<!-- mdchk:disable MD013 -->\nABCDEFGHIJ\n", true, false, 1, 0, 37},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				s := md013Settings()
				s["MD013"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allow, Options: engine.Options{"length": 8}}
				out := analyzeHeadings(t, r, tc.source, s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
				if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
					t.Fatal(out)
				}
				if tc.active > 0 && (out.Diagnostics[0].ByteRange.Start != tc.start || out.Diagnostics[0].ByteRange.End != tc.start+1) {
					t.Fatal(out.Diagnostics)
				}
				if tc.hidden > 0 {
					var hidden result.SuppressedDiagnostic
					if e := json.Unmarshal(out.SuppressedDiagnostics[0], &hidden); e != nil {
						t.Fatal(e)
					}
					if hidden.Diagnostic.ByteRange.Start != tc.start || hidden.Diagnostic.ByteRange.End != tc.start+1 {
						t.Fatal(hidden)
					}
				}
			})
		}
	}
}
