package rules

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func md043Settings(pattern []string) engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD043"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"headings": pattern}}
	return s
}

func TestMD043AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md043.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source          string
		Headings              []string
		Recognize, Incomplete bool
		Expected, GFMExpected []struct {
			Range  []int
			Source string
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	r := registry(t, Registrations())
	for _, f := range fixtures {
		if f.Name == "nul" && !strings.ContainsRune(f.Source, 0) {
			t.Fatal("NUL fixture is not original NUL")
		}
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(f.Name+"/"+flavor, func(t *testing.T) {
				fm, _ := parser.NewFrontmatterOptions(f.Recognize, nil)
				dc := engine.DocumentContext{Flavor: flavor, Frontmatter: fm}
				s := md043Settings(f.Headings)
				out := analyzeHeadings(t, r, f.Source, s, dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, s, dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) {
					t.Fatal("nondeterminism")
				}
				expected := f.Expected
				if flavor == "gfm" && f.GFMExpected != nil {
					expected = f.GFMExpected
				}
				if out.Complete == f.Incomplete || len(out.Diagnostics) != len(expected) || len(out.Errors) != 0 || len(out.Interruptions) != 0 {
					t.Fatalf("complete=%v diag=%v coverage=%v errors=%v", out.Complete, out.Diagnostics, out.Coverage, out.Errors)
				}
				if f.Incomplete {
					found := false
					for _, c := range out.Coverage {
						found = found || c.Reason == result.ParserUncertain
					}
					if !found {
						t.Fatal(out.Coverage)
					}
				}
				for i, w := range expected {
					d := out.Diagnostics[i]
					got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if d.Rule != "MD043" || d.Message != "Headings must match the configured outline." || !reflect.DeepEqual(got, w.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source {
						t.Fatalf("got %+v %v want %+v", d, got, w)
					}
				}
			})
		}
	}
}

func TestMD043CatalogAndOptions(t *testing.T) {
	r := registry(t, Registrations())
	if !r.Available("MD043") || r.Available("MD063") || len(r.Metadata()) != 87 {
		t.Fatal("availability")
	}
	for _, m := range r.Metadata() {
		if m.ID == "MD043" && (m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.NoFix || len(m.Options) != 1 || m.Options["headings"].Domain != engine.HeadingOutline) {
			t.Fatal(m)
		}
	}
	for _, enabled := range []bool{false, true} {
		for _, value := range []any{nil, "# A", []any{"# A", 1}, []string{""}, []string{"#"}, []string{"#A"}, []string{"####### A"}, []string{" # A"}, []string{"#\tA"}, []string{"* "}, []string{"# A\nB"}, []string{"# A\rB"}} {
			_, err := r.ResolveSettings(engine.Settings{"MD043": {Enabled: &enabled, Options: engine.Options{"headings": value}}}, engine.ValidationProviders{})
			if err == nil {
				t.Fatal("invalid outline admitted", value)
			}
		}
		for _, value := range [][]string{{}, {"*"}, {"*", "*"}, {"# "}, {"# *"}, {"#  A "}, {"###### A"}, {"# **A**"}} {
			if _, err := r.ResolveSettings(engine.Settings{"MD043": {Enabled: &enabled, Options: engine.Options{"headings": value}}}, engine.ValidationProviders{}); err != nil {
				t.Fatal(value, err)
			}
		}
		if _, err := r.ResolveSettings(engine.Settings{"MD043": {Enabled: &enabled, Options: engine.Options{"unknown": true}}}, engine.ValidationProviders{}); err == nil {
			t.Fatal("unknown option")
		}
	}
	s := md043Settings([]string{"# B"})
	delete(s, "MD043")
	out := analyzeHeadings(t, r, "# A", s, engine.DocumentContext{}, engine.RunOptions{})
	if !out.Complete || len(out.Diagnostics) != 0 {
		t.Fatal(out)
	}
}

