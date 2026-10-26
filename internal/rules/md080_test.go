package rules

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func md080Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD080"] = engine.RuleSetting{Enabled: &on}
	return s
}
func TestMD080AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md080.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, AnchorStyle               string
		Recognize, ParserUncertain, Unavailable bool
		GFMUnavailable                          *bool
		Expected, GFMExpected                   []struct {
			Range  []int
			Source string
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	r := registry(t, Registrations())
	for _, f := range fixtures {
		if f.Name == "nul normalized" && !strings.ContainsRune(f.Source, 0) {
			t.Fatal("fixture did not decode original NUL byte")
		}
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(f.Name+"/"+flavor, func(t *testing.T) {
				fm, _ := parser.NewFrontmatterOptions(f.Recognize, nil)
				dc := engine.DocumentContext{Flavor: flavor, Frontmatter: fm}
				settings := md080Settings()
				dc.AnchorStyle = f.AnchorStyle
				expected := f.Expected
				if flavor == "gfm" && f.GFMExpected != nil {
					expected = f.GFMExpected
				}
				out := analyzeHeadings(t, r, f.Source, settings, dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, settings, dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) {
					t.Fatal("nondeterministic analysis")
				}
				unavailable := f.Unavailable
				if flavor == "gfm" && f.GFMUnavailable != nil {
					unavailable = *f.GFMUnavailable
				}
				if out.Complete == (f.ParserUncertain || unavailable) || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(expected) {
					t.Fatalf("unexpected outcome: %+v", out)
				}
				if f.ParserUncertain || unavailable {
					found := false
					for _, c := range out.Coverage {
						found = found || (f.ParserUncertain && c.Reason == result.ParserUncertain) || (unavailable && c.Reason == result.ContextUnavailable)
					}
					if !found {
						t.Fatal(out.Coverage)
					}
				}
				for i, want := range expected {
					d := out.Diagnostics[i]
					got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if !reflect.DeepEqual(got, want.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != want.Source || d.Rule != "MD080" || d.Message != "Heading anchor base must not collide." {
						t.Fatalf("got %v %+v, want %+v", got, d, want)
					}
				}
			})
		}
	}
}

func TestMD080DefaultSchemaAndPerDocumentState(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD080") || r.Available("MD095") || len(r.Metadata()) != 87 {
		t.Fatal("availability")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD080" && (m.DefaultEnabled || m.Fix != engine.NoFix || !m.RequiresSyntax || m.UsesFrontmatter || len(m.Options) != 0) {
			t.Fatal(m)
		}
	}
	for _, enabled := range []bool{false, true} {
		for _, option := range []engine.Options{{"anchor_style": "github"}, {"unknown": true}} {
			if _, err := r.ResolveSettings(engine.Settings{"MD080": {Enabled: &enabled, Options: option}}, engine.ValidationProviders{}); err == nil {
				t.Fatal(option)
			}
		}
	}
	s := md080Settings()
	delete(s, "MD080")
	if out := analyzeHeadings(t, r, "# A\n# A", s, engine.DocumentContext{}, engine.RunOptions{}); !out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
	a, err := engine.NewInput("a.md", []byte("# A"), md080Settings(), engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := engine.NewInput("b.md", []byte("# A"), md080Settings(), engine.DocumentContext{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Lint(context.Background(), []engine.Input{a, b}, engine.RunOptions{})
	if err != nil || !out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out, err)
	}
}
func TestMD080HiddenContextAndDirectiveDenials(t *testing.T) {
	for _, tc := range []struct {
		name, source     string
		enabled, allowed bool
		active, hidden   int
	}{
		{"hidden first baseline", "<!-- mdchk:disable MD080 -->\n# A\n<!-- mdchk:enable MD080 -->\n# A", true, true, 1, 0},
		{"hidden middle establishes context", "# A\n<!-- mdchk:disable MD080 -->\n# A\n<!-- mdchk:enable MD080 -->\n# A", true, true, 1, 1},
		{"configured enable baseline", "# A\n<!-- mdchk:enable MD080 -->\n# A", false, true, 1, 0},
		{"off throughout", "# A\n# A", false, true, 0, 0},
		{"denied disable", "# A\n<!-- mdchk:disable MD080 -->\n# A", true, false, 1, 0},
		{"denied enable", "# A\n<!-- mdchk:enable MD080 -->\n# A", false, false, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := md080Settings()
			s["MD080"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allowed}
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, s, engine.DocumentContext{}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
				t.Fatal(out)
			}
		})
	}
}
func md080BudgetRun(t *testing.T, source string, nodes, work int64, compare bool) result.Result {
	t.Helper()
	limits := parser.DefaultLimits()
	limits.TotalNodes = nodes
	limits.DocumentNodes = nodes
	limits.WorkUnits = work
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	r := registry(t, []engine.Registration{{ID: "MD080", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
		hs, a, err := c.HeadingAnchors(ctx)
		if err != nil {
			return engine.RuleOutcome{Err: err}
		}
		if a != resource.Available {
			return engine.RuleOutcome{IncompleteReason: "unexpected unavailable test context"}
		}
		if compare {
			return compareHeadingAnchorBases(ctx, c, hs)
		}
		return engine.RuleOutcome{}
	}}})
	return analyzeHeadings(t, r, source, md080Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
}
func TestMD080MapReservationBeforeAllocation(t *testing.T) {
	source := "# A\n# B"
	low, high := int64(1), int64(5000)
	for low < high {
		mid := (low + high) / 2
		if md080BudgetRun(t, source, mid, 5000000, false).Complete {
			high = mid
		} else {
			low = mid + 1
		}
	}
	baseline := md080BudgetRun(t, source, low, 5000000, false)
	actual := md080BudgetRun(t, source, low, 5000000, true)
	if !baseline.Complete || actual.Complete || len(actual.Interruptions) != 1 || actual.Interruptions[0].Limit == nil || actual.Interruptions[0].Limit.Name != "total-nodes" || len(actual.Diagnostics) != 0 {
		t.Fatal(low, baseline, actual)
	}
}
func TestMD080LongBaseHashComparisonWork(t *testing.T) {
	source := "# " + strings.Repeat("a", 4000) + "x\n# " + strings.Repeat("a", 4000) + "y"
	low, high := int64(1), int64(5000000)
	for low < high {
		mid := (low + high) / 2
		if md080BudgetRun(t, source, 50000, mid, false).Complete {
			high = mid
		} else {
			low = mid + 1
		}
	}
	baseline := md080BudgetRun(t, source, 50000, low+1000, false)
	actual := md080BudgetRun(t, source, 50000, low+1000, true)
	enough := md080BudgetRun(t, source, 50000, 5000000, true)
	if !baseline.Complete || actual.Complete || len(actual.Interruptions) != 1 || actual.Interruptions[0].Limit == nil || actual.Interruptions[0].Limit.Name != "work-units" || len(actual.Diagnostics) != 0 || !enough.Complete {
		t.Fatal(low, baseline, actual, enough)
	}
}

