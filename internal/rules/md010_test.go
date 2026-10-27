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

func md010Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD010"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{}}
	return s
}
func TestMD010AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md010.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor, Style               string
		Recognize, Incomplete, Strict, CodeBlocks bool
		Spaces                                    int
		SHA256                                    string
		SizeBytes                                 int

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
				setting := md010Settings()
				setting["MD010"] = engine.RuleSetting{Options: engine.Options{"spaces_per_tab": f.Spaces, "code_blocks": f.CodeBlocks}}
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
					if !reflect.DeepEqual(got, w.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source || d.Rule != "MD010" || d.FixAvailable || d.Message != w.Message {
						t.Fatal(got, d, w)
					}
				}
			})
		}
	}
}
func TestMD010FinalPublicationWithholdsAllFindings(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		final        int
	}{{"empty", "", 1}, {"clean", "a\nb", 1}, {"violation", "a\t\nb\t\nc\t\nd\t\ne\t\n", 15}} {
		for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(tc.name+reason.Error(), func(t *testing.T) {
				var observed engine.RuleOutcome
				r := registry(t, []engine.Registration{{ID: "MD010", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, err := c.TabCharacters(ctx)
					if err != nil {
						t.Fatal(err)
					}
					// Fixed independent counts: one check per item, optional
					// finding growth/emission, then the final publication check.
					late := &md018LateContext{Context: ctx, reason: reason, stop: tc.final}
					observed = compareTabCharacters(late, c, items)
					return observed
				}}})
				out := analyzeHeadings(t, r, tc.source, md010Settings(), engine.DocumentContext{}, engine.RunOptions{})
				if !errors.Is(observed.Err, reason) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
					t.Fatal(observed, out)
				}
			})
		}
	}
}

func TestMD010CallbackQuotaAndCopyBoundaries(t *testing.T) {
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
					r := registry(t, []engine.Registration{{ID: "MD010", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						items, e := c.TabCharacters(ctx)
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
						observed = compareTabCharacters(ctx, c, items)
						return observed
					}}})
					return analyzeHeadings(t, r, "a\t\nb\t\nc\t\nd\t\ne\t\n", md010Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget}), observed
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

func TestMD010CatalogOptionsDefaultAndDirectives(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD010") || r.Available("MD011") || len(r.Metadata()) != 87 {
		t.Fatal("availability")
	}
	on, off := 0, 0
	for _, m := range r.Metadata() {
		if m.DefaultEnabled {
			on++
		} else {
			off++
		}
		if m.ID == "MD010" && (!m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.PlannedFix || len(m.Options) != 2) {
			t.Fatal(m)
		}
	}
	if on != 59 || off != 28 {
		t.Fatal(on, off)
	}
	for _, n := range []int{1, 4, int(^uint(0) >> 1)} {
		if _, err := r.ResolveSettings(engine.Settings{"MD010": {Options: engine.Options{"spaces_per_tab": n}}}, engine.ValidationProviders{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, opts := range []engine.Options{{"spaces_per_tab": 0}, {"spaces_per_tab": -1}, {"spaces_per_tab": nil}, {"spaces_per_tab": "4"}, {"spaces_per_tab": 4.0}, {"spaces_per_tab": true}, {"code_blocks": nil}, {"code_blocks": 1}, {"unknown": true}} {
		disabled := false
		if _, err := r.ResolveSettings(engine.Settings{"MD010": {Enabled: &disabled, Options: opts}}, engine.ValidationProviders{}); err == nil {
			t.Fatal(opts)
		}
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		s := md010Settings()
		delete(s, "MD010")
		out := analyzeHeadings(t, r, "a\t\n", s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
		if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD010" || out.Diagnostics[0].FixAvailable {
			t.Fatal(out)
		}
		for _, tc := range []struct {
			source         string
			active, hidden int
		}{
			{"a\t<!-- mdchk:disable MD010 -->\tb\n", 1, 1},
			{"<!-- mdchk:disable MD010 -->\na\t\nb <!-- mdchk:enable MD010 -->\tc", 1, 1},
			{"`<!-- mdchk:disable MD010 -->`\ta\n", 1, 0},
			{"a <script><!-- mdchk:disable MD010 -->x</script>\ta\n", 1, 0},
		} {
			out := analyzeHeadings(t, r, tc.source, md010Settings(), engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
				t.Fatal(tc, out)
			}
		}
	}
}

func TestMD010ActiveMetadataDirectiveControls(t *testing.T) {
	r := registry(t, Registrations())
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, header := range []string{"---\nx: y\n<!-- mdchk:disable MD010 -->\n---\na\t\n", "+++\nx = 1\n<!-- mdchk:disable MD010 -->\n+++\na\t\n"} {
			for _, recognize := range []bool{false, true} {
				fm, _ := parser.NewFrontmatterOptions(recognize, nil)
				out := analyzeHeadings(t, r, header, md010Settings(), engine.DocumentContext{Flavor: flavor, Frontmatter: fm}, engine.RunOptions{})
				want := 0
				if recognize {
					want = 1
				}
				if !out.Complete || len(out.Diagnostics) != want || len(out.SuppressedDiagnostics) != 1-want {
					t.Fatal(flavor, recognize, out)
				}
			}
		}
	}
}