func TestMD043Directives(t *testing.T) {
	for _, tc := range []struct {
		name, source          string
		pattern               []string
		enabled, allowed      bool
		active, hidden, owner int
	}{
		{"first hidden no later error", "<!-- mdchk:disable MD043 -->\n# X\n<!-- mdchk:enable MD043 -->\n# Y", []string{"# A", "# B"}, true, true, 0, 1, 0},
		{"hidden heading remains input", "<!-- mdchk:disable MD043 -->\n# A\n<!-- mdchk:enable MD043 -->\n# B", []string{"# A", "# B"}, true, true, 0, 0, 0},
		{"configured enable", "<!-- mdchk:enable MD043 -->\n# X", []string{"# A"}, false, true, 1, 0, 0},
		{"off throughout", "# X", []string{"# A"}, false, true, 0, 0, 0},
		{"deny disable", "<!-- mdchk:disable MD043 -->\n# X", []string{"# A"}, true, false, 1, 0, 0},
		{"deny enable", "<!-- mdchk:enable MD043 -->\n# X", []string{"# A"}, false, false, 0, 0, 0},
		{"last owner", "<!-- mdchk:disable MD043 -->\n<!-- mdchk:disable MD043 -->\n# X", []string{"# A"}, true, true, 0, 1, 1},
		{"EOF disabled", "# A\n<!-- mdchk:disable MD043 -->", []string{"# A", "# B"}, true, true, 0, 1, 0},
		{"protected fake", "`<!-- mdchk:disable MD043 -->`\n\n# X", []string{"# A"}, true, true, 1, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := md043Settings(tc.pattern)
			s["MD043"] = engine.RuleSetting{Enabled: &tc.enabled, AllowDirectives: &tc.allowed, Options: engine.Options{"headings": tc.pattern}}
			out := analyzeHeadings(t, registry(t, Registrations()), tc.source, s, engine.DocumentContext{}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden {
				t.Fatal(out)
			}
			if tc.hidden > 0 {
				trace := out.AnalysisRecords()[0].(engine.DirectiveAnalysis)
				if trace.Suppressed()[0].Owner != tc.owner {
					t.Fatal(trace.Suppressed())
				}
			}
		})
	}
}

func TestMD043AddedWorkInterruptions(t *testing.T) {
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		var observed error
		r := registry(t, []engine.Registration{{ID: "MD043", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			out := requiredHeadingOutline(&traversalContext{Context: ctx, err: reason}, c)
			observed = out.Err
			return out
		}}})
		out := analyzeHeadings(t, r, "# A\n# C", md043Settings([]string{"# A", "# B"}), engine.DocumentContext{}, engine.RunOptions{})
		if !errors.Is(observed, reason) || out.Complete || len(out.Interruptions) != 1 || len(out.Diagnostics) != 0 || len(out.Errors) != 0 {
			t.Fatal(observed, out)
		}
	}
	for _, kind := range []string{"work", "nodes"} {
		budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
		r := registry(t, []engine.Registration{{ID: "MD043", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			if kind == "work" {
				_ = budget.ChargeWork(budget.Limits().WorkUnits)
			} else {
				_ = budget.ChargeNodes(budget.Limits().TotalNodes)
			}
			return requiredHeadingOutline(ctx, c)
		}}})
		out := analyzeHeadings(t, r, "# A", md043Settings([]string{"# B"}), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
		if out.Complete || len(out.Interruptions) != 1 || len(out.Diagnostics) != 0 || len(out.Errors) != 0 {
			t.Fatal(out)
		}
	}
}

func TestMD043LateOperationRetainsReliableObservation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			var operation context.Context
			var cancel context.CancelFunc
			if deadline {
				operation, cancel = context.WithTimeout(context.Background(), time.Millisecond)
			} else {
				operation, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			seen := false
			r := registry(t, []engine.Registration{{ID: "MD043", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				out := requiredHeadingOutline(ctx, c)
				if out.Err != nil || len(out.Findings) != 1 {
					t.Fatal(out)
				}
				seen = true
				if deadline {
					time.Sleep(2 * time.Millisecond)
				} else {
					cancel()
				}
				return out
			}}})
			in, err := engine.NewInput("outline.md", []byte("# A"), md043Settings([]string{"# B"}), engine.DocumentContext{})
			if err != nil {
				t.Fatal(err)
			}
			out, err := r.Lint(operation, []engine.Input{in}, engine.RunOptions{})
			if err != nil || !seen || out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 2, End: 3}) || len(out.Interruptions) != 1 || len(out.Errors) != 0 {
				t.Fatal(err, out)
			}
		})
	}
}

func TestMD043WorkspaceArithmetic(t *testing.T) {
	maximum := int(^uint(0) >> 1)
	for _, tc := range [][2]int{{-1, 1}, {1, -1}, {maximum, 0}, {0, maximum}, {maximum / 2, 3}, {maximum / 8, 1}} {
		if _, _, ok := outlineDimensions(tc[0], tc[1]); ok {
			t.Fatal("overflow admitted", tc)
		}
	}
	if cols, cells, ok := outlineDimensions(3, 4); !ok || cols != 5 || cells != 20 {
		t.Fatal(cols, cells, ok)
	}
}