func TestMD080BorrowedBaseBytesDoNotConsumeNodeRecords(t *testing.T) {
	source := "# " + strings.Repeat("a", 2000) + "\n# " + strings.Repeat("a", 2000)
	limits := parser.DefaultLimits()
	limits.TotalNodes = 500
	limits.DocumentNodes = 500
	budget, _ := parser.NewOperationBudget(limits)
	out := analyzeHeadings(t, registry(t, Registrations()), source, md080Settings(), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
	if !out.Complete || len(out.Diagnostics) != 1 || len(out.Interruptions) != 0 {
		t.Fatal(out)
	}
}
func TestMD080AddedWorkInterruptionAndFinalChecks(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		var observed engine.RuleOutcome
		r := registry(t, []engine.Registration{{ID: "MD080", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			observed = collidingHeadingAnchors(&traversalContext{Context: ctx, err: reason}, c)
			return observed
		}}})
		out := analyzeHeadings(t, r, "# A\n# A", md080Settings(), engine.DocumentContext{}, engine.RunOptions{})
		if !errors.Is(observed.Err, reason) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
			t.Fatal(observed, out)
		}
	}
}
func TestMD080LateOperationRetainsRealDuplicateFindings(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			var ctx context.Context
			var cancel context.CancelFunc
			if deadline {
				ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			r := registry(t, []engine.Registration{{ID: "MD080", Check: func(caller context.Context, c engine.RuleContext) engine.RuleOutcome {
				out := collidingHeadingAnchors(caller, c)
				if out.Err != nil || len(out.Findings) != 2 {
					t.Fatal(out)
				}
				if deadline {
					time.Sleep(2 * time.Millisecond)
				} else {
					cancel()
				}
				return out
			}}})
			in, err := engine.NewInput("a.md", []byte("# A\n# A\n# A"), md080Settings(), engine.DocumentContext{})
			if err != nil {
				t.Fatal(err)
			}
			out, err := r.Lint(ctx, []engine.Input{in}, engine.RunOptions{})
			if err != nil || out.Complete || len(out.Diagnostics) != 2 || len(out.Interruptions) != 1 || len(out.Errors) != 0 {
				t.Fatal(out, err)
			}
		})
	}
}