// This finite oracle enumerates complete edit scripts without a DP table or the
// production matcher. Lexicographic script order implements the published tie
// order independently of the production action grid.
func TestMD043IndependentSmallAlignmentOracle(t *testing.T) {
	type answer struct {
		cost, first int
		kind        byte
		script      []byte
	}
	oracle := func(pattern, actual []string) answer {
		best := answer{cost: 1000, first: -1}
		var visit func(int, int, int, int, byte, []byte)
		visit = func(i, j, cost, first int, kind byte, script []byte) {
			if cost > best.cost {
				return
			}
			if i == len(pattern) && j == len(actual) {
				if cost < best.cost || cost == best.cost && string(script) < string(best.script) {
					best = answer{cost, first, kind, append([]byte(nil), script...)}
				}
				return
			}
			next := func(a, b, extra int, step byte) {
				at, k := first, kind
				if extra != 0 && at < 0 {
					at, k = j, step
				}
				visit(a, b, cost+extra, at, k, append(append([]byte(nil), script...), step))
			}
			if i < len(pattern) && pattern[i] == "*" {
				next(i+1, j, 0, 2)
				if j < len(actual) {
					next(i, j+1, 0, 3)
				}
				return
			}
			if i < len(pattern) && j < len(actual) {
				if pattern[i] == actual[j] {
					next(i+1, j+1, 0, 1)
				}
				next(i+1, j+1, 1, 4)
			}
			if i < len(pattern) {
				next(i+1, j, 1, 5)
			}
			if j < len(actual) {
				next(i, j+1, 1, 6)
			}
		}
		visit(0, 0, 0, -1, 0, nil)
		return best
	}
	words := func(alphabet []string, max int) [][]string {
		all := [][]string{{}}
		layer := [][]string{{}}
		for range max {
			var next [][]string
			for _, prefix := range layer {
				for _, x := range alphabet {
					next = append(next, append(append([]string(nil), prefix...), x))
				}
			}
			all = append(all, next...)
			layer = next
		}
		return all
	}
	r := registry(t, Registrations())
	for _, pattern := range words([]string{"A", "B", "*"}, 3) {
		for _, actual := range words([]string{"A", "B"}, 3) {
			descriptors := make([]string, len(pattern))
			for i, x := range pattern {
				descriptors[i] = x
				if x != "*" {
					descriptors[i] = "# " + x
				}
			}
			lines := make([]string, len(actual))
			for i, x := range actual {
				lines[i] = "# " + x
			}
			source := strings.Join(lines, "\n")
			want := oracle(pattern, actual)
			out := analyzeHeadings(t, r, source, md043Settings(descriptors), engine.DocumentContext{}, engine.RunOptions{})
			if !out.Complete || len(out.Errors) != 0 {
				t.Fatal(pattern, actual, out)
			}
			if len(pattern) == 0 || want.cost == 0 {
				if len(out.Diagnostics) != 0 {
					t.Fatal(pattern, actual, want, out.Diagnostics)
				}
				continue
			}
			if len(out.Diagnostics) != 1 {
				t.Fatal(pattern, actual, want, out.Diagnostics)
			}
			start, end := len(source), len(source)
			if want.first < len(actual) {
				start = want.first * 4
				end = start
				if want.kind != 5 {
					start += 2
					end = start + 1
				}
			}
			if out.Diagnostics[0].ByteRange != (result.ByteRange{Start: start, End: end}) {
				t.Fatalf("pattern%v actual%v oracle%+v got%v want[%d,%d)", pattern, actual, want, out.Diagnostics[0].ByteRange, start, end)
			}
		}
	}
}

func TestMD043MatchingWorkAndFinalWithholding(t *testing.T) {
	source := strings.Repeat("# "+strings.Repeat("x", 200)+"\n", 40)
	pattern := make([]string, 40)
	for i := range pattern {
		pattern[i] = "# " + strings.Repeat("x", 200)
	}
	run := func(outline []string, work int64) result.Result {
		limits := parser.DefaultLimits()
		limits.WorkUnits = work
		budget, _ := parser.NewOperationBudget(limits)
		return analyzeHeadings(t, registry(t, Registrations()), source, md043Settings(outline), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
	}
	empty := run([]string{}, 500000)
	small := run(pattern, 500000)
	enough := run(pattern, 5000000)
	if !empty.Complete || len(empty.Diagnostics) != 0 || small.Complete || len(small.Interruptions) != 1 || small.Interruptions[0].Limit == nil || small.Interruptions[0].Limit.Name != "work-units" || len(small.Diagnostics) != 0 || !enough.Complete || len(enough.Diagnostics) != 0 {
		t.Fatal(empty, small, enough)
	}

	calls := 0
	execute := func(stop int, reason error) (result.Result, engine.RuleOutcome) {
		var observed engine.RuleOutcome
		r := registry(t, []engine.Registration{{ID: "MD043", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
			p, err := c.HeadingOutline(ctx)
			if err != nil {
				t.Fatal(err)
			}
			hs, err := c.Headings(ctx)
			if err != nil {
				t.Fatal(err)
			}
			count := &md018LateContext{Context: ctx, reason: reason, stop: stop}
			span, err := compareHeadingOutline(count, c, p, hs)
			calls = count.calls
			observed = engine.RuleOutcome{Err: err}
			if span.Present() {
				observed.Findings = []engine.Finding{{Span: span, Message: "tested outline"}}
			}
			return observed
		}}})
		out := analyzeHeadings(t, r, "# A\n# B\n# B\n# C", md043Settings([]string{"# A", "*", "# B", "# D"}), engine.DocumentContext{}, engine.RunOptions{})
		return out, observed
	}
	full, observed := execute(0, context.Canceled)
	if !full.Complete || len(observed.Findings) != 1 {
		t.Fatal(full, observed)
	}
	total := calls
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, stop := range []int{1, total / 2, total - 1, total} {
			out, observed := execute(stop, reason)
			if !errors.Is(observed.Err, reason) || len(observed.Findings) != 0 || out.Complete || len(out.Diagnostics) != 0 || len(out.Interruptions) != 1 {
				t.Fatal(stop, observed, out)
			}
		}
	}
}

func TestMD043WorkspaceReservationUsesOriginalNodes(t *testing.T) {
	source := strings.Repeat("# A\n", 12)
	pattern := make([]string, 12)
	for i := range pattern {
		pattern[i] = "# A"
	}
	run := func(nodes int64, outline []string) result.Result {
		limits := parser.DefaultLimits()
		limits.TotalNodes = nodes
		limits.DocumentNodes = nodes
		budget, _ := parser.NewOperationBudget(limits)
		return analyzeHeadings(t, registry(t, Registrations()), source, md043Settings(outline), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
	}
	// A small finite allowance admits original parsing and the empty no-check path,
	// but cannot also retain the two 13x13 workspace arrays.
	empty := run(500, []string{})
	small := run(500, pattern)
	full := run(2000, pattern)
	limits := parser.DefaultLimits()
	limits.TotalNodes = 500
	limits.DocumentNodes = 500
	budget, _ := parser.NewOperationBudget(limits)
	reached := false
	r := registry(t, []engine.Registration{{ID: "MD043", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
		p, err := c.HeadingOutline(ctx)
		if err != nil {
			t.Fatal("options failed before grid", err)
		}
		hs, err := c.Headings(ctx)
		if err != nil {
			t.Fatal("inventory failed before grid", err)
		}
		reached = true
		span, err := compareHeadingOutline(ctx, c, p, hs)
		if span.Present() {
			t.Fatal("failed workspace exposed span")
		}
		return engine.RuleOutcome{Err: err}
	}}})
	grid := analyzeHeadings(t, r, source, md043Settings(pattern), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
	if !reached || grid.Complete || len(grid.Interruptions) != 1 || grid.Interruptions[0].Limit == nil || grid.Interruptions[0].Limit.Name != "total-nodes" {
		t.Fatal("not grid reservation", grid)
	}
	if !empty.Complete || small.Complete || len(small.Interruptions) != 1 || small.Interruptions[0].Limit == nil || small.Interruptions[0].Limit.Name != "total-nodes" || len(small.Diagnostics) != 0 || !full.Complete {
		t.Fatal(empty, small, full)
	}
}

func TestMD043EmptyOutlineAvoidsHeadingConstruction(t *testing.T) {
	source := "# **A**\n# B\n# C"
	baseline := registry(t, []engine.Registration{{ID: "MD043", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { return engine.RuleOutcome{} }}})
	run := func(r *engine.Registry, n int64) result.Result {
		limits := parser.DefaultLimits()
		limits.TotalNodes = n
		limits.DocumentNodes = n
		budget, _ := parser.NewOperationBudget(limits)
		return analyzeHeadings(t, r, source, md043Settings([]string{}), engine.DocumentContext{}, engine.RunOptions{Budget: budget})
	}
	low, high := int64(1), int64(1000)
	for low < high {
		n := (low + high) / 2
		if run(baseline, n).Complete {
			high = n
		} else {
			low = n + 1
		}
	}
	plain := run(baseline, low)
	actual := run(registry(t, Registrations()), low)
	if !plain.Complete || !actual.Complete || len(actual.Diagnostics) != 0 {
		t.Fatal("empty outline constructed headings", plain, actual)
	}
}
